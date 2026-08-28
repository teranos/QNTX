package server

import "sync/atomic"

// What this node answered, counted as it answers.

// A 4xx is the node saying the caller was wrong; a 5xx is it saying it was.
// Both are already logged, and a log on a box nothing ships from is a place
// nobody looks.
type answers struct {
	refused atomic.Int64 // 4xx
	broke   atomic.Int64 // 5xx
}

// note records one answer by its status. Below 400 is the node working, and a
// row has no room to say so.
func (a *answers) note(status int) {
	switch {
	case status >= 500:
		a.broke.Add(1)
	case status >= 400:
		a.refused.Add(1)
	}
}

// Answered is how many 4xx and 5xx this process has written since it started.
// In memory: what this node has seen, not what the deployment has.
func (s *QNTXServer) Answered() (refused, broke int64) {
	if s == nil {
		return 0, 0
	}
	return s.answers.refused.Load(), s.answers.broke.Load()
}
