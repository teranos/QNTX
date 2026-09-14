package server

import (
	"context"
	"time"
)

// When a request arrived, carried on its context from the access log to the
// handler, so a handler can say how long the gate took before it was reached.
// A write that took four seconds and logged nothing inside them is a number
// nobody can find; the phases on the log line are how it is found.

type startedKey struct{}

func withStarted(ctx context.Context, at time.Time) context.Context {
	return context.WithValue(ctx, startedKey{}, at)
}

// sinceStarted is how long ago the request arrived, or zero for a request
// that came in by no access log.
func sinceStarted(ctx context.Context) time.Duration {
	at, ok := ctx.Value(startedKey{}).(time.Time)
	if !ok {
		return 0
	}
	return time.Since(at)
}
