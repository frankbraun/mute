package fuzzer

import (
	"fmt"
	"testing"
)

func TestFuzzer(t *testing.T) {
	fuzzer := &SequentialFuzzer{
		Data:     []byte{0x01, 0x01, 0x01, 0x01},
		TestFunc: func([]byte) error { return nil },
	}
	ok := fuzzer.Fuzz()
	if ok {
		t.Error("Fuzz must return false if no errors were generated")
	}
	fuzzer.TestFunc = func([]byte) error { return fmt.Errorf("error") }
	ok = fuzzer.Fuzz()
	if !ok {
		t.Error("Fuzz must not fail, errors found")
	}
}
