// Copyright (c) 2015 Mute Communications Ltd.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package walletauth

import (
	"crypto/rand"
	"math"
	"testing"

	"crypto/ed25519"
	"github.com/mutecomm/mute/util/times"
)

func TestCheckToken(t *testing.T) {
	now := uint64(times.Now()) / SkewWindow
	pubkey, privkey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Key generation failed: %s", err)
	}
	authtoken := CreateToken(pubkey, privkey, 1)
	pubkey2, ltime, lcounter, err := authtoken.CheckToken()
	if err != nil {
		t.Errorf("Token verification failed: %s", err)
	}
	if *pubkey2 != *pubkey {
		t.Errorf("Token decode failed, pubkey does not match: %x!=%x", *pubkey2, *pubkey)
	}
	if lcounter != 1 {
		t.Errorf("Token decode failed. Counter %d!=1", lcounter)
	}
	if uint64(math.Abs(float64(ltime-now))) > 1 {
		t.Errorf("Token decode failed. Time: %d!=%d", now, ltime)
	}
	authtoken[0] = 0x00
	_, _, _, err = authtoken.CheckToken()
	if err == nil {
		t.Error("Token verification MUST fail")
	}
}
