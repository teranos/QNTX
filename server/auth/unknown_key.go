package auth

import (
	"sync"
	"time"
)

// "CAN WE PLEASE STOP EXPECTING AN AGENT TO REQUEST IT OWN PERMISSIONS AND ALSO BE PERFECT?"
// "yeah, it jus tfeels like this should happen for sure"
//
// A key that signs as itself, for this node, and that this node holds nothing
// for, is a key to admit: the node is told, so it waits on the person.

// unknownKeyAgain is how long one key is told once.
const unknownKeyAgain = time.Hour

// unknownKeysAnHour is how many keys are told within an hour: a key costs
// nothing to make, and each told one stands before a person.
const unknownKeysAnHour = 16

// unknownKeys is which keys were told, and when.
type unknownKeys struct {
	mu   sync.Mutex
	told map[string]time.Time
}

// first is whether did is told now: not told within the hour, and fewer than
// unknownKeysAnHour keys were.
func (u *unknownKeys) first(did string, now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.told == nil {
		u.told = map[string]time.Time{}
	}
	for other, at := range u.told {
		if now.Sub(at) >= unknownKeyAgain {
			delete(u.told, other)
		}
	}
	if _, already := u.told[did]; already || len(u.told) >= unknownKeysAnHour {
		return false
	}
	u.told[did] = now
	return true
}

// SetUnknownKey has the node told of each key that signed for it, as itself,
// and that it holds nothing for: its did:key and what it calls itself.
func (h *Handler) SetUnknownKey(told func(did, label string)) {
	h.unknownKey = told
}

// Roots are the identities am.toml names ROOT here.
func (h *Handler) Roots() []string {
	return h.identities.roots()
}

// toldUnknown tells the node of did, once.
func (h *Handler) toldUnknown(did, label string, now time.Time) {
	if h.unknownKey == nil || !h.unknownKeys.first(did, now) {
		return
	}
	h.unknownKey(did, label)
}
