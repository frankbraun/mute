// Copyright (c) 2015 Mute Communications Ltd.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package session defines session states and session stores in Mute.
package session

import (
	"github.com/mutecomm/mute/cipher"
	"github.com/mutecomm/mute/encode/base64"
	"github.com/mutecomm/mute/uid"
)

// State describes the current session state between two communicating parties.
type State struct {
	SenderSessionCount          uint64        // total number of messages sent in sessions before this SenderSessionPub was used
	SenderMessageCount          uint64        // total number of messages sent with this SenderSessionPub
	MaxRecipientCount           uint64        // the highest SenderMessageCount from recipient (for message key timeouts)
	RecipientTemp               uid.KeyEntry  // RecipientKeyInitPub or RecipientSessionPub
	SenderSessionPub            uid.KeyEntry  // public session key from sender
	NextSenderSessionPub        *uid.KeyEntry // new SenderSessionPub to refresh the session
	NextRecipientSessionPubSeen *uid.KeyEntry // currently known NextSenderSessionPub of the other party
	NymAddress                  string        // current NymAddress from recipient
	KeyInitSession              bool          // this session was started with a KeyInit message
}

// StateEqual returns a boolean reporting whether a and b have the same exported fields.
func StateEqual(a, b *State) bool {
	if a == b {
		return true
	}
	if a.SenderSessionCount != b.SenderSessionCount {
		return false
	}
	if a.SenderMessageCount != b.SenderMessageCount {
		return false
	}
	if a.MaxRecipientCount != b.MaxRecipientCount {
		return false
	}
	if !uid.KeyEntryEqual(&a.RecipientTemp, &b.RecipientTemp) {
		return false
	}
	if !uid.KeyEntryEqual(&a.SenderSessionPub, &b.SenderSessionPub) {
		return false
	}
	if !uid.KeyEntryEqual(a.NextSenderSessionPub, b.NextSenderSessionPub) {
		return false
	}
	if !uid.KeyEntryEqual(a.NextRecipientSessionPubSeen,
		b.NextRecipientSessionPubSeen) {
		return false
	}
	if a.NymAddress != b.NymAddress {
		return false
	}
	if a.KeyInitSession != b.KeyInitSession {
		return false
	}
	return true
}

// The Store interface defines all methods for managing session keys.
type Store interface {
	// GetSessionState returns the current session state or nil, if no state
	// exists between the two parties.
	GetSessionState(sessionStateKey string) (*State, error)
	// SetSesssionState sets the current session state between two parties.
	SetSessionState(sessionStateKey string, sessionState *State) error
	// StoreSession stores a new session.
	// rootKeyHash is the base64 encoded root key hash.
	// chainKey is the base64 encoded chain key.
	// send and recv are arrays containing NumOfFutureKeys many base64 encoded
	// future keys.
	StoreSession(sessionKey, rootKeyHash, chainKey string,
		send, recv []string) error
	// HasSession returns a boolean reporting whether a session exists.
	HasSession(sessionKey string) bool
	// GetPublicKeyInit returns the private KeyEntry contained in the KeyInit
	// message with the given pubKeyHash.
	// If no such KeyEntry is available, ErrNoKeyEntry is returned.
	GetPrivateKeyEntry(pubKeyHash string) (*uid.KeyEntry, error)
	// GetPrivateKeyInit returns a public KeyEntry and NYMADDRESS contained in
	// the KeyInit message for the given uidMsg.
	// If no such KeyEntry is available, ErrNoKeyEntry is returned.
	GetPublicKeyEntry(uidMsg *uid.Message) (*uid.KeyEntry, string, error)
	// GetMessageKey returns the message key with index msgIndex. If sender is
	// true the sender key is returned, otherwise the recipient key.
	GetMessageKey(sessionKey string, sender bool,
		msgIndex uint64) (*[64]byte, error)
	// NumMessageKeys returns the number of precomputed messages keys.
	NumMessageKeys(sessionKey string) (uint64, error)
	// GetRootKeyHash returns the root key hash for the session.
	GetRootKeyHash(sessionKey string) (*[64]byte, error)
	// GetChainKey returns the chain key for the session.
	GetChainKey(sessionKey string) (*[32]byte, error)
	// DelMessageKey deleted the message key with index msgIndex. If sender is
	// true the sender key is deleted, otherwise the recipient key.
	DelMessageKey(sessionKey string, sender bool, msgIndex uint64) error

	// AddSessionKey adds a session key.
	AddSessionKey(hash, json, privKey string, cleanupTime uint64) error
	// GetSessionKey returns a session key.
	// If no such session key is available, ErrNoKeyEntry is returned.
	GetSessionKey(hash string) (json, privKey string, err error)
	// DelPrivSessionKey deletes the private key of a session key.
	// It should not fail if no public key for the hash exists or if the
	// private key has already been deleted.
	DelPrivSessionKey(hash string) error
	// CleanupSessionKeys deletes all session keys with a cleanup time before t.
	CleanupSessionKeys(t uint64) error
}

// CalcStateKey computes the session state key from senderIdentityPub and
// recipientIdentityPub.
func CalcStateKey(senderIdentityPub, recipientIdentityPub *[32]byte) string {
	key := append(senderIdentityPub[:], recipientIdentityPub[:]...)
	return base64.Encode(cipher.SHA512(key))
}

// CalcKey computes the session key from senderIdentityHash,
// recipientIdentityHash, senderSessionHash, and recipientSessionHash.
func CalcKey(
	senderIdentityHash string,
	recipientIdentityHash string,
	senderSessionHash string,
	recipientSessionHash string,
) string {
	key := senderIdentityHash + recipientIdentityHash
	key += senderSessionHash + recipientSessionHash
	return base64.Encode(cipher.SHA512([]byte(key)))
}
