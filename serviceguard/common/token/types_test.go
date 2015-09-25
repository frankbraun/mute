package token

import (
	"bytes"
	"testing"

	"github.com/mutecomm/mute/serviceguard/common/signkeys"

	"github.com/agl/ed25519"
)

func TestNew(t *testing.T) {
	keyID := [signkeys.KeyIDSize]byte{0x01, 0x03, 0x01}
	owner := [ed25519.PublicKeySize]byte{0x00, 0x15, 0xff}
	tkn := New(&keyID, nil)
	if tkn.HasOwner() {
		t.Error("Token should NOT have an owner")
	}
	hsh := tkn.Hash()
	tkn = New(&keyID, &owner)
	if !tkn.HasOwner() {
		t.Error("Token should have an owner")
	}
	keyIDr, ownerr := tkn.Properties()
	if *keyIDr != keyID {
		t.Error("KeyID mismatch")
	}
	if *ownerr != owner {
		t.Error("Owner mismatch")
	}
	hsh1 := tkn.Hash()
	m, err := tkn.Marshal()
	if err != nil {
		t.Errorf("Marshal error: %s", err)
	}
	tkn2, err := Unmarshal(m)
	if err != nil {
		t.Errorf("Unmarshal error: %s", err)
	}
	hsh2 := tkn2.Hash()
	if bytes.Equal(hsh, hsh1) {
		t.Error("hsh and hsh1 must differ")
	}
	if !bytes.Equal(hsh1, hsh2) {
		t.Error("hsh and hsh2 must match")
	}
}
