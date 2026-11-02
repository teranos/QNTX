//go:build !libacp

package acp

import (
	"context"

	errors "github.com/teranos/sacred-error"
)

// Linked is whether this QNTX was built with libACP, and so runs an ACP harness.
const Linked = false

// Run refuses: this QNTX was built without libACP.
func Run(_ context.Context, _ Agent, _ Turn, _ func(Update)) (Answer, error) {
	return Answer{}, errors.New("this QNTX was built without libACP (build tag libacp, make libacp), so it runs no ACP harness")
}
