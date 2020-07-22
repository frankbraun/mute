// Copyright (c) 2015 Mute Communications Ltd.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package util contains utility functions for Mute.
package util

import (
	"bufio"
	"errors"
	"fmt"
	"os"

	"github.com/mutecomm/mute/log"
	"golang.org/x/crypto/ssh/terminal"
)

// ErrNotImplemented is returned if the used functionality is not implemented y
//
// TODO: implement everything and remove.
var ErrNotImplemented = errors.New("not implemented")

// Fatal prints err to stderr and exits the process with exit code 1.
func Fatal(err error) {
	fmt.Fprintf(os.Stderr, "%s: error: %s\n", os.Args[0], err)
	os.Exit(1)
}

// Readline reads a single line from the file pointer fp with given name.
// It closes the file pointer afterwards.
// Make sure you do not call it multiple times on the same file pointer!
func Readline(fp *os.File) ([]byte, error) {
	defer fp.Close()
	fd := int(fp.Fd())
	if terminal.IsTerminal(fd) {
		return terminal.ReadPassword(fd)
	}
	scanner := bufio.NewScanner(fp)
	var line []byte
	if scanner.Scan() {
		line = scanner.Bytes()
	} else if err := scanner.Err(); err != nil {
		return nil, log.Error(err)
	}
	return line, nil
}

// CreateDirs creates all given directories.
func CreateDirs(dirs ...string) error {
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return log.Error(err)
		}
	}
	return nil
}

// ContainsString returns true, if the string array sa contains the string s.
// Otherwise, it returns false.
func ContainsString(sa []string, s string) bool {
	for _, v := range sa {
		if v == s {
			return true
		}
	}
	return false
}
