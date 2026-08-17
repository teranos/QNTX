package server

import (
	"context"
	"time"
)

// How often the node asks whether it can still read what it cannot run without.
const operationalCheckInterval = 5 * time.Second

// watchOperationalStore ends the process when the operational store stops being
// readable. Passkeys, jobs, schedules and the canvas live there, so a node that
// cannot read it is a port that answers rather than a node.
func (s *QNTXServer) WatchOperationalStore(stop func(reason error)) {
	ticker := time.NewTicker(operationalCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(s.ctx, operationalCheckInterval)
			err := s.db.PingContext(ctx)
			cancel()
			if err == nil {
				continue
			}

			s.logger.Errorw("the operational store is unreadable; QNTX cannot function",
				"error", err,
				"holds", "passkeys, jobs, schedules, canvas",
			)
			stop(err)
			return
		}
	}
}
