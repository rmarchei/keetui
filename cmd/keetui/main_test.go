package main

import (
	"testing"
)

func TestZeroBytes(t *testing.T) {
	b := []byte{1, 2, 3, 4, 5}
	zeroBytes(b)
	for i, v := range b {
		if v != 0 {
			t.Errorf("b[%d] = %d, want 0", i, v)
		}
	}
}

func TestZeroBytesEmpty(t *testing.T) {
	b := []byte{}
	zeroBytes(b) // should not panic
}

func TestZeroBytesNil(t *testing.T) {
	zeroBytes(nil) // should not panic
}

func TestSelfHash(t *testing.T) {
	h, err := selfHash()
	if err != nil {
		t.Fatalf("selfHash: %v", err)
	}
	if len(h) != 64 { // SHA256 hex is 64 chars
		t.Errorf("hash length = %d, want 64", len(h))
	}
}
