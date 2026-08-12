//go:build cgo && rustduckdb

package duckdbcgo

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"

	"github.com/teranos/QNTX/server/nodedid"
)

// Same caveat as tokens_cgo_test.go: `make test` builds with rustsqlite, not
// rustduckdb, so this is the only thing exercising the FFI for node identity.

// nodedid must be able to use whatever this is.
var _ nodedid.IdentityStore = (*IdentityStore)(nil)

func newIdentityStore(t *testing.T) *IdentityStore {
	t.Helper()
	store, err := NewIdentityStore("file://" + t.TempDir())
	if err != nil {
		t.Fatalf("NewIdentityStore: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

// First boot has no identity, and that is not a failure. nodedid.NewWithStore
// reads nil as "generate one" — an error here would abort startup instead.
func TestLoadOnAFreshLocationReturnsNothing(t *testing.T) {
	store := newIdentityStore(t)

	id, err := store.Load()
	if err != nil {
		t.Fatalf("Load on a fresh location: %v", err)
	}
	if id != nil {
		t.Errorf("a fresh location yielded an identity: %+v", id)
	}
}

// Namespace is the top-level prefix in a location. ADR-026 makes the system
// namespace the node, so its identity lands under that prefix — never at the
// root, where a later namespace would have nowhere to go beside it.
func TestIdentityLandsUnderTheSystemNamespace(t *testing.T) {
	dir := t.TempDir()

	store, err := NewIdentityStore("file://" + dir)
	if err != nil {
		t.Fatalf("NewIdentityStore: %v", err)
	}
	t.Cleanup(store.Close)

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if err := store.Save(&nodedid.Identity{PrivateKey: priv, PublicKey: pub, DID: "did:key:ztest"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "system"))
	if err != nil {
		t.Fatalf("nothing was written under the system namespace: %v", err)
	}
	if len(entries) == 0 {
		t.Error("the system namespace prefix is empty after Save")
	}
}

// The node's signing key has to survive a restart. If it does not, every
// attestation the node ever signed becomes unverifiable against its DID.
func TestIdentitySurvivesReopen(t *testing.T) {
	location := "file://" + t.TempDir()

	first, err := NewIdentityStore(location)
	if err != nil {
		t.Fatalf("NewIdentityStore: %v", err)
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	saved := &nodedid.Identity{
		PrivateKey: priv,
		PublicKey:  pub,
		DID:        "did:key:ztest",
	}
	if err := first.Save(saved); err != nil {
		t.Fatalf("Save: %v", err)
	}
	first.Close()

	second, err := NewIdentityStore(location)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(second.Close)

	loaded, err := second.Load()
	if err != nil {
		t.Fatalf("Load after reopen: %v", err)
	}
	if loaded == nil {
		t.Fatal("the identity did not survive reopening the location")
	}
	if loaded.DID != saved.DID {
		t.Errorf("DID = %q, want %q", loaded.DID, saved.DID)
	}
	if !bytes.Equal(loaded.PrivateKey, saved.PrivateKey) {
		t.Error("the private key did not round-trip")
	}
	if !bytes.Equal(loaded.PublicKey, saved.PublicKey) {
		t.Error("the public key did not round-trip")
	}
}
