package namespaces

import (
	"context"
	"testing"
)

// A standing row fires in one namespace, and the built-in it reaches has to
// know which: the namespace rides the context, put there by the observer and
// read by whatever runs. One key, in the package that is about namespaces.
func TestTheNamespaceACallCarriesIsReadBack(t *testing.T) {
	ctx := WithNamespace(context.Background(), "Ground")
	if got := NamespaceOf(ctx); got != "Ground" {
		t.Fatalf("NamespaceOf = %q, want Ground", got)
	}
}

// A context nothing put a namespace on carries none, and the caller is told
// so by the empty string rather than by a namespace it did not fire in.
func TestAContextCarryingNoNamespaceSaysSo(t *testing.T) {
	if got := NamespaceOf(context.Background()); got != "" {
		t.Fatalf("NamespaceOf = %q, want empty", got)
	}
}
