package identity_test

import (
	"testing"

	"github.com/sjfehlen/mediamesh/internal/identity"
)

func TestLoadGenerates(t *testing.T) {
	dir := t.TempDir()
	id, err := identity.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(id.Fingerprint) != 64 {
		t.Errorf("fingerprint length = %d, want 64", len(id.Fingerprint))
	}
	if id.PublicKey == nil {
		t.Error("PublicKey is nil")
	}
	if id.PrivateKey == nil {
		t.Error("PrivateKey is nil")
	}
}

func TestLoadIdempotent(t *testing.T) {
	dir := t.TempDir()
	id1, err := identity.Load(dir)
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	id2, err := identity.Load(dir)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if id1.Fingerprint != id2.Fingerprint {
		t.Errorf("fingerprints differ: %s vs %s", id1.Fingerprint, id2.Fingerprint)
	}
}
