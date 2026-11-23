package auth

import "context"

// "or should it be in the log?"
// "A2A ?"
//
// The node asks itself in process: what was collected and released is said
// to its agent over its own A2A, as ROOT (server/a2a_named.go, askedInProcess).
// No request crosses a network there, so nothing is presented: the gate takes
// the admission the node itself put on the asking, and still holds it to the
// reach table. Nothing outside this process sets a context value, so only the
// node's own asking is admitted so.

// inProcess marks a context as the node asking itself.
type inProcess struct{}

// InProcess is ctx as the node asking itself, admitted as a.
func InProcess(ctx context.Context, a Admission) context.Context {
	return context.WithValue(WithAdmission(ctx, a), inProcess{}, a)
}

// inProcessAdmission is the admission the node asked itself as, when it did.
func inProcessAdmission(ctx context.Context) (Admission, bool) {
	a, inside := ctx.Value(inProcess{}).(Admission)
	return a, inside
}
