package namespaces

import "context"

type carriedKey struct{}

// WithNamespace carries the namespace a standing row fired in, or a CI push
// arrived for, to the handler it reaches.
func WithNamespace(ctx context.Context, namespace string) context.Context {
	return context.WithValue(ctx, carriedKey{}, namespace)
}

// NamespaceOf is the namespace carried, or the empty string when none was,
// which the caller refuses out loud.
func NamespaceOf(ctx context.Context) string {
	ns, ok := ctx.Value(carriedKey{}).(string)
	if !ok {
		return ""
	}
	return ns
}
