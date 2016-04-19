// Copyright (c) 2015 Mute Communications Ltd.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ctrlengine implements the command engine for mutectrl.
package ctrlengine

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/agl/ed25519"
	"github.com/codegangsta/cli"
	"github.com/mutecomm/mute/configclient"
	"github.com/mutecomm/mute/def"
	"github.com/mutecomm/mute/def/version"
	"github.com/mutecomm/mute/encode/base64"
	"github.com/mutecomm/mute/log"
	"github.com/mutecomm/mute/msgdb"
	"github.com/mutecomm/mute/release"
	"github.com/mutecomm/mute/serviceguard/client"
	"github.com/mutecomm/mute/serviceguard/client/trivial"
	"github.com/mutecomm/mute/util"
	"github.com/mutecomm/mute/util/bzero"
	"github.com/mutecomm/mute/util/descriptors"
	"github.com/mutecomm/mute/util/git"
	"github.com/mutecomm/mute/util/home"
	"github.com/peterh/liner"
)

// possible states
const (
	startState = iota
	noDBs
	lockedDBs
	emptyDBs
	unlockedDBs
	lockedDaemon
	composingMsg
)

var (
	defaultHomeDir = home.AppDataDir("mute", false)
	defaultLogDir  = filepath.Join(defaultHomeDir, "log")
	errExit        = errors.New("cryptengine: requests exit")
)

// CtrlEngine abstracts a mutectrl command engine.
type CtrlEngine struct {
	prepared   bool
	fileTable  *descriptors.Table
	state      int
	msgDB      *msgdb.MsgDB
	passphrase []byte
	client     *client.Client // service guard client
	config     configclient.Config
	app        *cli.App
	err        error
}

func (ce *CtrlEngine) translateError(err error) error {
	switch err {
	case client.ErrNoUser:
		var walletPubkey string
		var pk []byte
		privkey, err := ce.msgDB.GetValue(msgdb.WalletKey)
		if err == nil {
			pk, err = base64.Decode(privkey)
		}
		if err == nil {
			walletPubkey = base64.Encode(pk[32:])
		}
		return fmt.Errorf("Unfortunately, you do not have tokens, yet!\n"+
			"Please send your \n"+
			"WALLETPUBKEY\t%s\n"+
			"per email to frank@cryptogroup.net and stay tuned!", walletPubkey)
	default:
		return err
	}
}

func (ce *CtrlEngine) getConfig(homedir string, offline bool) error {
	// read default config
	netDomain, _, _ := def.ConfigParams()
	jsn, err := ce.msgDB.GetValue(netDomain)
	if err != nil {
		return err
	}
	if jsn != "" {
		if err := json.Unmarshal([]byte(jsn), &ce.config); err != nil {
			return err
		}
		// apply old configuration
		err := def.InitMute(&ce.config)
		if err != nil {
			// init failed -> update config (which will try init again)
			fmt.Fprintf(ce.fileTable.StatusFP,
				"initialization failed, try to update config\n")
			if offline {
				return log.Error("ctrlengine: cannot fetch config in " +
					"--offline mode, run without")
			}
			err := ce.upkeepFetchconf(ce.msgDB, homedir, false, nil,
				ce.fileTable.StatusFP)
			if err != nil {
				return err
			}
		} else {
			// fetch new configuration, if last fetch is older than 24h
			timestr, err := ce.msgDB.GetValue("time." + netDomain)
			if err != nil {
				return err
			}
			if timestr != "" {
				t, err := strconv.ParseInt(timestr, 10, 64)
				if err != nil {
					return log.Error(err)
				}
				last := time.Now().Sub(time.Unix(t, 0))
				if last > def.FetchconfMinDuration {
					if offline {
						if last > def.FetchconfMaxDuration {
							return log.Error("ctrlengine: configuration is " +
								"outdated, please run without --offline")
						}
						log.Warn("ctrlengine: cannot fetch outdated config " +
							"in --offline mode")
						fmt.Fprintf(ce.fileTable.StatusFP,
							"ctrlengine: cannot fetch outdated config in "+
								"--offline mode\n")
					} else {
						// update config
						err := ce.upkeepFetchconf(ce.msgDB, homedir, false, nil,
							ce.fileTable.StatusFP)
						if err != nil {
							return err
						}
					}
				}
			}
		}
	} else {
		// no config found, fetch it
		if offline {
			return log.Error("ctrlengine: cannot fetch config in --offline mode")
		}
		fmt.Fprintf(ce.fileTable.StatusFP, "no system config found\n")
		err := ce.upkeepFetchconf(ce.msgDB, homedir, false, nil,
			ce.fileTable.StatusFP)
		if err != nil {
			return err
		}
	}
	return nil
}

func (ce *CtrlEngine) checkUpdates() error {
	commit := ce.config.Map["release.Commit"]
	log.Info("checkUpdates()")
	log.Infof("server: release.Commit: %s", commit)
	log.Infof("binary: release.Commit: %s", release.Commit)
	if release.Commit != commit {
		// parse release date
		tRelease, err := time.Parse(git.Date, ce.config.Map["release.Date"])
		if err != nil {
			return err
		}
		// parse binary date
		tBinary, err := time.Parse(git.Date, release.Date)
		if err != nil {
			return err
		}
		// switch to UTC
		tRelease = tRelease.UTC()
		tBinary = tBinary.UTC()
		log.Infof("server: release.Date: %s", tRelease.Format(time.RFC3339))
		log.Infof("binary: release.Date: %s", tBinary.Format(time.RFC3339))
		// compare dates
		if !tBinary.Before(tRelease) {
			// binary is newer than release -> do nothing
			log.Info("binary is newer than release -> do nothing")
		} else if tBinary.Add(def.UpdateDuration).Before(tRelease) {
			// binary is totally outdated -> force update
			log.Info("binary is totally outdated -> force update")
			return log.Error("ctrlengine: software is outdated, you have to " +
				"update with `mutectrl upkeep update`")
		} else {
			// new version available -> inform user
			log.Info("new version available -> inform user")
			fmt.Fprintf(ce.fileTable.StatusFP, "ctrlengine: software "+
				"available, please update with `mutectrl upkeep update`\n")
		}
	}
	return nil
}

func startWallet(msgDB *msgdb.MsgDB, offline bool) (*client.Client, error) {
	// get wallet key
	wk, err := msgDB.GetValue(msgdb.WalletKey)
	if err != nil {
		return nil, err
	}
	walletKey, err := decodeWalletKey(wk)
	if err != nil {
		return nil, err
	}

	// create wallet
	client, err := trivial.New(msgDB.DB(), walletKey, def.CACert)
	if err != nil {
		return nil, err
	}
	if !offline {
		client.GoOnline()
		err = client.GetVerifyKeys()
		if err != nil {
			return nil, err
		}
	}

	return client, nil
}

func (ce *CtrlEngine) prepare(
	c *cli.Context,
	openMsgDB, checkUpdates bool,
) error {
	if !ce.prepared {
		// create the necessary directories if they don't already exist
		err := util.CreateDirs(c.GlobalString("homedir"), c.GlobalString("logdir"))
		if err != nil {
			return err
		}

		// initialize logging framework
		err = log.Init(c.GlobalString("loglevel"), "ctrl ",
			c.GlobalString("logdir"), c.GlobalBool("logconsole"))
		if err != nil {
			return err
		}

		// initialize file descriptors
		ce.fileTable, err = descriptors.NewTable(c)
		if err != nil {
			return err
		}

		ce.prepared = true
	}

	log.Infof("prepare(openMsgDB=%s)", strconv.FormatBool(openMsgDB))

	// open MsgDB, if necessary
	if openMsgDB {
		homedir := c.GlobalString("homedir")
		offline := c.GlobalBool("offline")

		// open messsage DB, if necessary
		if ce.msgDB == nil {
			err := ce.openMsgDB(homedir)
			if err != nil {
				return err
			}
		}

		// get config
		if err := ce.getConfig(homedir, offline); err != nil {
			return err
		}

		// check for updates, if necessary
		if checkUpdates {
			if err := ce.checkUpdates(); err != nil {
				return err
			}
		}

		// start wallet
		var err error
		ce.client, err = startWallet(ce.msgDB, offline)
		if err != nil {
			return err
		}
	}

	return nil
}

func buildCmdList(commands []cli.Command, prefix string) []string {
	var cmds []string
	for _, cmd := range commands {
		if cmd.Subcommands != nil {
			cmds = append(cmds, buildCmdList(cmd.Subcommands, cmd.Name+" ")...)
		} else {
			cmds = append(cmds, prefix+cmd.Name)
		}
	}
	return cmds
}

var (
	interactive bool
	line        *liner.State
)

// loop runs the CtrlEngine in a loop and reads commands from the file
// descriptor command-fd.
// TODO: actually read from command-fd!
func (ce *CtrlEngine) loop(c *cli.Context) {
	if len(c.Args()) > 0 {
		ce.err = fmt.Errorf("ctrlengine: unknown command '%s', try 'help'",
			strings.Join(c.Args(), " "))
		return
	}

	log.Info("ctrlengine: starting")

	interactive = true

	// run command(s)
	line = liner.NewLiner()
	defer line.Close()
	line.SetCtrlCAborts(true)
	commands := buildCmdList(c.App.Commands, "")
	line.SetCompleter(func(line string) (c []string) {
		for _, command := range commands {
			if strings.HasPrefix(command, line) {
				c = append(c, command)
			}
		}
		return
	})

	for {
		active, err := ce.msgDB.GetValue(msgdb.ActiveUID)
		if err != nil {
			util.Fatal(err)
		}
		if active == "" {
			active = "none"
		}
		fmt.Fprintf(ce.fileTable.StatusFP, "active user ID: %s\n", active)
		fmt.Fprintln(ce.fileTable.StatusFP, "READY.")
		ln, err := line.Prompt("")
		if err != nil {
			if err == liner.ErrPromptAborted {
				fmt.Fprintf(ce.fileTable.StatusFP, "aborting...\n")
			}
			log.Info("ctrlengine: stopping (error)")
			log.Error(err)
			return
		}
		line.AppendHistory(ln)

		args := []string{ce.app.Name}
		if ln == "" {
			log.Infof("read empty line")
			continue
		}
		log.Infof("read: %s", ln)
		// in the loop these global variables are reset, therefore we have to
		// pass them in again
		args = append(args,
			"--homedir", c.GlobalString("homedir"),
			"--logdir", c.GlobalString("logdir"),
			"--loglevel", c.GlobalString("loglevel"),
		)
		args = append(args, strings.Fields(ln)...)
		if err := ce.app.Run(args); err != nil {
			// command execution failed -> issue status and continue
			log.Infof("command execution failed (app): %s", err)
			fmt.Fprintln(ce.fileTable.StatusFP, err)
			continue
		}
		if ce.err != nil {
			if ce.err == errExit {
				// exit requested -> return
				log.Info("ctrlengine: stopping (exit requested)")
				ce.err = nil
				return
			}
			// command execution failed -> issue status and continue
			fmt.Fprintln(ce.fileTable.StatusFP, ce.translateError(ce.err))
			ce.err = nil
		} else {
			log.Info("command successful")
		}
	}
}

func (ce *CtrlEngine) getID(c *cli.Context) string {
	id := c.String("id")
	if id == "" && interactive {
		active, err := ce.msgDB.GetValue(msgdb.ActiveUID)
		if err != nil {
			panic(log.Critical(err))
		}
		id = active
	}
	return id
}

func checkDelayArgs(c *cli.Context) error {
	if !c.Bool("nodelaycheck") {
		if c.Int("mindelay") < def.MinMinDelay {
			return log.Errorf("--mindelay must be at least %d", def.MinMinDelay)
		}
		if c.Int("maxdelay") < def.MinMaxDelay {
			return log.Errorf("--maxdelay must be at least %d", def.MinMaxDelay)
		}
		if c.Int("mindelay") >= c.Int("maxdelay") {
			return log.Error("--mindelay must be strictly smaller than --maxdelay")
		}
	}
	return nil
}

// New returns a new CtrlEngine.
func New() *CtrlEngine {
	var ce CtrlEngine
	ce.app = cli.NewApp()
	ce.app.Usage = "tool that handles message DB, contacts, and tokens."
	ce.app.Version = version.Number
	ce.app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "homedir",
			Value: defaultHomeDir,
			Usage: "set home directory",
		},
		descriptors.InputFDFlag,
		descriptors.OutputFDFlag,
		descriptors.StatusFDFlag,
		descriptors.PassphraseFDFlag,
		descriptors.CommandFDFlag,
		cli.BoolFlag{
			Name:  "offline",
			Usage: "use offline mode",
		},
		cli.StringFlag{
			Name:  "loglevel",
			Value: "info",
			Usage: "logging level {trace, debug, info, warn, error, critical}",
		},
		cli.StringFlag{
			Name:  "logdir",
			Value: defaultLogDir,
			Usage: "directory to log output",
		},
		cli.BoolFlag{
			Name:  "logconsole",
			Usage: "enable logging to console",
		},
	}
	ce.app.Before = func(c *cli.Context) error {
		return ce.prepare(c, false, false)
	}
	ce.app.After = func(c *cli.Context) error {
		// TODO: close all file descriptors?
		return nil
	}
	ce.app.Action = func(c *cli.Context) {
		if err := ce.prepare(c, true, true); err != nil {
			util.Fatal(err)
		}
		ce.loop(c)
	}
	idFlag := cli.StringFlag{
		Name:  "id",
		Usage: "user ID (self)",
	}
	allFlag := cli.BoolFlag{
		Name:  "all",
		Usage: "perform action for all user IDs (bad for anonymity!)",
	}
	contactFlag := cli.StringFlag{
		Name:  "contact",
		Usage: "user ID of contact (peer)",
	}
	fullNameFlag := cli.StringFlag{
		Name:  "full-name",
		Usage: "optional full name for user ID (local)",
	}
	hostFlag := cli.StringFlag{
		Name:  "host",
		Usage: "alternative hostname",
	}
	mindelayFlag := cli.IntFlag{
		Name:  "mindelay",
		Value: int(def.MinDelay),
		Usage: fmt.Sprintf("minimum delay for mix (min. %ds)", def.MinMinDelay),
	}
	maxdelayFlag := cli.IntFlag{
		Name:  "maxdelay",
		Value: int(def.MaxDelay),
		Usage: fmt.Sprintf("maximum delay for mix (min. %ds)", def.MinMaxDelay),
	}
	nodelaycheckFlag := cli.BoolFlag{
		Name:  "nodelaycheck",
		Usage: "disable delay checks (for testing purposes only!)",
	}
	msgNumFlag := cli.IntFlag{
		Name:  "msgnum",
		Usage: "message ID to process",
	}
	ce.app.Commands = []cli.Command{
		{
			Name:  "app",
			Usage: "Start app mode (opens web browser)",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "docroot",
					Value: "gui/docroot",
					Usage: "document root for app",
				},
				cli.StringFlag{
					Name:  "http",
					Value: "localhost:0",
					Usage: "HTTP service address (port 0 means random port)",
				},
			},
			Before: func(c *cli.Context) error {
				if len(c.Args()) > 0 {
					return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
				}
				if err := ce.prepare(c, false, false); err != nil {
					return err
				}
				return nil
			},
			Action: func(c *cli.Context) {
				ce.err = ce.appStart(c, ce.fileTable.StatusFP,
					c.String("docroot"), c.String("http"))
			},
		},
		{
			Name:  "db",
			Usage: "Commands for local databases",
			Subcommands: []cli.Command{
				{
					Name:  "create",
					Usage: "Create databases",
					Flags: []cli.Flag{
						cli.IntFlag{
							Name:  "iterations",
							Value: def.KDFIterationsDB,
							Usage: "number of KDF iterations used for DB creation",
						},
						cli.StringFlag{
							Name:  "walletkey",
							Usage: "use this private wallet key instead of generated one",
						},
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						return ce.prepare(c, false, false)
					},
					Action: func(c *cli.Context) {
						ce.err = ce.dbCreate(ce.fileTable.OutputFP,
							ce.fileTable.StatusFP, c.GlobalString("homedir"), c)
					},
				},
				{
					Name:  "rekey",
					Usage: "Rekey databases",
					Flags: []cli.Flag{
						cli.IntFlag{
							Name:  "iterations",
							Value: def.KDFIterationsDB,
							Usage: "number of KDF iterations used for DB rekeying",
						},
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						return ce.prepare(c, false, false)
					},
					Action: func(c *cli.Context) {
						ce.err = ce.dbRekey(ce.fileTable.StatusFP, c)
					},
				},
				/*
					{
						Name:  "status",
						Usage: "Show DB status",
						Before: func(c *cli.Context) error {
							if len(c.Args()) > 0 {
								return log.Errorf("superfluous argument(s): %s",
									strings.Join(c.Args(), " "))
							}
							if err := ce.prepare(c, true, true); err != nil {
								return err
							}
							return nil
						},
						Action: func(c *cli.Context) {
							ce.err = ce.dbStatus(c, ce.fileTable.OutputFP)
						},
					},
				*/
				{
					Name:  "vacuum",
					Usage: "Do full DB rebuild (VACUUM)",
					/*
						Flags: []cli.Flag{
							cli.StringFlag{
								Name:  "auto-vacuum",
								Usage: "also change auto_vacuum mode (possible modes: NONE, FULL, INCREMENTAL)",
							},
						},
					*/
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s",
								strings.Join(c.Args(), " "))
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.dbVacuum(c, "FULL")
					},
				},
				/*
					{
						Name:  "incremental",
						Usage: "Remove free pages in auto_vacuum=INCREMENTAL mode",
						Flags: []cli.Flag{
							cli.IntFlag{
								Name:  "pages",
								Usage: "number of pages to remove (default: all)",
							},
						},
						Before: func(c *cli.Context) error {
							if len(c.Args()) > 0 {
								return log.Errorf("superfluous argument(s): %s",
									strings.Join(c.Args(), " "))
							}
							if err := ce.prepare(c, true, true); err != nil {
								return err
							}
							return nil
						},
						Action: func(c *cli.Context) {
							ce.err = ce.dbIncremental(c, int64(c.Int("pages")))
						},
					},
				*/
				{
					Name:  "version",
					Usage: "Show DB version",
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s",
								strings.Join(c.Args(), " "))
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.dbVersion(c, ce.fileTable.OutputFP)
					},
				},
			},
		},
		{
			Name:  "uid",
			Usage: "Commands for user IDs",
			Subcommands: []cli.Command{
				{
					Name:  "new",
					Usage: "register a new user ID",
					Description: `
Tries to register a new user ID with the corresponding key server.
`,
					Flags: []cli.Flag{
						idFlag,
						fullNameFlag,
						hostFlag,
						mindelayFlag,
						maxdelayFlag,
						nodelaycheckFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if err := checkDelayArgs(c); err != nil {
							return err
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.uidNew(c, int32(c.Int("mindelay")),
							int32(c.Int("maxdelay")), c.String("host"))
					},
				},
				{
					Name:  "edit",
					Usage: "edit an existing user ID",
					Flags: []cli.Flag{
						idFlag,
						fullNameFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.uidEdit(c.String("id"), c.String("full-name"))
					},
				},
				{
					Name:  "active",
					Usage: "show active user ID",
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.uidActive(c, ce.fileTable.OutputFD,
							ce.fileTable.OutputFP)
					},
				},
				{
					Name:  "switch",
					Usage: "switch active user ID",
					Flags: []cli.Flag{
						idFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.uidSwitch(c.String("id"))
					},
				},
				{
					Name:  "delete",
					Usage: "delete own user ID",
					Flags: []cli.Flag{
						idFlag,
						cli.BoolFlag{
							Name:  "force",
							Usage: "force deletion (do not prompt)",
						},
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.uidDelete(c, c.String("id"), c.Bool("force"),
							ce.fileTable.StatusFP)
					},
				},
				{
					Name:  "list",
					Usage: "list own (unmapped) user IDs",
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.uidList(ce.fileTable.OutputFP)
					},
				},
			},
		},
		{
			Name:  "contact",
			Usage: "Commands for contact management",
			Subcommands: []cli.Command{
				{
					Name:  "add",
					Usage: "add new contact to active user ID (-> white list)",
					Flags: []cli.Flag{
						idFlag,
						contactFlag,
						fullNameFlag,
						hostFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if !c.IsSet("contact") {
							return log.Error("option --contact is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.contactAdd(ce.getID(c), c.String("contact"),
							c.String("full-name"), c.String("host"),
							msgdb.WhiteList, c)
					},
				},
				{
					Name:  "edit",
					Usage: "edit contact entry of active user ID",
					Flags: []cli.Flag{
						idFlag,
						contactFlag,
						fullNameFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if !c.IsSet("contact") {
							return log.Error("option --contact is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.contactEdit(ce.getID(c),
							c.String("contact"), c.String("full-name"))
					},
				},
				{
					Name:  "remove",
					Usage: "remove contact for active user ID (-> gray list)",
					Flags: []cli.Flag{
						idFlag,
						contactFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if !c.IsSet("contact") {
							return log.Error("option --contact is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.contactRemove(ce.getID(c),
							c.String("contact"))
					},
				},
				{
					Name:  "block",
					Usage: "block contact for active user ID (-> black list)",
					Flags: []cli.Flag{
						idFlag,
						contactFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if !c.IsSet("contact") {
							return log.Error("option --contact is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.contactBlock(ce.getID(c),
							c.String("contact"))
					},
				},
				{
					Name:  "unblock",
					Usage: "unblock contact for active user ID (-> white list)",
					Flags: []cli.Flag{
						idFlag,
						contactFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if !c.IsSet("contact") {
							return log.Error("option --contact is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.contactUnblock(ce.getID(c),
							c.String("contact"))
					},
				},
				{
					Name:  "list",
					Usage: "list contacts for active user ID (white list)",
					Flags: []cli.Flag{
						idFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.contactList(ce.fileTable.OutputFP, ce.getID(c))
					},
				},
				{
					Name:  "blacklist",
					Usage: "list blocked contacts for active user ID (black list)",
					Flags: []cli.Flag{
						idFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.contactBlacklist(ce.fileTable.OutputFP,
							ce.getID(c))
					},
				},
			},
		},
		{
			Name:  "msg",
			Usage: "Commands for message processing",
			Subcommands: []cli.Command{
				{
					Name:  "add",
					Usage: "add a new message to outqueue",
					Description: `
Add a new message to outqueue.
If option --mail-input is set the input is parsed as an email message and the
'To' field is used as recipient and the optional 'Subject' combined with the
email body as the actual message.
`,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:  "from, id",
							Usage: "user ID to send message from",
						},
						cli.StringFlag{
							Name:  "to",
							Usage: "user ID to send message to",
						},
						cli.StringFlag{
							Name:  "file",
							Usage: "read message from file",
						},
						cli.BoolFlag{
							Name:  "mail-input",
							Usage: "treat input as email message",
						},
						// TODO: implement options
						/*
							cli.StringSliceFlag{
								Name:  "attach",
								Usage: "file to append as attachment",
							},
							cli.BoolFlag{
								Name:  "permanent-signature",
								Usage: "add permanent sign. to message",
							},
						*/
						mindelayFlag,
						maxdelayFlag,
						nodelaycheckFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s",
								strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("from") {
							return log.Error("option --from is mandatory")
						}
						if !c.IsSet("mail-input") && !c.IsSet("to") {
							return log.Error("option --to is mandatory")
						}
						if c.IsSet("mail-input") && c.IsSet("to") {
							return log.Error("options --to and --mail-input exclude each other")
						}
						if err := checkDelayArgs(c); err != nil {
							return err
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.msgAdd(c, ce.getID(c), c.String("to"),
							c.String("file"), c.Bool("mail-input"),
							c.Bool("permanent-signature"),
							c.StringSlice("attach"),
							int32(c.Int("mindelay")), int32(c.Int("maxdelay")),
							line, ce.fileTable.InputFP)
					},
				},
				{
					Name:  "send",
					Usage: "send messages from out queue",
					Flags: []cli.Flag{
						idFlag,
						allFlag,
						cli.BoolFlag{
							Name:  "fail-delivery",
							Usage: "Fail on first delivery attempt (for testing purposes)",
						},
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("all") && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.msgSend(c, ce.getID(c), c.Bool("all"),
							c.Bool("fail-delivery"))
					},
				},
				{
					Name:  "fetch",
					Usage: "fetch new messages and decrypt them",
					Flags: []cli.Flag{
						idFlag,
						allFlag,
						hostFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("all") && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.msgFetch(c, ce.getID(c), c.Bool("all"),
							c.String("host"))
					},
				},
				{
					Name:  "list",
					Usage: "list messages",
					Flags: []cli.Flag{
						idFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.msgList(ce.fileTable.OutputFP, ce.getID(c))
					},
				},
				{
					Name:  "read",
					Usage: "read message",
					Flags: []cli.Flag{
						idFlag,
						msgNumFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if !c.IsSet("msgnum") {
							return log.Error("option --msgnum is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.msgRead(ce.fileTable.OutputFP, ce.getID(c),
							int64(c.Int("msgnum")))
					},
				},
				{
					Name:  "delete",
					Usage: "delete a message",
					Description: `
Deletes a message.					
A deleted message is permanently gone. Handle with care!
					`,
					Flags: []cli.Flag{
						idFlag,
						msgNumFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if !c.IsSet("msgnum") {
							return log.Error("option --msgnum is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.msgDelete(ce.getID(c), int64(c.Int("msgnum")))
					},
				},
			},
		},
		{
			Name:  "upkeep",
			Usage: "Commands for upkeep (maintenance)",
			Subcommands: []cli.Command{
				{
					Name:  "all",
					Usage: "Perform all upkeep tasks for user ID",
					Flags: []cli.Flag{
						idFlag,
						cli.StringFlag{
							Name:  "period",
							Usage: "perform task only if last execution was earlier than period",
						},
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s",
								strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if !c.IsSet("period") {
							return log.Error("option --period is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.upkeepAll(c, ce.getID(c),
							c.String("period"), ce.fileTable.StatusFP)
					},
				},
				{
					Name:  "fetchconf",
					Usage: "Fetch current Mute system config",
					Flags: []cli.Flag{
						cli.BoolFlag{
							Name:  "show",
							Usage: "Show config on output-fd",
						},
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s",
								strings.Join(c.Args(), " "))
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.upkeepFetchconf(ce.msgDB,
							c.GlobalString("homedir"), c.Bool("show"),
							ce.fileTable.OutputFP, ce.fileTable.StatusFP)
					},
				},
				{
					Name:  "update",
					Usage: "Update Mute binaries (from source)",
					// TODO: "Update Mute binaries (from source or download binaries)",
					/*
						Flags: []cli.Flag{
							cli.BoolFlag{
								Name:  "source",
								Usage: "Force update from source",
							},
							cli.BoolFlag{
								Name:  "binary",
								Usage: "Force binary update",
							},
						},
					*/
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s",
								strings.Join(c.Args(), " "))
						}
						/*
							if c.Bool("source") && c.Bool("binary") {
								return log.Error("options --source and --binary exclude each other")
							}
						*/
						if err := ce.prepare(c, true, false); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.upkeepUpdate(c.GlobalString("homedir"),
							/* c.Bool("source"), c.Bool("binary"), */
							ce.fileTable.OutputFP, ce.fileTable.StatusFP)
					},
				},
				{
					Name:  "accounts",
					Usage: "Renew accounts on server",
					Flags: []cli.Flag{
						idFlag,
						cli.StringFlag{
							Name:  "period",
							Usage: "perform task only if last execution was earlier than period",
						},
						cli.StringFlag{
							Name:  "remaining",
							Value: "2160h",
							Usage: "renew account only if remaining time is less than remaining",
						},
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s",
								strings.Join(c.Args(), " "))
						}
						if !interactive && !c.IsSet("id") {
							return log.Error("option --id is mandatory")
						}
						if !c.IsSet("period") {
							return log.Error("option --period is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.upkeepAccounts(ce.getID(c),
							c.String("period"), c.String("remaining"),
							ce.fileTable.StatusFP)
					},
				},
				{
					Name:  "hashchain",
					Usage: "Sync and verify hashchain for the given domain.",
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:  "domain",
							Usage: "key server domain",
						},
						hostFlag,
					},
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s",
								strings.Join(c.Args(), " "))
						}
						if !c.IsSet("domain") {
							return log.Error("option --domain is mandatory")
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.upkeepHashchain(c, c.String("domain"),
							c.String("host"))
					},
				},
			},
		},
		{
			Name:  "wallet",
			Usage: "Commands for wallet management",
			Subcommands: []cli.Command{
				{
					Name:  "pubkey",
					Usage: "Show public key of wallet",
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s",
								strings.Join(c.Args(), " "))
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.walletPubkey(ce.fileTable.OutputFP)
					},
				},
				{
					Name:  "balance",
					Usage: "Show balance key of wallet",
					Before: func(c *cli.Context) error {
						if len(c.Args()) > 0 {
							return log.Errorf("superfluous argument(s): %s",
								strings.Join(c.Args(), " "))
						}
						if err := ce.prepare(c, true, true); err != nil {
							return err
						}
						return nil
					},
					Action: func(c *cli.Context) {
						ce.err = ce.walletBalance(ce.fileTable.OutputFP)
					},
				},
			},
		},
		{
			Name:  "quit",
			Usage: "End program",
			Before: func(c *cli.Context) error {
				if len(c.Args()) > 0 {
					return log.Errorf("superfluous argument(s): %s", strings.Join(c.Args(), " "))
				}
				if err := ce.prepare(c, false, false); err != nil {
					return err
				}
				return nil
			},
			Action: func(c *cli.Context) {
				ce.err = errExit
			},
		},
	}
	return &ce
}

// Start starts the CtrlEngine with the given args.
func (ce *CtrlEngine) Start(args []string) error {
	ce.app.Name = args[0]
	if err := ce.app.Run(args); err != nil {
		return err
	}
	if ce.err != nil {
		return ce.translateError(ce.err)
	}
	return nil
}

// TODO: extract method
func decodeWalletKey(p string) (*[ed25519.PrivateKeySize]byte, error) {
	var ret [ed25519.PrivateKeySize]byte
	pd, err := base64.Decode(p)
	if err != nil {
		return nil, err
	}
	copy(ret[:], pd)
	return &ret, nil
}

func (ce *CtrlEngine) openMsgDB(
	homedir string,
) error {
	// read passphrase, if necessary
	if ce.passphrase == nil {
		fmt.Fprintf(ce.fileTable.StatusFP, "read passphrase from fd %d (not echoed)\n",
			ce.fileTable.PassphraseFD)
		log.Infof("read passphrase from fd %d (not echoed)",
			ce.fileTable.PassphraseFD)
		var err error
		ce.passphrase, err = util.Readline(ce.fileTable.PassphraseFP)
		if err != nil {
			return err
		}
		log.Info("done")
	}

	// open msgDB
	msgdbname := filepath.Join(homedir, "msgs")
	log.Infof("open msgDB %s", msgdbname)
	var err error
	ce.msgDB, err = msgdb.Open(msgdbname, ce.passphrase)
	if err != nil {
		return err
	}
	return nil
}

// Close the underlying database of the CtrlEngine.
func (ce *CtrlEngine) Close() {
	if ce.msgDB != nil {
		// stop service guard client before we close the DB
		if ce.client != nil {
			ce.client.GoOffline()
		}
		ce.msgDB.Close()
		ce.msgDB = nil
	}
	bzero.Bytes(ce.passphrase)
}
