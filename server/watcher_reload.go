package server

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/errors"
)

// pendingUpsert captures the post-DB-write state needed for post-reload processing.
type pendingUpsert struct {
	watcherID     string
	semanticQuery string
	watcherQuery  string
	threshold     float32
	clusterID     *int
}

// watcherReloadCoalescer batches rapid watcher_upsert messages into a single
// ReloadWatchers() call. Each upsert writes to DB immediately (cheap) but
// defers the reload + post-reload work behind a coalescing window.
//
// When the window fires, one reload happens, then each pending watcher gets
// its post-reload processing (compound suppression check, parse error broadcast,
// historical query dispatch).
type watcherReloadCoalescer struct {
	server  *QNTXServer
	mu      sync.Mutex
	pending []pendingUpsert
	timer   *time.Timer
	window  time.Duration
}

func newWatcherReloadCoalescer(s *QNTXServer, window time.Duration) *watcherReloadCoalescer {
	return &watcherReloadCoalescer{
		server: s,
		window: window,
	}
}

// schedule adds a pending upsert and resets the coalescing timer.
// Safe to call from multiple goroutines (WebSocket read pumps).
func (c *watcherReloadCoalescer) schedule(p pendingUpsert) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pending = append(c.pending, p)

	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(c.window, c.flush)
}

// flush is the timer callback. Grabs all pending upserts, calls ReloadWatchers()
// once, then runs post-reload processing for each watcher.
func (c *watcherReloadCoalescer) flush() {
	c.mu.Lock()
	batch := c.pending
	c.pending = nil
	c.timer = nil
	c.mu.Unlock()

	if len(batch) == 0 {
		return
	}

	s := c.server

	s.logger.Infow("Coalesced watcher reload",
		"pending_upserts", len(batch),
	)

	// Single reload for the entire batch
	if err := s.watcherEngine.ReloadWatchers(); err != nil {
		s.logger.Errorw("Failed to reload watchers (coalesced)",
			"error", err,
			"batch_size", len(batch),
		)
		// Broadcast error to each pending watcher
		severity := extractErrorSeverity(err)
		for _, p := range batch {
			s.broadcastWatcherError(p.watcherID, err.Error(), severity, errors.GetAllDetails(err)...)
		}
		return
	}

	// Post-reload processing for each watcher
	for _, p := range batch {
		c.postReload(p)
	}
}

// postReload runs the per-watcher logic that was previously inline in handleWatcherUpsert
// after the ReloadWatchers() call: compound suppression check, parse error broadcast,
// and historical query dispatch.
func (c *watcherReloadCoalescer) postReload(p pendingUpsert) {
	s := c.server

	reloadedWatcher, exists := s.watcherEngine.GetWatcher(p.watcherID)
	if !exists || reloadedWatcher == nil {
		// SE watchers absent from engine may be compound-suppressed (SE→SE meld)
		if strings.HasPrefix(p.watcherID, "se-glyph-") {
			glyphID := strings.TrimPrefix(p.watcherID, "se-glyph-")
			compoundWatchers, err := s.watcherEngine.GetStore().FindCompoundWatchersForTarget(s.ctx, glyphID)
			if err == nil && len(compoundWatchers) > 0 {
				s.logger.Infow("SE watcher suppressed by engine (compound target)",
					"watcher_id", p.watcherID,
					"compound_watchers", len(compoundWatchers))
				// Persist latest query to compound watchers (restart durability)
				for _, cw := range compoundWatchers {
					if cw.SemanticQuery != p.semanticQuery || cw.SemanticThreshold != p.threshold {
						cw.SemanticQuery = p.semanticQuery
						cw.SemanticThreshold = p.threshold
						cw.SemanticClusterID = p.clusterID
						if err := s.watcherEngine.GetStore().Update(s.ctx, cw); err != nil {
							s.logger.Warnw("Failed to propagate query to compound watcher",
								"compound_watcher_id", cw.ID,
								"error", err)
						}
					}
				}
				// Trigger historical query on compound watcher(s)
				for _, cw := range compoundWatchers {
					cwID := cw.ID
					s.wg.Add(1)
					go func() {
						defer s.wg.Done()
						if err := s.watcherEngine.QueryHistoricalMatches(cwID); err != nil {
							s.logger.Errorw("Failed to query historical matches for compound watcher",
								"watcher_id", cwID,
								"error", err)
						}
					}()
				}
				return
			}
		}

		// Watcher exists in DB but failed to load (likely parse error)
		parseErr := s.watcherEngine.GetParseError(p.watcherID)
		if parseErr != nil {
			s.logger.Warnw("Watcher parse failed",
				"watcher_id", p.watcherID,
				"query", p.watcherQuery,
				"error", parseErr,
			)
			severity := extractErrorSeverity(parseErr)
			s.broadcastWatcherError(p.watcherID, parseErr.Error(), severity, errors.GetAllDetails(parseErr)...)
		} else {
			errMsg := "Failed to parse AX query - watcher not activated"
			s.logger.Warnw("Watcher parse failed (no error details)",
				"watcher_id", p.watcherID,
				"query", p.watcherQuery,
			)
			s.broadcastWatcherError(p.watcherID, errMsg, "error",
				fmt.Sprintf("Query: %s", p.watcherQuery),
			)
		}
		return
	}

	// Query historical matches for the watcher (in goroutine to avoid blocking)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.watcherEngine.QueryHistoricalMatches(p.watcherID); err != nil {
			s.logger.Errorw("Failed to query historical matches",
				"watcher_id", p.watcherID,
				"error", err,
			)
		}
	}()
}
