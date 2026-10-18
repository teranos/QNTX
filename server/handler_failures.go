package server

import (
	"time"

	"github.com/teranos/QNTX/server/statusline"
)

// noteHandlerFailure records one failed handler execution for the row.
func (s *QNTXServer) noteHandlerFailure(f statusline.HandlerFailure) {
	if s == nil || s.handlerFailures == nil {
		return
	}
	if f.AtMs == 0 {
		f.AtMs = time.Now().UnixMilli()
	}
	// An unnamed handler is still a failing one. The job id is worse to read
	// than a name and better than an empty span on the row.
	if f.Handler == "" {
		f.Handler = f.ScheduledJobID
	}
	if f.Handler == "" {
		f.Handler = "handler"
	}
	s.handlerFailures.Record(f)
}
