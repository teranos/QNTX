//go:build cgo && rustduckdb

package duckdbcgo

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/server/auth"
)

// These run only under the rustduckdb tag, which `make test` does not set
// (Makefile builds with rustsqlite,qntxwasm). ADR-024:91 puts the parquet
// path in CI against file:// — until that exists, this is the only thing
// exercising the FFI, so run it before trusting the backend.

func newStore(t *testing.T) *TokenStore {
	t.Helper()
	store, err := NewTokenStore("file://" + t.TempDir())
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	t.Cleanup(store.Close)
	return store
}

func hashOf(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// The raw token leaves once, and it authenticates.
func TestCreateReturnsAUsableToken(t *testing.T) {
	store := newStore(t)

	raw, id, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespace: NamespaceDefault, ScopeRead: []string{"noted"}, ScopeWrite: []string{"ingested"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(raw, "qntx_") {
		t.Errorf("raw token %q lacks the qntx_ prefix (ADR-025:16)", raw)
	}
	if id == "" {
		t.Error("Create returned an empty id")
	}
	if !store.lookupOK(hashOf(raw)) {
		t.Error("a freshly created token does not authenticate")
	}
}

// The requirement, through the whole stack: revoke it and it is dead.
func TestRevokeKillsTheToken(t *testing.T) {
	store := newStore(t)
	raw, id, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespace: NamespaceDefault, ScopeRead: []string{"noted"}, ScopeWrite: []string{"ingested"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Revoke(id); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if store.lookupOK(hashOf(raw)) {
		t.Error("a revoked token still authenticates")
	}
}

// Revocation is a switch (ADR-025).
func TestEnableBringsItBack(t *testing.T) {
	store := newStore(t)
	raw, id, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespace: NamespaceDefault, ScopeRead: []string{"noted"}, ScopeWrite: []string{"ingested"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Revoke(id); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if err := store.Enable(id); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if !store.lookupOK(hashOf(raw)) {
		t.Error("an enabled token does not authenticate")
	}
}

// A revoke that matched nothing must not read as done.
func TestRevokeUnknownIDFails(t *testing.T) {
	store := newStore(t)
	if err := store.Revoke("no-such-token"); err == nil {
		t.Fatal("revoking an unknown id reported success")
	}
}

// Tokens are objects at the location. Reopening must find them, or every
// credential dies at the next restart.
func TestTokensSurviveReopen(t *testing.T) {
	location := "file://" + t.TempDir()

	first, err := NewTokenStore(location)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	raw, _, err := first.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespace: NamespaceDefault, ScopeRead: []string{"noted"}, ScopeWrite: []string{"ingested"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	first.Close()

	second, err := NewTokenStore(location)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()

	if !second.lookupOK(hashOf(raw)) {
		t.Error("a token did not survive reopening the store")
	}
}

// Neither the raw token nor its hash may reach a list response.
func TestListLeaksNeitherRawNorHash(t *testing.T) {
	store := newStore(t)
	raw, id, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespace: NamespaceDefault, ScopeRead: []string{"noted"}, ScopeWrite: []string{"ingested"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	listed, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("got %d tokens, want 1", len(listed))
	}

	info := listed[0]
	if info.ID != id || info.Label != "laptop-cron" {
		t.Errorf("got id %q label %q, want %q and laptop-cron", info.ID, info.Label, id)
	}
	if info.CreatedAt == "" {
		t.Error("CreatedAt is empty")
	}
	for field, value := range map[string]string{"raw": raw, "hash": hashOf(raw)} {
		if strings.Contains(info.ID+info.Label+info.CreatedAt, value) {
			t.Errorf("the %s value appears in a list response", field)
		}
	}
}

// A revoked token stays listed, carrying the moment it stopped working —
// that is what the UI draws next to the red X.
func TestListKeepsRevokedTokens(t *testing.T) {
	store := newStore(t)
	_, id, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespace: NamespaceDefault, ScopeRead: []string{"noted"}, ScopeWrite: []string{"ingested"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Revoke(id); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	listed, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("got %d tokens, want the revoked one still listed", len(listed))
	}
	if listed[0].RevokedAt == nil {
		t.Error("RevokedAt is nil on a revoked token")
	}
}

// An expiry set at creation has to reach the backend, or a token meant to
// die on its own never does.
func TestExpiredTokenDoesNotAuthenticate(t *testing.T) {
	store := newStore(t)
	past := time.Now().UTC().Add(-time.Hour)

	raw, _, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: &past, MintedBy: "https://mastodon.example/@tim", Namespace: NamespaceDefault, ScopeRead: []string{"noted"}, ScopeWrite: []string{"ingested"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if store.lookupOK(hashOf(raw)) {
		t.Error("a token that expired an hour ago still authenticates")
	}
}

// Two tokens must not collide, and revoking one must not touch the other.
func TestRevokeHitsOnlyItsOwnToken(t *testing.T) {
	store := newStore(t)
	rawA, idA, err := store.Create(auth.NewToken{Label: "a", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespace: NamespaceDefault, ScopeRead: []string{"noted"}, ScopeWrite: []string{"ingested"}})
	if err != nil {
		t.Fatalf("Create a: %v", err)
	}
	rawB, _, err := store.Create(auth.NewToken{Label: "b", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespace: NamespaceDefault, ScopeRead: []string{"noted"}, ScopeWrite: []string{"ingested"}})
	if err != nil {
		t.Fatalf("Create b: %v", err)
	}
	if rawA == rawB {
		t.Fatal("two tokens minted the same raw value")
	}

	if err := store.Revoke(idA); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if store.lookupOK(hashOf(rawA)) {
		t.Error("the revoked token still authenticates")
	}
	if !store.lookupOK(hashOf(rawB)) {
		t.Error("revoking one token killed another")
	}
}

// lookupOK is the yes-or-no a caller asks when all it needs is whether the
// credential is good.
func (s *TokenStore) lookupOK(hash string) bool {
	_, ok := s.Lookup(hash)
	return ok
}
