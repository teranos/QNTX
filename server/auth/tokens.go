package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/teranos/QNTX/internal/access"
)

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

// The token records live in internal/access, which storage can import
// without importing the server.
type (
	Grant       = access.Grant
	NewToken    = access.NewToken
	IssuedToken = access.IssuedToken
	TokenRecord = access.TokenRecord
	TokenInfo   = access.TokenInfo
)
