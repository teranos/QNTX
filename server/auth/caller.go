package auth

import "context"

// The two namespaces a deployment always has (ADR-026). Every other namespace
// is created by SUPER, and neither of these can be deleted.
const (
	NamespaceSystem  = "system"
	NamespaceDefault = "default"
)

// Level is what a caller may do (ADR-027). It says how much, never where.
type Level string

// The ladder is scope. ATTESTOR acts inside a namespace, SUPER crosses
// namespaces, ROOT goes beyond QNTX.
const (
	// LevelSuper crosses namespaces and creates or disables them.
	LevelSuper Level = "SUPER"
	// LevelRoot goes beyond QNTX — wanted on dev, not on prod.
	LevelRoot Level = "ROOT"
	// LevelToken is what a bearer token gets. It cannot mint tokens.
	LevelToken Level = "TOKEN"
	// LevelAttestor acts inside a namespace. A User is the human; this is what
	// they may do (ADR-031).
	LevelAttestor Level = "ATTESTOR"
)

// Caller is who reached a handler: a level and the namespace they inhabit.
// Namespace answers where, Level answers how much, and keeping them apart is
// what lets visibility be per-namespace without privilege leaking into it.
type Caller struct {
	Level     Level
	Namespace string
	// Identity is the auth.root_identities entry that admitted this caller —
	// an account URL or a did:key. Level says how much; this says how they got
	// in. A token carries the identity that minted it.
	Identity string
	// UserID and Username are who that identity reaches (ADR-031). A person
	// holds several routes and several keys, so the route says which door was
	// used and this says who walked through it.

	// Empty on a deployment that keeps no Users, and on a bearer token, which
	// names the route that minted it and has not been resolved past that.
	UserID   string
	Username string
	// Grant is present only when a token made the request. It names the token's
	// own DID and the predicates it may touch, and nil means unrestricted —
	// which is what a passkey session is.
	Grant *Grant
}

// MayRead reports whether this caller may read attestations with a predicate.
// A caller with no grant is unrestricted; a grant is only ever a narrowing.
func (c Caller) MayRead(predicate string) bool {
	return c.Grant == nil || c.Grant.MayRead(predicate)
}

// MayWrite reports whether this caller may write attestations with a predicate.
func (c Caller) MayWrite(predicate string) bool {
	return c.Grant == nil || c.Grant.MayWrite(predicate)
}

type callerKey struct{}

type callerSinkKey struct{}

// WithCallerSink puts a slot in the context for whoever the request turns out
// to be. Middleware hands the caller down on a copy of the request, so a layer
// wrapped around it cannot see the answer without somewhere to have it written.
func WithCallerSink(ctx context.Context) (context.Context, *Caller) {
	sink := &Caller{}
	return context.WithValue(ctx, callerSinkKey{}, sink), sink
}

// WithCaller returns a context carrying the caller, and fills the sink when an
// outer layer left one.
func WithCaller(ctx context.Context, caller Caller) context.Context {
	if sink, ok := ctx.Value(callerSinkKey{}).(*Caller); ok {
		*sink = caller
	}
	return context.WithValue(ctx, callerKey{}, caller)
}

// CallerFrom returns the caller a handler was reached by. False means the
// handler ran outside Middleware — a wiring mistake, not an anonymous
// request, since Middleware never calls through without authenticating.
func CallerFrom(ctx context.Context) (Caller, bool) {
	caller, ok := ctx.Value(callerKey{}).(Caller)
	return caller, ok
}
