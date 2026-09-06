package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Grant is what a token turns out to be once resolved: whose it is and where
// it may act. Which predicates it may touch is not on the credential: the
// roles its DID holds say, through their WRITE and READ lines (ADR-034).
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
	// Level is what kind of token this is, chosen at minting.
	Level Level `json:"level,omitempty"`
	// Namespaces is where the token may act, named by the record rather than by
	// the path it was found under.
	Namespaces []string `json:"namespaces"`
}

func permits(words []string, predicate string) bool {
	return slices.Contains(words, predicate)
}

// Scoped reports whether the lines are what say how far this token reaches.
//
// A SUPER token is not scoped: it is ROOT handing its own reach to a token it
// made, and the kind that does pretty much everything does all of it.
func (g Grant) Scoped() bool {
	return g.Level != LevelSuper
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
	// Level is which kind of token to mint, and the mint says which.
	Level      Level
	Namespaces []string
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
	Level               Level    `json:"level,omitempty"`
	Namespaces          []string `json:"namespaces"`
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
