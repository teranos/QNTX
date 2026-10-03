package server

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/errors"
)

// pendingUpsert captures the post-DB-write state needed for post-reload processing.
type pendingUpsert struct {
	// in is the namespace whose engine the watcher was written to.
	in            string
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

	// One reload per namespace in the batch, each against its own engine.
	byNamespace := map[string][]pendingUpsert{}
	for _, p := range batch {
		byNamespace[p.in] = append(byNamespace[p.in], p)
	}
	for in, pending := range byNamespace {
		c.flushIn(in, pending)
	}
}

// flushIn reloads one namespace's engine and runs the post-reload work of the
// watchers written to it.
func (c *watcherReloadCoalescer) flushIn(in string, batch []pendingUpsert) {
	s := c.server

	engine := s.engineIn(in)
	if engine == nil {
		s.logger.Errorw("A watcher was written to a namespace whose engine is gone",
			"namespace", in,
			"batch_size", len(batch),
		)
		for _, p := range batch {
			s.broadcastWatcherError(in, p.watcherID, "no watcher engine runs in "+in, "error")
		}
		return
	}

	if err := engine.ReloadWatchers(); err != nil {
		s.logger.Errorw("Failed to reload watchers (coalesced)",
			"namespace", in,
			"error", err,
			"batch_size", len(batch),
		)
		// Broadcast error to each pending watcher
		severity := extractErrorSeverity(err)
		for _, p := range batch {
			s.broadcastWatcherError(in, p.watcherID, err.Error(), severity, errors.GetAllDetails(err)...)
		}
		return
	}

	// Pre-flight: check if attestations are readable through Rust before spawning per-watcher queries.
	type counter interface{ CountAttestations() (int, error) }
	if c, ok := s.held.Served().(counter); ok && in == s.held.ServedUniverse().Name() {
		if _, err := c.CountAttestations(); err != nil {
			s.logger.Errorw("Failed to count attestations — skipping historical queries for batch",
				"namespace", in,
				"error", err,
				"batch_size", len(batch),
			)
			return
		}
	}

	// Post-reload processing for each watcher
	for _, p := range batch {
		c.postReload(engine, p)
	}
}

// postReload runs the per-watcher logic that was previously inline in handleWatcherUpsert
// after the ReloadWatchers() call: compound suppression check, parse error broadcast,
// and historical query dispatch.
func (c *watcherReloadCoalescer) postReload(engine *watcher.Engine, p pendingUpsert) {
	s := c.server

	reloadedWatcher, exists := engine.GetWatcher(p.watcherID)
	if !exists || reloadedWatcher == nil {
		// SE watchers absent from engine may be compound-suppressed (SE→SE meld)
		if strings.HasPrefix(p.watcherID, "se-element-") {
			elementID := strings.TrimPrefix(p.watcherID, "se-element-")
			compoundWatchers, err := engine.GetStore().FindCompoundWatchersForTarget(s.ctx, elementID)
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
						if err := engine.GetStore().Update(s.ctx, cw); err != nil {
							s.logger.Warnw("Failed to propagate query to compound watcher",
								"compound_watcher_id", cw.ID,
								"error", err)
						}
					}
				}
				// Trigger historical query on compound watcher(s)
				for _, cw := range compoundWatchers {
					cwID := cw.ID
					s.wg.Go("watcher.historicalCompound", func() {
						if err := engine.QueryHistoricalMatches(cwID); err != nil {
							s.logger.Errorw("Failed to query historical matches for compound watcher",
								"watcher_id", cwID,
								"error", err)
						}
					})
				}
				return
			}
		}

		// Watcher exists in DB but failed to load (likely parse error)
		parseErr := engine.GetParseError(p.watcherID)
		if parseErr != nil {
			s.logger.Warnw("Watcher parse failed",
				"watcher_id", p.watcherID,
				"query", p.watcherQuery,
				"error", parseErr,
			)
			severity := extractErrorSeverity(parseErr)
			s.broadcastWatcherError(p.in, p.watcherID, parseErr.Error(), severity, errors.GetAllDetails(parseErr)...)
		} else {
			errMsg := "Failed to parse AX query - watcher not activated"
			s.logger.Warnw("Watcher parse failed (no error details)",
				"watcher_id", p.watcherID,
				"query", p.watcherQuery,
			)
			s.broadcastWatcherError(p.in, p.watcherID, errMsg, "error",
				fmt.Sprintf("Query: %s", p.watcherQuery),
			)
		}
		return
	}

	// Query historical matches for the watcher (in goroutine to avoid blocking)
	s.wg.Go("watcher.historical", func() {
		if err := engine.QueryHistoricalMatches(p.watcherID); err != nil {
			s.logger.Errorw("Failed to query historical matches",
				"watcher_id", p.watcherID,
				"error", err,
			)
		}
	})
}
