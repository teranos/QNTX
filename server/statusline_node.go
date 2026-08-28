package server

import (
	"runtime"
	"time"

	"github.com/teranos/QNTX/server/syscap"
)

// The node's own answers for the rotating slot (StatusLineNode).

// Everything here is in memory or already sampled on the stats ticker. The row
// is polled by a terminal, so a frame querying a store on every draw would make
// redrawing the status line the most expensive thing the node does.

// Goroutines and HeapBytes are what pprof would tell you, without going to
// pprof. Both are counters the runtime already keeps.

// A goroutine count that climbs and never falls is the shape of a leak, and it
// is only visible if somebody is looking at it.
func (s *QNTXServer) Goroutines() int { return runtime.NumGoroutine() }

// HeapBytes is what is allocated and reachable now. ReadMemStats stops the
// world briefly, so this is read when a frame is drawn rather than on a ticker.
func (s *QNTXServer) HeapBytes() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// Uptime is how long this process has been answering.
func (s *QNTXServer) Uptime() time.Duration {
	if s == nil || s.startedAt.IsZero() {
		return 0
	}
	return time.Since(s.startedAt)
}

// ParserVersion is the ats WASM module's own version, or empty where the parser
// is the Go one and has none to report.
func (s *QNTXServer) ParserVersion() string {
	return syscap.Get(s.dbPath).ParserVersion
}

// Pressure is the last sampled CPU and memory percentages. Sampling CPU blocks
// for a second, so this reads what the stats ticker already paid for.
func (s *QNTXServer) Pressure() (float64, float64, bool) {
	live, ok := s.cachedLive()
	if !ok {
		return 0, 0, false
	}
	cpu, cpuOK := live["cpu_pct"].(float64)
	mem, memOK := live["mem_pct"].(float64)
	return cpu, mem, cpuOK && memOK
}

// Attestations is the count from that same cache. False is uncounted rather
// than none — a backend that cannot count says so.
func (s *QNTXServer) Attestations() (int, bool) {
	if s == nil {
		return 0, false
	}
	cached := s.dbStatsCache.Load()
	if cached == nil {
		return 0, false
	}
	held, ok := cached.response["total_attestations"].(int)
	return held, ok
}

// Watchers is how many this node has loaded, read from the engine rather than
// counted in the store.
func (s *QNTXServer) Watchers() int {
	if s == nil || s.watcherEngine == nil {
		return 0
	}
	return len(s.watcherEngine.GetAllWatchers())
}

// Schedules asks the store, which is why frames are produced only when drawn —
// at rest that is once per sweep rather than once per redraw.
func (s *QNTXServer) Schedules() int {
	if s == nil || s.scheduleStore == nil {
		return 0
	}

	jobs, err := s.scheduleStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Warnw("status line could not count schedules", "error", err)
		return 0
	}
	return len(jobs)
}

// Handlers is what the pulse registry holds: the built-in job handlers plus one
// proxy per handler a plugin declared.
func (s *QNTXServer) Handlers() int {
	if s == nil || s.daemon == nil {
		return 0
	}

	registry := s.daemon.Registry()
	if registry == nil {
		return 0
	}
	return len(registry.Names())
}

// Refusals is what the auth handler has turned away since this process started.
// A node running without auth refuses nobody and reports so.
func (s *QNTXServer) Refusals() (int64, int64) {
	if s == nil || s.authHandler == nil {
		return 0, 0
	}
	return s.authHandler.Refusals()
}

// cachedLive is the live section of the stats cache, absent until the first
// refresh has run.
func (s *QNTXServer) cachedLive() (map[string]interface{}, bool) {
	if s == nil {
		return nil, false
	}

	cached := s.dbStatsCache.Load()
	if cached == nil {
		return nil, false
	}
	live, ok := cached.response["live"].(map[string]interface{})
	return live, ok
}
