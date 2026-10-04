package access

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/teranos/errors"
)

// TokenPrefix marks a raw token as this node's (ADR-025:16).
const TokenPrefix = "qntx_"

// TokenSeedBytes is the length of the random half: 32 bytes, an ed25519 seed,
// so the token has a public half worth naming.
const TokenSeedBytes = ed25519.SeedSize

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
	// ClientDID is the client this token was issued through, which a refresh
	// spent at the token endpoint has to be checked against: fosite compares
	// it to the client that authenticated. Empty on a token no flow issued.
	ClientDID string `json:"client_did,omitempty"`
	// RequestID is the fosite request this token was issued under, so a
	// rotation naming the request can find what it issued.
	RequestID string `json:"request_id,omitempty"`
	// ID is the record's own id, which is what Revoke names.
	ID string `json:"id,omitempty"`
}

// Scoped reports whether the lines are what say how far this token reaches.
//
// A SUPER token is not scoped: it is ROOT handing its own reach to a token it
// made, and the kind that does pretty much everything does all of it.
func (g Grant) Scoped() bool {
	// ROOT's own kind is the ROOT agent's token alone (ADR-048), and it is
	// narrowed by no line either.
	return g.Level != LevelSuper && g.Level != LevelRoot
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
	// ClientDID is the client the flow issued this through.
	ClientDID string
	// RequestID is the fosite request this was issued under. Rotation and
	// revocation arrive naming it rather than a hash, so the record carries
	// it or the two cannot be joined after a restart.
	RequestID string
}

// TokenRecord is a token whole, hash included — what the operational db holds
// and what the record holds, in one shape so the two can be compared.
//
// The hash is not the token. The raw is an ed25519 seed the holder keeps and
// this node never stores; the hash is what a presented raw is reduced to by
// sha256Hex on the way in, and what every lookup is keyed by.
type TokenRecord struct {
	ID                  string   `json:"id"`
	Hash                string   `json:"hash"`
	Label               string   `json:"label"`
	DID                 string   `json:"did"`
	MintedBy            string   `json:"minted_by"`
	MintedByUser        string   `json:"minted_by_user"`
	MintedByDisplayName string   `json:"minted_by_display_name"`
	Level               string   `json:"level"`
	Namespaces          []string `json:"namespaces"`
	ReturnAddress       string   `json:"return_address"`
	ClientDID           string   `json:"client_did"`
	RequestID           string   `json:"request_id"`
	ScopeRead           []string `json:"scope_read"`
	ScopeWrite          []string `json:"scope_write"`
	CreatedAt           int64    `json:"created_at"`
	ExpiresAt           *int64   `json:"expires_at,omitempty"`
	LastUsedAt          *int64   `json:"last_used_at,omitempty"`
	RevokedAt           *int64   `json:"revoked_at,omitempty"`
	// GitHub is what a GITHUB token holds and no other kind does: the
	// credential itself, which the node spends at GitHub (ADR-043).
	GitHub *GitHubSecret `json:"github,omitempty"`
}

// Usable reports whether this token authorizes a request at nowMS. Revoked is
// never usable; an expiry in the past is not either. No expiry does not expire.
func (t TokenRecord) Usable(nowMS int64) bool {
	if t.RevokedAt != nil {
		return false
	}
	if t.ExpiresAt == nil {
		return true
	}
	return *t.ExpiresAt > nowMS
}

// Grant is what this token authorizes, for a caller that has already decided
// the token is live.
func (t TokenRecord) Grant() Grant {
	return Grant{
		ID:                  t.ID,
		Label:               t.Label,
		DID:                 t.DID,
		MintedBy:            t.MintedBy,
		MintedByUser:        t.MintedByUser,
		MintedByDisplayName: t.MintedByDisplayName,
		Level:               Level(t.Level),
		Namespaces:          t.Namespaces,
		ReturnAddress:       t.ReturnAddress,
		ClientDID:           t.ClientDID,
		RequestID:           t.RequestID,
	}
}

// Info is this token without its hash — the safe-to-return shape.
func (t TokenRecord) Info() TokenInfo {
	return TokenInfo{
		ID:                  t.ID,
		Label:               t.Label,
		DID:                 t.DID,
		MintedBy:            t.MintedBy,
		MintedByUser:        t.MintedByUser,
		MintedByDisplayName: t.MintedByDisplayName,
		Level:               Level(t.Level),
		Namespaces:          t.Namespaces,
		ReturnAddress:       t.ReturnAddress,
		ClientDID:           t.ClientDID,
		RequestID:           t.RequestID,
		CreatedAt:           whenMS(&t.CreatedAt),
		ExpiresAt:           whenMSOrNil(t.ExpiresAt),
		LastUsedAt:          whenMSOrNil(t.LastUsedAt),
		RevokedAt:           whenMSOrNil(t.RevokedAt),
	}
}

// whenMS is epoch milliseconds as the instant an API answers with.
func whenMS(ms *int64) string {
	if ms == nil {
		return ""
	}
	return time.UnixMilli(*ms).UTC().Format(time.RFC3339Nano)
}

func whenMSOrNil(ms *int64) *string {
	if ms == nil {
		return nil
	}
	when := whenMS(ms)
	return &when
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
	ClientDID           string   `json:"client_did,omitempty"`
	RequestID           string   `json:"request_id,omitempty"`
	CreatedAt           string   `json:"created_at"`
	ExpiresAt           *string  `json:"expires_at,omitempty"`
	LastUsedAt          *string  `json:"last_used_at,omitempty"`
	RevokedAt           *string  `json:"revoked_at,omitempty"`
}

// GitHubSecret is the credential a GITHUB token holds. Never listed.
type GitHubSecret struct {
	Token            string `json:"token"`
	Refresh          string `json:"refresh,omitempty"`
	ExpiresAt        *int64 `json:"expires_at,omitempty"`
	RefreshExpiresAt *int64 `json:"refresh_expires_at,omitempty"`
	// Client is the GitHub App the token was issued through; a refresh is
	// spent with that one and no other.
	Client string `json:"client,omitempty"`
	Source string `json:"source"`
	Login  string `json:"login,omitempty"`
}

// MintToken draws the raw token and the DID it names: 32 random bytes,
// hex-encoded, `qntx_` prefixed (ADR-025:16). The bytes are an ed25519 seed,
// so the token has a public half worth naming and its holder can sign as it.
//
// The one place a token is drawn, whether the mint element asks the store or
// fosite asks the strategy.
func MintToken() (raw, did string, err error) {
	seed := make([]byte, TokenSeedBytes)
	if _, err := rand.Read(seed); err != nil {
		return "", "", errors.Wrap(err, "failed to read a seed for an access token")
	}
	pub, isEd25519 := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if !isEd25519 {
		return "", "", errors.Newf(
			"an ed25519 seed produced a %T public half, so the token has no DID to be named by",
			ed25519.NewKeyFromSeed(seed).Public())
	}
	return TokenPrefix + hex.EncodeToString(seed), EncodeDIDKey(pub), nil
}
