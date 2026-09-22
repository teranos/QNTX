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

// A client's return address is where its codes go (ADR-025). It rides the
// record through the FFI and back, and survives reopen.
func TestAClientKeepsItsReturnAddress(t *testing.T) {
	dir := "file://" + t.TempDir()
	first, err := NewTokenStore(dir)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	raw, _, err := first.Create(auth.NewToken{
		Label: "app", MintedBy: "https://mastodon.example/@tim", Level: auth.LevelOAuth,
		Namespaces: []string{NamespaceDefault}, ReturnAddress: "https://app.example/callback",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	grant, ok := first.Lookup(hashOf(raw))
	if !ok {
		t.Fatal("the client did not resolve")
	}
	if grant.Level != auth.LevelOAuth || grant.ReturnAddress != "https://app.example/callback" {
		t.Fatalf("resolved as %s at %q", grant.Level, grant.ReturnAddress)
	}
	first.Close()

	second, err := NewTokenStore(dir)
	if err != nil {
		t.Fatalf("NewTokenStore (reopen): %v", err)
	}
	defer second.Close()
	grant, ok = second.Lookup(hashOf(raw))
	if !ok || grant.ReturnAddress != "https://app.example/callback" {
		t.Fatalf("after reopen the client resolved as %v at %q", ok, grant.ReturnAddress)
	}
	listed, err := second.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].ReturnAddress != "https://app.example/callback" {
		t.Fatalf("the list says %+v", listed)
	}
}

// fosite exchanges the code for the token the strategy already mints, and the
// store writes it with the DID the session carries (ADR-025). What it wrote
// is found by the hash the strategy named it by, as a token of the kind and
// namespace it was issued in.
func TestAnIssuedTokenIsFoundByItsHash(t *testing.T) {
	store := newStore(t)
	raw, did, err := auth.MintToken()
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	expires := time.Now().UTC().Add(time.Hour)
	id, err := store.Issue(auth.IssuedToken{
		Hash: hashOf(raw), DID: did, Label: "app", MintedBy: "https://mastodon.example/@tim",
		MintedByUser: "US-1", Level: auth.LevelAttestor, Namespaces: []string{NamespaceDefault},
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if id == "" {
		t.Fatal("Issue returned an empty id")
	}
	grant, ok := store.Lookup(hashOf(raw))
	if !ok {
		t.Fatal("the issued token does not authenticate")
	}
	if grant.DID != did || grant.Level != auth.LevelAttestor || grant.MintedByUser != "US-1" {
		t.Fatalf("resolved as %+v", grant)
	}
	listed, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != id || listed[0].Label != "app" || listed[0].ExpiresAt == nil {
		t.Fatalf("the list says %+v", listed)
	}
}

// A token that names no namespace acts in every namespace its person reaches,
// which is what a connector's token is. It is written down all the same: a nil
// list marshals as null, and the store reads a list.
func TestATokenNamingNoNamespaceIsWritten(t *testing.T) {
	store := newStore(t)
	raw, did, err := auth.MintToken()
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	if _, err := store.Issue(auth.IssuedToken{
		Hash: hashOf(raw), DID: did, Label: "connector", MintedBy: "https://mastodon.example/@tim",
		Level: auth.LevelRoot,
	}); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	grant, ok := store.Lookup(hashOf(raw))
	if !ok {
		t.Fatal("the issued token does not authenticate")
	}
	if len(grant.Namespaces) != 0 {
		t.Fatalf("a token that named no namespace resolved in %v", grant.Namespaces)
	}
}

// The raw token leaves once, and it authenticates.
func TestCreateReturnsAUsableToken(t *testing.T) {
	store := newStore(t)

	raw, id, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespaces: []string{NamespaceDefault}})
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

// A use is recorded on the record and read back off the list, through the
// FFI: last used is what a revocation is watched by (ADR-025).
func TestTouchIsReadBackAsLastUsed(t *testing.T) {
	store := newStore(t)
	raw, id, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespaces: []string{NamespaceDefault}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	before, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if before[0].LastUsedAt != nil {
		t.Fatalf("a token never presented reads as used at %s", *before[0].LastUsedAt)
	}

	if err := store.Touch(hashOf(raw)); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	after, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if after[0].ID != id || after[0].LastUsedAt == nil {
		t.Fatalf("the use was not recorded on %s: %+v", id, after[0])
	}
}

// The requirement, through the whole stack: revoke it and it is dead.
func TestRevokeKillsTheToken(t *testing.T) {
	store := newStore(t)
	raw, id, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespaces: []string{NamespaceDefault}})
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
	raw, id, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespaces: []string{NamespaceDefault}})
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
	raw, _, err := first.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespaces: []string{NamespaceDefault}})
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
	raw, id, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespaces: []string{NamespaceDefault}})
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
	_, id, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespaces: []string{NamespaceDefault}})
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

	raw, _, err := store.Create(auth.NewToken{Label: "laptop-cron", ExpiresAt: &past, MintedBy: "https://mastodon.example/@tim", Namespaces: []string{NamespaceDefault}})
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
	rawA, idA, err := store.Create(auth.NewToken{Label: "a", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespaces: []string{NamespaceDefault}})
	if err != nil {
		t.Fatalf("Create a: %v", err)
	}
	rawB, _, err := store.Create(auth.NewToken{Label: "b", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespaces: []string{NamespaceDefault}})
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

// The token store asks its location for things nothing else counts: a token
// is rewritten on every use (ADR-037), and that PUT is invisible while
// Requests reaches only the attestation store's tally. A file:// location
// asks for nothing, so what this proves is the seam, not a number.
func TestTheTokenStoreSaysWhatItAskedFor(t *testing.T) {
	store := newStore(t)
	if _, _, err := store.Create(auth.NewToken{Label: "counted", ExpiresAt: nil, MintedBy: "https://mastodon.example/@tim", Namespaces: []string{NamespaceDefault}}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	asked, err := store.Requests()
	if err != nil {
		t.Fatalf("Requests: %v", err)
	}
	for _, one := range asked {
		if one.HeldOnNode {
			t.Errorf("the token store counted %s as held on the node; make parity says access_tokens is NO on the node", one.Of)
		}
	}
}

// lookupOK is the yes-or-no a caller asks when all it needs is whether the
// credential is good.
func (s *TokenStore) lookupOK(hash string) bool {
	_, ok := s.Lookup(hash)
	return ok
}
