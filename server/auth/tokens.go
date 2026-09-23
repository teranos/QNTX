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

// Namespace is what a word ends in to mean every predicate under it: `tag:`
// is every tag there will ever be.
//
// The colon is written down rather than inferred, so a line that says `type`
// says type and nothing that merely starts with it. Widening is a word somebody
// wrote, which is the same shape as `all` on a READ line.
const Namespace = ":"

// Every is the word that means every predicate: `WRITE is * of GROUND`. A
// token that records what happens rather than what a role is for names no
// list, and the line is a word like any other — written down, outranked,
// revoked. "i think * is more clea then all": `all` stays what it is, a word
// on a READ line about whose rows.
const Every = "*"

func permits(words []string, predicate string) bool {
	for _, word := range words {
		if word == predicate || word == Every {
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
// ever be, and `*` is every predicate — so a read narrowed by one is filtered
// after the store answers rather than handed to it as a filter.
func Names(words []string) bool {
	for _, word := range words {
		if word == Every || strings.HasSuffix(word, Namespace) {
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
	// ClientDID is the client the flow issued this through.
	ClientDID string
	// RequestID is the fosite request this was issued under. Rotation and
	// revocation arrive naming it rather than a hash, so the record carries
	// it or the two cannot be joined after a restart.
	RequestID string
}

// TokenStore is the full access-token contract used by middleware and the
// /auth/tokens endpoints. See ADR-025.
type TokenStore interface {
	// Lookup resolves a token hash to what it grants. False means no live token
	// has this hash — revoked, expired, unknown, or the store did not answer.
	Lookup(hash string) (Grant, bool)
	// LookupSpent answers for a token this hash names whether or not it still
	// works: the grant, whether it is live, and whether the store holds it at
	// all. Why Lookup is not enough: ADR-040.
	LookupSpent(hash string) (grant Grant, live bool, held bool)
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

// NamespaceMover is a TokenStore that can change where a token acts. The
// operational table is one; a store that is not answers a move as unavailable.
type NamespaceMover interface {
	SetNamespaces(id string, namespaces []string) error
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

// TokenRecordStore is the record behind the table: on parquet, one object per
// token under system/access_tokens/. It is what the table is rebuilt from
// after host loss, and nothing a request touches.
type TokenRecordStore interface {
	// Records returns every token the record holds, hashes included, which is
	// what a take-in needs and what List deliberately strips.
	Records() ([]TokenRecord, error)
	// PutRecord writes one token whole, replacing what was there.
	PutRecord(TokenRecord) error
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
