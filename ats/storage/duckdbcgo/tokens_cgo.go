//go:build cgo && rustduckdb

package duckdbcgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../../crates/ats-duckdb/include

#include "duckdb_ffi.h"
#include <stdlib.h>
*/
import "C"

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// Defined once in server/auth, where Caller carries a namespace.
const (
	NamespaceSystem  = auth.NamespaceSystem
	NamespaceDefault = auth.NamespaceDefault
)

// TokenStore is the parquet-backend implementation of auth.TokenStore
// (ADR-025). Tokens are one object each under `<location>/access_tokens/`,
// held by the Rust crate.
//
// The raw token is minted here and never crosses the FFI boundary — only its
// SHA-256 hash does, and only inbound. Nothing below this line can hand a
// working credential back out.
type TokenStore struct {
	ptr unsafe.Pointer // *C.TokenStore
	mu  sync.Mutex
}

// tokenRecord is the wire shape of crates/ats-duckdb/src/tokens.rs.
// Timestamps are Unix milliseconds, matching the attestation path.
type tokenRecord struct {
	ID         string   `json:"id"`
	Hash       string   `json:"hash"`
	Label      string   `json:"label"`
	DID        string   `json:"did"`
	MintedBy         string   `json:"minted_by"`
	MintedByUser     string   `json:"minted_by_user"`
	MintedByUsername string   `json:"minted_by_username"`
	Namespace        string   `json:"namespace"`
	ScopeRead  []string `json:"scope_read"`
	ScopeWrite []string `json:"scope_write"`
	CreatedAt  int64    `json:"created_at"`
	ExpiresAt  *int64   `json:"expires_at,omitempty"`
	LastUsedAt *int64   `json:"last_used_at,omitempty"`
	RevokedAt  *int64   `json:"revoked_at,omitempty"`
}

// tokenSummary is what comes back from a list: the same record without the
// hash. Mirrors TokenSummary in the crate.
type tokenSummary struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	DID        string   `json:"did"`
	MintedBy         string   `json:"minted_by"`
	MintedByUser     string   `json:"minted_by_user"`
	MintedByUsername string   `json:"minted_by_username"`
	Namespace        string   `json:"namespace"`
	ScopeRead  []string `json:"scope_read"`
	ScopeWrite []string `json:"scope_write"`
	CreatedAt  int64    `json:"created_at"`
	ExpiresAt  *int64   `json:"expires_at,omitempty"`
	LastUsedAt *int64   `json:"last_used_at,omitempty"`
	RevokedAt  *int64   `json:"revoked_at,omitempty"`
}

// NewTokenStore opens the token store at a storage location. There is one for
// the deployment: a token record names the namespace it authorizes, because a
// bearer arrives naming none (ADR-027).
func NewTokenStore(location string) (*TokenStore, error) {
	cLocation := C.CString(location)
	defer C.free(unsafe.Pointer(cLocation))

	ptr := C.duckdb_tokens_new(cLocation)
	if ptr == nil {
		return nil, errors.Newf("failed to open the access token store at %s", location)
	}
	return &TokenStore{ptr: unsafe.Pointer(ptr)}, nil
}

// Close releases the Rust-owned store.
func (s *TokenStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ptr != nil {
		C.duckdb_tokens_free((*C.TokenStore)(s.ptr))
		s.ptr = nil
	}
}

// Create issues a token. The raw value is returned once and never stored —
// only its hash reaches the backend, so a leaked store yields nothing usable.
func (s *TokenStore) Create(spec auth.NewToken) (string, string, error) {
	raw, did, err := mintToken()
	if err != nil {
		return "", "", err
	}
	id := uuid.NewString()

	record := tokenRecord{
		ID:         id,
		Hash:       hashToken(raw),
		Label:      spec.Label,
		DID:        did,
		MintedBy:         spec.MintedBy,
		MintedByUser:     spec.MintedByUser,
		MintedByUsername: spec.MintedByUsername,
		Namespace:  spec.Namespace,
		ScopeRead:  emptyIfNil(spec.ScopeRead),
		ScopeWrite: emptyIfNil(spec.ScopeWrite),
		CreatedAt:  time.Now().UTC().UnixMilli(),
	}
	if spec.ExpiresAt != nil {
		ms := spec.ExpiresAt.UTC().UnixMilli()
		record.ExpiresAt = &ms
	}

	body, err := json.Marshal(record)
	if err != nil {
		return "", "", errors.Wrapf(err, "failed to serialize access token %s (%s)", id, spec.Label)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	cBody := C.CString(string(body))
	defer C.free(unsafe.Pointer(cBody))

	result := C.duckdb_tokens_put((*C.TokenStore)(s.ptr), cBody)
	if err := storageResultErr(result, "create access token "+spec.Label); err != nil {
		return "", "", err
	}
	return raw, id, nil
}

// A nil scope and an empty scope have to serialize the same, because the Rust
// side reads an absent list as granting nothing and so must this.
func emptyIfNil(scope []string) []string {
	if scope == nil {
		return []string{}
	}
	return scope
}

// Lookup reports whether the token authorizes a request right now.
//
// A backend failure returns false. The interface has no error to return, so
// the only safe reading of "the store did not answer" is that the credential
// is not good — a store that fails open is a store that authenticates
// everyone the moment it breaks.
func (s *TokenStore) Lookup(hash string) (auth.Grant, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cHash := C.CString(hash)
	defer C.free(unsafe.Pointer(cHash))

	result := C.duckdb_tokens_resolve((*C.TokenStore)(s.ptr), cHash, C.int64_t(time.Now().UTC().UnixMilli()))
	defer C.duckdb_tokens_result_free(result)
	if !bool(result.success) || result.tokens_json == nil {
		return auth.Grant{}, false
	}

	// A live token serializes as an object; `null` is the store saying no such
	// token, which is an answer rather than a failure.
	var resolved *tokenSummary
	if err := json.Unmarshal([]byte(C.GoString(result.tokens_json)), &resolved); err != nil || resolved == nil {
		return auth.Grant{}, false
	}
	return auth.Grant{
		DID:        resolved.DID,
		MintedBy:         resolved.MintedBy,
		MintedByUser:     resolved.MintedByUser,
		MintedByUsername: resolved.MintedByUsername,
		Namespace:  resolved.Namespace,
		ScopeRead:  resolved.ScopeRead,
		ScopeWrite: resolved.ScopeWrite,
	}, true
}

// List returns every token without raw values or hashes.
func (s *TokenStore) List() ([]auth.TokenInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_tokens_list((*C.TokenStore)(s.ptr))
	defer C.duckdb_tokens_result_free(result)

	if !bool(result.success) {
		return nil, errors.Newf("failed to list access tokens: %s", C.GoString(result.error_msg))
	}

	var summaries []tokenSummary
	if err := json.Unmarshal([]byte(C.GoString(result.tokens_json)), &summaries); err != nil {
		return nil, errors.Wrap(err, "failed to parse the access token list from the parquet backend")
	}

	out := make([]auth.TokenInfo, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, auth.TokenInfo{
			ID:         s.ID,
			Label:      s.Label,
			DID:        s.DID,
			MintedBy:         s.MintedBy,
			MintedByUser:     s.MintedByUser,
			MintedByUsername: s.MintedByUsername,
			Namespace:  s.Namespace,
			ScopeRead:  s.ScopeRead,
			ScopeWrite: s.ScopeWrite,
			CreatedAt:  millisToRFC3339(&s.CreatedAt),
			ExpiresAt:  optionalRFC3339(s.ExpiresAt),
			LastUsedAt: optionalRFC3339(s.LastUsedAt),
			RevokedAt:  optionalRFC3339(s.RevokedAt),
		})
	}
	return out, nil
}

// Revoke stops the token authenticating. Durable before it returns.
func (s *TokenStore) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.duckdb_tokens_revoke((*C.TokenStore)(s.ptr), cID, C.int64_t(time.Now().UTC().UnixMilli()))
	return storageResultErr(result, "revoke access token "+id)
}

// Enable lifts a revocation (ADR-025). It does not extend an expiry.
func (s *TokenStore) Enable(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.duckdb_tokens_enable((*C.TokenStore)(s.ptr), cID)
	return storageResultErr(result, "enable access token "+id)
}

// storageResultErr turns a StorageResultC into an error carrying what failed,
// and frees the Rust-owned message either way.
func storageResultErr(result C.StorageResultC, operation string) error {
	defer C.duckdb_storage_result_free(result)
	if bool(result.success) {
		return nil
	}
	message := C.GoString(result.error_msg)
	if message == "" {
		message = "the parquet backend reported failure without a message"
	}
	return errors.Newf("failed to %s: %s", operation, message)
}

// mintToken generates the raw token and the DID it names: 32 random bytes,
// hex-encoded, `qntx_` prefixed (ADR-025:16). The bytes are an ed25519 seed, so
// the token has a public half worth naming and its holder can sign as it.
func mintToken() (string, string, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return "", "", errors.Wrap(err, "failed to read a seed for an access token")
	}
	key := ed25519.NewKeyFromSeed(seed)
	did := auth.EncodeDIDKey(key.Public().(ed25519.PublicKey))
	return "qntx_" + hex.EncodeToString(seed), did, nil
}

// hashToken is the only form of a token that is ever stored.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func millisToRFC3339(ms *int64) string {
	if ms == nil {
		return ""
	}
	return time.UnixMilli(*ms).UTC().Format(time.RFC3339Nano)
}

func optionalRFC3339(ms *int64) *string {
	if ms == nil {
		return nil
	}
	formatted := millisToRFC3339(ms)
	return &formatted
}

// Compile-time proof that this satisfies the contract the middleware holds.
var _ auth.TokenStore = (*TokenStore)(nil)
