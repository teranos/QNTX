package namespaces

import "context"

// The namespace a call is for, carried on its context. A standing row fires
// in one namespace and the built-in it reaches runs in no namespace of its
// own; the observer that ran it puts the namespace here, and whatever runs
// reads it back (server/standing_observer.go). One key, in the package that
// is about namespaces, so QNTX's own built-ins and a subsystem's read the same.
type carriedKey struct{}

// WithNamespace carries the namespace a call is for.
func WithNamespace(ctx context.Context, namespace string) context.Context {
	return context.WithValue(ctx, carriedKey{}, namespace)
}

// NamespaceOf is the namespace carried, or the empty string when none was,
// which the caller refuses out loud rather than reads as a namespace.
func NamespaceOf(ctx context.Context) string {
	ns, ok := ctx.Value(carriedKey{}).(string)
	if !ok {
		return ""
	}
	return ns
}
