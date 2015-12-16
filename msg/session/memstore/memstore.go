// Copyright (c) 2015 Mute Communications Ltd.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package memstore implements a key store in memory (for testing purposes).
package memstore

import (
	"fmt"

	"github.com/mutecomm/mute/encode/base64"
	"github.com/mutecomm/mute/log"
	"github.com/mutecomm/mute/msg/session"
	"github.com/mutecomm/mute/uid"
	"github.com/mutecomm/mute/uid/identity"
)

type memSession struct {
	rootKeyHash string
	chainKey    string
	send        []string
	recv        []string
}

// MemStore implements the KeyStore interface in memory.
type MemStore struct {
	privateKeyEntryMap   map[string]*uid.KeyEntry
	publicKeyEntryMap    map[string]*uid.KeyEntry
	sessionStates        map[string]*session.State
	sessions             map[string]*memSession
	senderSessionPubHash string
}

// New returns a new MemStore.
func New() *MemStore {
	return &MemStore{
		privateKeyEntryMap: make(map[string]*uid.KeyEntry),
		publicKeyEntryMap:  make(map[string]*uid.KeyEntry),
		sessionStates:      make(map[string]*session.State),
		sessions:           make(map[string]*memSession),
	}
}

// SenderSessionPubHash returns the most recent senderSessionPubHash in
// MemStore.
func (ms *MemStore) SenderSessionPubHash() string {
	return ms.senderSessionPubHash
}

// AddPrivateKeyEntry adds private KeyEntry to memory store.
func (ms *MemStore) AddPrivateKeyEntry(ke *uid.KeyEntry) {
	ms.privateKeyEntryMap[ke.HASH] = ke
}

// AddPublicKeyEntry adds public KeyEntry from identity to memory store.
func (ms *MemStore) AddPublicKeyEntry(identity string, ke *uid.KeyEntry) {
	ms.publicKeyEntryMap[identity] = ke
}

// GetSessionState implemented in memory.
func (ms *MemStore) GetSessionState(myID, contactID string) (
	*session.State,
	error,
) {
	return ms.sessionStates[myID+"@"+contactID], nil
}

// SetSessionState implemented in memory.
func (ms *MemStore) SetSessionState(
	myID, contactID string,
	sessionState *session.State,
) error {
	log.Debugf("memstore.SetSessionState(): %s", sessionState.SenderSessionPub.HASH)
	ms.sessionStates[myID+"@"+contactID] = sessionState
	return nil
}

// StoreSession implemented in memory.
func (ms *MemStore) StoreSession(
	myID, contactID, senderSessionPubHash, rootKeyHash, chainKey string,
	send, recv []string,
) error {
	if err := identity.IsMapped(myID); err != nil {
		return log.Error(err)
	}
	if err := identity.IsMapped(contactID); err != nil {
		return log.Error(err)
	}
	if len(send) != len(recv) {
		return log.Error("memstore: len(send) != len(recv)")
	}
	/*
		for i := 0; i < 3; i++ {
			log.Debugf("send[%d]: %s", i, send[i])
			log.Debugf("recv[%d]: %s", i, recv[i])
		}
	*/
	index := myID + "@" + contactID + "@" + senderSessionPubHash
	log.Debugf("memstore.StoreSession(): %s", index)
	ms.sessions[index] = &memSession{
		rootKeyHash: rootKeyHash,
		chainKey:    chainKey,
		send:        send,
		recv:        recv,
	}
	ms.senderSessionPubHash = senderSessionPubHash
	return nil
}

// HasSession implemented in memory.
func (ms *MemStore) HasSession(
	myID, contactID, senderSessionPubHash string,
) bool {
	_, ok := ms.sessions[myID+"@"+contactID+"@"+senderSessionPubHash]
	return ok
}

// GetPrivateKeyEntry implemented in memory.
func (ms *MemStore) GetPrivateKeyEntry(pubKeyHash string) (*uid.KeyEntry, error) {
	ke, ok := ms.privateKeyEntryMap[pubKeyHash]
	if !ok {
		return nil, fmt.Errorf("memstore: could not find key entry %s", pubKeyHash)
	}
	return ke, nil
}

// GetPublicKeyEntry implemented in memory.
func (ms *MemStore) GetPublicKeyEntry(uidMsg *uid.Message) (*uid.KeyEntry, string, error) {
	ke, ok := ms.publicKeyEntryMap[uidMsg.Identity()]
	if !ok {
		return nil, "", log.Error(session.ErrNoKeyInit)
	}
	return ke, "undefined", nil
}

// NumMessageKeys implemented in memory.
func (ms *MemStore) NumMessageKeys(
	myID, contactID, senderSessionPubHash string,
) (uint64, error) {
	index := myID + "@" + contactID + "@" + senderSessionPubHash
	log.Debugf("memstore.GetMessageKey(): %s", index)
	s, ok := ms.sessions[index]
	if !ok {
		return 0, log.Errorf("memstore: no session found for %s and %s",
			myID, contactID)
	}
	return uint64(len(s.send)), nil
}

// GetMessageKey implemented in memory.
func (ms *MemStore) GetMessageKey(
	myID, contactID, senderSessionPubHash string,
	sender bool,
	msgIndex uint64,
) (*[64]byte, error) {
	index := myID + "@" + contactID + "@" + senderSessionPubHash
	log.Debugf("memstore.GetMessageKey(): %s", index)
	s, ok := ms.sessions[index]
	if !ok {
		return nil, log.Errorf("memstore: no session found for %s and %s",
			myID, contactID)
	}
	if msgIndex >= uint64(len(s.send)) {
		return nil, log.Error("memstore: message index out of bounds")
	}
	var key string
	var party string
	if sender {
		key = s.send[msgIndex]
		party = "sender"
	} else {
		key = s.recv[msgIndex]
		party = "recipient"
	}
	// make sure key wasn't used yet
	if key == "" {
		return nil, log.Error(session.ErrMessageKeyUsed)
	}
	// decode key
	var messageKey [64]byte
	k, err := base64.Decode(key)
	if err != nil {
		return nil, log.Errorf("memstore: cannot decode %s key for %s and %s: ",
			party, myID, contactID)
	}
	if copy(messageKey[:], k) != 64 {
		return nil, log.Errorf("memstore: %s key for %s and %s has wrong length",
			party, myID, contactID)
	}
	return &messageKey, nil
}

// GetRootKeyHash implemented in memory.
func (ms *MemStore) GetRootKeyHash(
	myID, contactID, senderSessionPubHash string,
) (*[64]byte, error) {
	index := myID + "@" + contactID + "@" + senderSessionPubHash
	log.Debugf("memstore.GetRootKeyHash(): %s", index)
	s, ok := ms.sessions[index]
	if !ok {
		return nil, log.Errorf("memstore: no session found for %s and %s",
			myID, contactID)
	}
	// decode root key hash
	var hash [64]byte
	k, err := base64.Decode(s.rootKeyHash)
	if err != nil {
		return nil, log.Error("memstore: cannot decode root key hash")
	}
	if copy(hash[:], k) != 64 {
		return nil, log.Errorf("memstore: root key hash has wrong length")
	}
	return &hash, nil
}

// DelMessageKey implemented in memory.
func (ms *MemStore) DelMessageKey(
	myID, contactID, senderSessionPubHash string,
	sender bool,
	msgIndex uint64,
) error {
	index := myID + "@" + contactID + "@" + senderSessionPubHash
	log.Debugf("memstore.DelMessageKey(): %s", index)
	s, ok := ms.sessions[index]
	if !ok {
		return log.Errorf("memstore: no session found for %s and %s",
			myID, contactID)
	}
	if msgIndex >= uint64(len(s.send)) {
		return log.Error("memstore: message index out of bounds")
	}
	// delete key
	if sender {
		s.send[msgIndex] = ""
	} else {
		s.recv[msgIndex] = ""
	}
	return nil
}