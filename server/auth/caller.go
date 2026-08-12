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

const (
	// LevelSuper crosses namespaces and creates or deletes them.
	LevelSuper Level = "SUPER"
	// LevelRoot goes beyond QNTX — wanted on dev, not on prod.
	LevelRoot Level = "ROOT"
	// LevelToken is what a bearer token gets. It cannot mint tokens.
	LevelToken Level = "TOKEN"
	// LevelUser is a logged-in user.
	LevelUser Level = "USER"
)

// Caller is who reached a handler: a level and the namespace they inhabit.
// Namespace answers where, Level answers how much, and keeping them apart is
// what lets visibility be per-namespace without privilege leaking into it.
type Caller struct {
	Level     Level
	Namespace string
}

type callerKey struct{}

// WithCaller returns a context carrying the caller.
func WithCaller(ctx context.Context, caller Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, caller)
}

// CallerFrom returns the caller a handler was reached by. False means the
// handler ran outside Middleware — a wiring mistake, not an anonymous
// request, since Middleware never calls through without authenticating.
func CallerFrom(ctx context.Context) (Caller, bool) {
	caller, ok := ctx.Value(callerKey{}).(Caller)
	return caller, ok
}
