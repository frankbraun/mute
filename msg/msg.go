// Package msg defines messages in Mute. Specification:
// https://github.com/mutecomm/mute/blob/master/doc/messages.md
package msg

import (
	"github.com/mutecomm/mute/uid"
)

// Version is the current version number of Mute messages.
const Version = 1

// DefaultCiphersuite is the default ciphersuite used for Mute messages.
const DefaultCiphersuite = "CURVE25519 XSALSA20 POLY1305"

// NumOfFutureKeys defines the default number of future message keys which
// are precomputed.
const NumOfFutureKeys = 50

// StoreSession stores a new session.
//
// TODO: document parameters in detail.
type StoreSession func(identity, partner, rootKeyHash, chainKey string,
	send, recv []string) error

// FindKeyEntry defines the type for a function which should return a KeyEntry
// for the given pubKeyHash.
type FindKeyEntry func(pubKeyHash string) (*uid.KeyEntry, error)