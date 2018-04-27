## Mute — secure messaging — α release

[![GoDoc](https://img.shields.io/badge/go-documentation-blue.svg?style=flat-square)](https://godoc.org/github.com/mutecomm/mute) [![Build Status](https://img.shields.io/travis/mutecomm/mute.svg?style=flat-square)](https://travis-ci.org/mutecomm/mute) [![Go Report Card](https://goreportcard.com/badge/github.com/mutecomm/mute?style=flat-square)](https://goreportcard.com/report/github.com/mutecomm/mute) [![License](https://img.shields.io/badge/license-BSD-brightgreen.svg?style=flat-square)](https://github.com/mutecomm/mute/blob/master/LICENSE)

This is an **alpha** release of Mute — **use at your own risk**!
There are known bugs, all sorts of things might go wrong, and you might not be
able to keep registered user IDs.
At the moment, you can only play with the command-line interface `mutectrl`,
a graphical user interface will be released at a later stage.


### Features

- End-to-end encryption.
- Communication with forward secrecy (keys required to decrypt past messages
  are frequently replaced and destroyed).
- Establish forward secret communication with first message (no synchronous
  two-way handshake).
- Asynchronous communication between peers.
- Authenticity of the identity-key relationship is established and preserved.
- Human-readable/human-memorizable identities.
- Communication with established standards where possible (e.g., using SMTP as
  the transport protocol).
- Sending messages and changing the state of one's own identity requires
  payment by the user to both finance the system and to limit SPAM.
- Better-than-nothing anonymity: mixing/delaying of messages to prevent simple
  discovery of sender-recipient relationship by third party passive observer.
- Both sender and recipient anonymity.
- Plausible deniability of message content and some deniability of
  communication relationship.


### Installation

At the moment, only installation from source is supported.
You must have at least [Go 1.6](https://golang.org/dl/) installed (with `GOPATH` set accordingly and `$GOPATH/bin` being part of your `PATH`).

To install `mutectrl` execute the following three commands:

```
go get -u -v github.com/mutecomm/mute/cmd/mutegenerate
go generate -v github.com/mutecomm/mute/release
go get -u -v github.com/mutecomm/mute/cmd/...
```


### Setup

Before you can start using `mutectrl` you have to create your encrypted
database files with a passphrase. The following command does just that (and
reads the passphrase from stdin):

```
mutectrl --passphrase-fd stdin db create
```

This also fetches the necessary configuration settings from our config server
and prints your `WALLETPUBKEY` (you can always print your wallet key with
`mutectrl wallet pubkey`).

To be able to use Mute we have to charge your wallet. For now, this is
absolutely **free of charge**. Just send an email to `frank@cryptogroup.net`
with your wallet pubkey. The payment tokens you receive are fully blinded
before they are used to pay for Mute services, **there is no way for us to
connect the used tokens to your wallet pubkey**!


### Example usage

For the following commands you either have to enter your passphrase every time:

```
exec 3<`tty`; mutectrl ...
```

Or you can use the [interactive mode](#interactive-mode) described below.

To be able to send and receive messages you have to create a unique _user ID_
(UID) for the `@mute.one` domain first:

```
mutectrl uid new --id your.name@mute.one
```


To be able to write somebody, you have to add him as a contact first:

```
mutectrl contact add --id your.name@mute.one --contact a_friend@mute.one
```

This automatically fetches all the necessary key material.

Now you can add a message to your friend to the outqueue (without actually sending it)

```
mutectrl msg add --from your.name@mute.one --to a_friend@mute.one --file msg.txt
```

Then send (all) messages from the outqueue:

```
mutectrl msg send --id your.name@mute.one
```

To check if your friend wrote you back already use the following commands:

```
mutectrl msg fetch --id your.name@mute.one
mutectrl msg list --id your.name@mute.one
mutectrl msg read --id your.name@mute.one --msgid X
```

(add `help` to a command to get help).

Messages are delayed and mixed with other messages on the server, so do not be
surprised if your message is not delivered instantly.


### Interactive mode

You can also use `mutectrl` in interactive mode:

```
exec 3<`tty`; mutectrl
```

`help` shows you all possible commands and with `quit` you can leave the
interactive mode.

In interactive mode you have an _active user ID_ which is used as the `--id`
argument, if you do not specify it explicitly.
Use `mutectrl uid switch` to switch the active UID.


### Updates

You can automatically update `mutectrl` from source:

```
mutectrl upkeep update
```

Since this is an alpha release the software is evolving at a very high speed,
please update frequently. We enforce an update, if your version is older than
two weeks.


### Backups

`mutectrl` writes its keys and messages to two encrypted databases in the
directory given by option `--homedir` (default: `~/.config/mute`).
To backup your keys and messages, backup the following files in this directory:

```
keys.db
keys.key
msgs.db
msgs.key
```

The `*.db` files are the database files which are encrypted with a random key stored in the corresponding `*.key` file. The `*.key` files are protected by your passphrase.
Make sure you keep backups of **all four** files and do not loose your passphrase!


### Articles

- [Solving the key exchange problem](doc/keyexchangeproblem.md)


### Specification documents

- [Overview](doc/overview.md)
- [Keyserver design](doc/keyserver.md)
- [Example of a complete Keyserver use-case](doc/keyserver-usecase.md)
- [Message protocol](doc/messages.md)
- [Ciphersuites](doc/ciphers.md)
- [Account server](doc/accountserver.md)
