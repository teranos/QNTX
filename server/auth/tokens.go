package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Grant is what a token turns out to be once resolved: whose it is, where it
// may act, and which predicates it may touch. A bool could carry none of this,
// which is why Lookup stopped returning one.
type Grant struct {
	// DID is the token's own did:key. The raw token is the ed25519 seed behind
	// it, so a holder can sign as this DID rather than only present a string.
	DID string `json:"did"`
	// MintedBy is the root_identities entry whose session issued the token.
	MintedBy string `json:"minted_by"`
	// MintedByUser and MintedByDisplayName are the person that entry reaches,
	// resolved when the token was minted rather than on every use (ADR-031).
	// A token speaks on behalf of a person; this is who.
	MintedByUser        string `json:"minted_by_user"`
	MintedByDisplayName string `json:"minted_by_display_name"`
	// Namespace is where the token may act, named by the record rather than by
	// the path it was found under.
	Namespace string `json:"namespace"`
	// ScopeRead and ScopeWrite are predicates. Empty grants nothing, so a token
	// issued without scope can do nothing rather than everything.
	ScopeRead  []string `json:"scope_read"`
	ScopeWrite []string `json:"scope_write"`
}

// ScopeAll is a scope naming every predicate. A token predating scoping was
// unrestricted, and without a way to say so there is no record that gives one
// back what it had.
const ScopeAll = "*"

func permits(scope []string, predicate string) bool {
	return slices.Contains(scope, ScopeAll) || slices.Contains(scope, predicate)
}

// MayRead reports whether this token may read attestations with a predicate.
func (g Grant) MayRead(predicate string) bool {
	return permits(g.ScopeRead, predicate)
}

// MayWrite reports whether this token may write attestations with a predicate.
func (g Grant) MayWrite(predicate string) bool {
	return permits(g.ScopeWrite, predicate)
}

// Unrestricted reports whether this token is scoped to everything, which is
// what a query with no predicate filter has to be left alone for.
func (g Grant) Unrestricted() bool {
	return slices.Contains(g.ScopeRead, ScopeAll)
}

// NewToken is what the caller asks for when minting one.
type NewToken struct {
	Label     string
	ExpiresAt *time.Time
	MintedBy  string
	// Who MintedBy reaches, taken from the minting session rather than looked
	// up, so nothing scans the User store to issue a token.
	MintedByUser        string
	MintedByDisplayName string
	Namespace           string
	ScopeRead           []string
	ScopeWrite          []string
}

// TokenStore is the full access-token contract used by middleware and the
// /auth/tokens endpoints. See ADR-025.
type TokenStore interface {
	// Lookup resolves a token hash to what it grants. False means no live token
	// has this hash — revoked, expired, unknown, or the store did not answer.
	Lookup(hash string) (Grant, bool)
	// Create issues a new token. The raw token is returned once — never stored.
	Create(spec NewToken) (raw, id string, err error)
	// List returns all tokens without raw values or hashes.
	List() ([]TokenInfo, error)
	// Revoke marks a token revoked, so Lookup rejects it. Idempotent, and
	// durable before it returns — a revocation that a restart could undo is
	// worse than none, because it reads as done.
	Revoke(id string) error
	// Enable lifts a revocation. Revocation is a switch: kill the token,
	// watch whether anything is still presenting it, turn it back on if that
	// was you. Idempotent. Does not extend an expiry.
	Enable(id string) error
}

// TokenInfo is the safe-to-return shape for GET /auth/tokens.
type TokenInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// A DID is a public key, so naming it is what lets a signature made by this
	// token be traced back to the token that made it.
	DID      string `json:"did"`
	MintedBy string `json:"minted_by"`
	// Who minted it, rather than which of their routes they used.
	MintedByUser        string   `json:"minted_by_user,omitempty"`
	MintedByDisplayName string   `json:"minted_by_display_name,omitempty"`
	Namespace           string   `json:"namespace"`
	ScopeRead           []string `json:"scope_read"`
	ScopeWrite          []string `json:"scope_write"`
	CreatedAt           string   `json:"created_at"`
	ExpiresAt           *string  `json:"expires_at,omitempty"`
	LastUsedAt          *string  `json:"last_used_at,omitempty"`
	RevokedAt           *string  `json:"revoked_at,omitempty"`
}

// sha256Hex hashes a raw access token to the form stored in TokenStore.
func sha256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header. Returns the raw token and true when present, empty string and
// false otherwise.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	raw := strings.TrimSpace(h[len(prefix):])
	if raw == "" {
		return "", false
	}
	return raw, true
}
