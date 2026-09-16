package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

// Grant is what a token turns out to be once resolved: whose it is and where
// it may act. Which predicates it may touch is not on the credential: the
// roles its DID holds say, through their WRITE and READ lines (ADR-034).
type Grant struct {
	// Label is the token's name: what a grant names when it hands the token a
	// role, the way a route names a person. One live token per label.
	Label string `json:"label"`
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
	// ReturnAddress is where a client's codes are sent (ADR-030: a door is a
	// return address). Written at minting by the same hand that writes a door
	// in am.toml. Empty on every kind but OAUTH.
	ReturnAddress string `json:"return_address,omitempty"`
}

// Namespace is what a word ends in to mean every predicate under it: `tag:`
// is every tag there will ever be.
//
// The colon is written down rather than inferred, so a line that says `type`
// says type and nothing that merely starts with it. Widening is a word somebody
// wrote, which is the same shape as `all` on a READ line.
const Namespace = ":"

func permits(words []string, predicate string) bool {
	for _, word := range words {
		if word == predicate {
			return true
		}
		if strings.HasSuffix(word, Namespace) && strings.HasPrefix(predicate, word) {
			return true
		}
	}
	return false
}

// Permits reports whether these words permit this predicate, exactly or by the
// namespace one of them names. What MayRead and MayWrite ask, asked by a caller
// holding the words rather than the admission — narrowing a query is the same
// question about the same list.
func Permits(words []string, predicate string) bool { return permits(words, predicate) }

// Names reports whether any of these words is a namespace rather than one
// predicate. A namespace has no literal list — `tag:` is every tag there will
// ever be — so a read narrowed by one is filtered after the store answers
// rather than handed to it as a filter.
func Names(words []string) bool {
	for _, word := range words {
		if strings.HasSuffix(word, Namespace) {
			return true
		}
	}
	return false
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
	// ReturnAddress is a client's, and only a client's.
	ReturnAddress string
}

// IssuedToken is a token the flow minted (token_strategy.go), written down.
// The strategy drew the raw and named its DID where the raw existed; the
// store is handed the hash and the DID and never the raw, the same as Create
// keeps.
type IssuedToken struct {
	Hash  string
	DID   string
	Label string
	// Who said yes at the door, carried on the session (ADR-025).
	MintedBy            string
	MintedByUser        string
	MintedByDisplayName string
	Level               Level
	Namespaces          []string
	ExpiresAt           *time.Time
}

// TokenStore is the full access-token contract used by middleware and the
// /auth/tokens endpoints. See ADR-025.
type TokenStore interface {
	// Lookup resolves a token hash to what it grants. False means no live token
	// has this hash — revoked, expired, unknown, or the store did not answer.
	Lookup(hash string) (Grant, bool)
	// Create issues a new token. The raw token is returned once — never stored.
	Create(spec NewToken) (raw, id string, err error)
	// Issue writes down a token the flow already minted: fosite exchanges the
	// code for the token the strategy already mints, and the store writes it
	// with the DID the session carries.
	Issue(spec IssuedToken) (id string, err error)
	// List returns all tokens without raw values or hashes.
	List() ([]TokenInfo, error)
	// Revoke marks a token revoked, so Lookup rejects it. Idempotent, and
	// durable before it returns.
	Revoke(id string) error
	// Enable lifts a revocation. Revocation is a switch: kill the token,
	// watch whether anything is still presenting it, turn it back on if that
	// was you. Idempotent. Does not extend an expiry.
	Enable(id string) error
	// Touch records that the token with this hash was presented now. It is
	// what "watch whether anything is still presenting it" reads.
	Touch(hash string) error
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
	ReturnAddress       string   `json:"return_address,omitempty"`
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
	if strings.HasPrefix(h, prefix) {
		raw := strings.TrimSpace(h[len(prefix):])
		if raw != "" {
			return raw, true
		}
	}
	if proto, ok := SocketBearer(r); ok {
		return strings.TrimPrefix(proto, socketBearerPrefix), true
	}
	return "", false
}

// socketBearerPrefix marks the one subprotocol a browser socket can carry a
// token in. A browser's WebSocket sets no header of its own, so an app that
// holds its session as a bearer names it here, and the node echoes the
// protocol back or the browser closes the handshake.
const socketBearerPrefix = "bearer."

// SocketBearer is the bearer subprotocol a socket handshake offered, verbatim,
// so the upgrade can select it. Absent when the handshake offered none.
func SocketBearer(r *http.Request) (string, bool) {
	for _, offered := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		offered = strings.TrimSpace(offered)
		if strings.HasPrefix(offered, socketBearerPrefix) && len(offered) > len(socketBearerPrefix) {
			return offered, true
		}
	}
	return "", false
}
