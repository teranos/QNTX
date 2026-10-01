package auth

import (
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// "a stall under load should shed or throttle the heaviest caller, not end
// the process." — "Yes"
//
// A caller is an access token, and it is turned away with a 429 that says when
// to come back. A session is a person at the node and is never turned away.

// Shed counts what each bearer presents and turns away the ones the node has
// named, until it lifts them. The count is kept by the hash a bearer reduces
// to, before the token is looked up, so a caller turned away costs the node
// nothing past the hash.
type Shed struct {
	retryAfter time.Duration

	mu     sync.Mutex
	counts map[string]int    // hash → requests presented since Lift
	labels map[string]string // hash → the token's label, once it was admitted
	turned map[string]string // hash → the label of a caller being turned away
}

// NewShed is a Shed that tells whoever it turns away to come back after
// retryAfter.
func NewShed(retryAfter time.Duration) *Shed {
	return &Shed{
		retryAfter: retryAfter,
		counts:     map[string]int{},
		labels:     map[string]string{},
		turned:     map[string]string{},
	}
}

// presented counts one request from the bearer this hash is, and says whether
// it is turned away.
func (s *Shed) presented(hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[hash]++
	_, turned := s.turned[hash]
	return turned
}

// named remembers the label of a bearer that was admitted, so who is turned
// away can be said by name.
func (s *Shed) named(hash, label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.labels[hash] = label
}

// Heaviest turns away the bearer that presented the most since Lift and is
// not turned away already, and says who. False is nobody left to turn away.
func (s *Shed) Heaviest() (string, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hashes := make([]string, 0, len(s.counts))
	for hash := range s.counts {
		if _, turned := s.turned[hash]; !turned {
			hashes = append(hashes, hash)
		}
	}
	if len(hashes) == 0 {
		return "", 0, false
	}
	sort.Slice(hashes, func(i, j int) bool {
		if s.counts[hashes[i]] != s.counts[hashes[j]] {
			return s.counts[hashes[i]] > s.counts[hashes[j]]
		}
		return hashes[i] < hashes[j]
	})
	hash := hashes[0]
	label, known := s.labels[hash]
	if !known {
		label = "a bearer this node does not hold"
	}
	s.turned[hash] = label
	return label, s.counts[hash], true
}

// Turned is who is being turned away, by label.
func (s *Shed) Turned() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	turned := make([]string, 0, len(s.turned))
	for _, label := range s.turned {
		turned = append(turned, label)
	}
	sort.Strings(turned)
	return turned
}

// Lift lets every caller in again, says who was turned away, and starts the
// count again: the node lifts whenever it is not slow, so the heaviest is who
// sent the most since it last was not.
func (s *Shed) Lift() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	lifted := make([]string, 0, len(s.turned))
	for _, label := range s.turned {
		lifted = append(lifted, label)
	}
	sort.Strings(lifted)
	s.turned = map[string]string{}
	s.counts = map[string]int{}
	return lifted
}

// retryAfterSeconds is the Retry-After a caller turned away is told.
func (s *Shed) retryAfterSeconds() string {
	return strconv.Itoa(int(s.retryAfter.Round(time.Second) / time.Second))
}

// rejectTurnedAway answers a caller the node is turning away: 429, and when to
// come back.
func (h *Handler) rejectTurnedAway(w http.ResponseWriter) {
	w.Header().Set("Retry-After", h.shed.retryAfterSeconds())
	h.writeError(w, http.StatusTooManyRequests, "the node is slow and this token sends the most; come back after Retry-After")
}

// SetShed hands the gate what turns callers away. Nil turns nobody away.
func (h *Handler) SetShed(s *Shed) {
	h.shed = s
}
