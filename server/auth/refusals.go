package auth

import "sync/atomic"

// How many callers this node turned away, and how many of them were machines.

// A refusal is already attested and already logged. Neither answers at a glance
// whether something is still out there presenting a credential that will never
// work — a browser retries once and signs in, a cron retries forever.
type refusals struct {
	turnedAway atomic.Int64
	stale      atomic.Int64
}

// note records one refusal. bearer says the caller presented a token, which is
// the difference between somebody who has not signed in and something that
// cannot.
func (r *refusals) note(bearer bool) {
	r.turnedAway.Add(1)
	if bearer {
		r.stale.Add(1)
	}
}

// Refusals is how many callers were turned away since this process started, and
// how many of those held a token. Counted in memory: it says what this node has
// seen, not what the deployment has.
func (h *Handler) Refusals() (turnedAway, stale int64) {
	if h == nil {
		return 0, 0
	}
	return h.refused.turnedAway.Load(), h.refused.stale.Load()
}
