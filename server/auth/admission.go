package auth

import "context"

// The two namespaces a deployment always has (ADR-026). Every other namespace
// is created by SUPER, and neither of these can be deleted.
const (
	NamespaceSystem  = "system"
	NamespaceDefault = "default"
)

// Level is how much an admission may do (ADR-027). It says how much, never
// where.
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

// Admission is what a request was granted at the door. Middleware resolves it
// once and every handler past that point reads it: Level is how much,
// Namespace is where, Identity and UserID are who, Grant is which predicates.
type Admission struct {
	Level     Level
	Namespace string
	// Identity is the auth.root_identities entry that admitted this request —
	// an account URL or a did:key. A token carries the identity that minted it.
	Identity string
	// UserID is who that identity reaches (ADR-031). A person holds several
	// routes and several keys, so the route says which door was used and this
	// says who walked through it.

	// Empty on a deployment that keeps no Users, and on a bearer token, which
	// names the route that minted it and has not been resolved past that.
	UserID string
	// DisplayName is what to call that person. The status line draws it, because
	// a route is a door rather than a name.
	DisplayName string
	// Grant is present only when a token made the request. It names the token's
	// own DID and the predicates it may touch, and nil means unrestricted —
	// which is what a passkey session is.
	Grant *Grant
}

// MayRead reports whether this admission may read attestations with a
// predicate. No grant is unrestricted; a grant is only ever a narrowing.
func (a Admission) MayRead(predicate string) bool {
	return a.Grant == nil || a.Grant.MayRead(predicate)
}

// MayWrite reports whether this admission may write attestations with a
// predicate.
func (a Admission) MayWrite(predicate string) bool {
	return a.Grant == nil || a.Grant.MayWrite(predicate)
}

type admissionKey struct{}

type admissionSinkKey struct{}

// WithAdmissionSink puts a slot in the context for what the request turns out
// to be granted. Middleware hands the admission down on a copy of the request,
// so a layer wrapped around it needs somewhere to have the answer written.
func WithAdmissionSink(ctx context.Context) (context.Context, *Admission) {
	sink := &Admission{}
	return context.WithValue(ctx, admissionSinkKey{}, sink), sink
}

// WithAdmission returns a context carrying the admission, and fills the sink
// when an outer layer left one.
func WithAdmission(ctx context.Context, admission Admission) context.Context {
	if sink, ok := ctx.Value(admissionSinkKey{}).(*Admission); ok {
		*sink = admission
	}
	return context.WithValue(ctx, admissionKey{}, admission)
}

// AdmissionFrom returns what the request was granted. False means the handler
// ran outside Middleware — a wiring mistake, not an anonymous request, since
// Middleware never calls through without authenticating.
func AdmissionFrom(ctx context.Context) (Admission, bool) {
	admission, ok := ctx.Value(admissionKey{}).(Admission)
	return admission, ok
}
