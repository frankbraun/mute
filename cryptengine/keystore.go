// Copyright (c) 2015 Mute Communications Ltd.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cryptengine

import (
	"github.com/mutecomm/mute/log"
	"github.com/mutecomm/mute/msg"
	"github.com/mutecomm/mute/uid"
	"github.com/mutecomm/mute/util"
)

// GetSessionState implements corresponding method for msg.KeyStore interface.
func (ce *CryptEngine) GetSessionState(myID, contactID string) (
	*msg.SessionState,
	error,
) {
	return nil, util.ErrNotImplemented
}

// SetSessionState implements corresponding method for msg.KeyStore interface.
func (ce *CryptEngine) SetSessionState(
	myID, contactID string,
	sessionState *msg.SessionState,
) error {
	return util.ErrNotImplemented
}

// StoreSession implements corresponding method for msg.KeyStore interface.
func (ce *CryptEngine) StoreSession(
	myID, contactID, rootKeyHash, chainKey string,
	send, recv []string,
) error {
	return ce.keyDB.AddSession(myID, contactID, rootKeyHash, chainKey, send, recv)
}

// FindKeyEntry implements corresponding method for msg.KeyStore interface.
func (ce *CryptEngine) FindKeyEntry(pubKeyHash string) (*uid.KeyEntry, error) {
	log.Debugf("ce.FindKeyEntry: pubKeyHash=%s", pubKeyHash)
	ki, sigPubKey, privateKey, err := ce.keyDB.GetPrivateKeyInit(pubKeyHash)
	if err != nil {
		return nil, err
	}
	// decrypt KeyEntry
	ke, err := ki.KeyEntryECDHE25519(sigPubKey)
	if err != nil {
		return nil, err
	}
	// set private key
	if err := ke.SetPrivateKey(privateKey); err != nil {
		return nil, err
	}
	return ke, nil
}

// GetMessageKey implements corresponding method for msg.KeyStore interface.
func (ce *CryptEngine) GetMessageKey(
	myID, contactID string,
	sender bool,
	msgIndex uint64,
) (*[64]byte, error) {
	return nil, util.ErrNotImplemented
}

// DelMessageKey implements corresponding method for msg.KeyStore interface.
func (ce *CryptEngine) DelMessageKey(
	myID, contactID string,
	sender bool,
	msgIndex uint64,
) error {
	return util.ErrNotImplemented
}