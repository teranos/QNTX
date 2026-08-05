package grpc

// Keeping installed plugins level with what their repos publish.
//
// resolvePlugin reconciles at startup, which leaves a plugin as old as the last
// restart. A release published after boot arrives only when something unrelated
// restarts QNTX, so a fix can sit published and unused for as long as the
// process happens to live.
//
// This is the same reconcile on a schedule. It owns no policy of its own: what
// counts as stale, what replaces it, and what is left alone are all decided by
// resolvePlugin.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// UpdateHandlerName is the Pulse handler that reconciles plugins against their
// latest release.
const UpdateHandlerName = "plugin.update"

// How often a plugin is checked, by how long it has been since it last
// changed. A plugin someone is actively releasing is worth watching closely; a
// plugin untouched for a week is not worth three hundred API calls a day.
//
// UpdatePollInterval is the floor: the schedule fires at that cadence and each
// plugin decides whether it is due, so one job serves every rung.
const UpdatePollInterval = 3 * time.Minute

// updateLadder maps time since a plugin last changed to how often to check it.
// First rung whose age bound is not yet reached wins; past the last bound, the
// slowest cadence applies.
var updateLadder = []struct {
	within time.Duration
	every  time.Duration
}{
	{within: 6 * time.Hour, every: 3 * time.Minute},
	{within: 12 * time.Hour, every: 5 * time.Minute},
	{within: 32 * time.Hour, every: 10 * time.Minute},
	{within: 72 * time.Hour, every: time.Hour},
	{within: 7 * 24 * time.Hour, every: time.Hour},
}

// updateDormantInterval applies once a plugin has been unchanged for longer
// than the last rung.
const updateDormantInterval = 6 * time.Hour

// UpdateHandler reconciles every plugin that declared a repo.
type UpdateHandler struct {
	Manager  *PluginManager
	Registry *plugin.Registry
	Services plugin.ServiceRegistry
	Logger   *zap.SugaredLogger

	mu          sync.Mutex
	lastChecked map[string]time.Time
}

func (h *UpdateHandler) Name() string { return UpdateHandlerName }

// pollInterval is how often this plugin should be checked, given how long it
// has been since it last changed.
func pollInterval(sinceChange time.Duration) time.Duration {
	for _, rung := range updateLadder {
		if sinceChange < rung.within {
			return rung.every
		}
	}
	return updateDormantInterval
}

// sinceLastChange reports how long ago a plugin was last installed, read from
// the digest record's mtime.
//
// The record is written on every install, so its age is the age of what is
// running. Taking it from the filesystem rather than from memory means a
// restart does not reset a plugin to "just changed" and put it back on the
// fastest cadence.
func sinceLastChange(name string) (time.Duration, bool) {
	dir, err := PluginInstallPath(name)
	if err != nil {
		return 0, false
	}

	info, err := os.Stat(filepath.Join(dir, installedDigestFile))
	if err != nil {
		return 0, false
	}

	return time.Since(info.ModTime()), true
}

// due reports whether name should be checked now.
func (h *UpdateHandler) due(name string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.lastChecked == nil {
		h.lastChecked = make(map[string]time.Time)
	}

	last, seen := h.lastChecked[name]
	if !seen {
		// Never checked this process. Checking now also means a restart
		// reconciles immediately rather than waiting out an interval.
		h.lastChecked[name] = now
		return true
	}

	sinceChange, ok := sinceLastChange(name)
	if !ok {
		// Nothing installed by QNTX, so nothing for this to replace.
		return false
	}

	if now.Sub(last) < pollInterval(sinceChange) {
		return false
	}

	h.lastChecked[name] = now
	return true
}

func (h *UpdateHandler) Execute(ctx context.Context, job *async.Job) error {
	if h.Manager == nil || h.Registry == nil {
		return errors.New("plugin update handler is missing its manager or registry")
	}

	now := time.Now()

	h.Manager.mu.RLock()
	names := make([]string, 0, len(h.Manager.plugins))
	for name := range h.Manager.plugins {
		names = append(names, name)
	}
	h.Manager.mu.RUnlock()

	for _, name := range names {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// A plugin enabled by bare name has no repo and nothing to reconcile
		// against.
		repo := config.PluginRepo(name)
		if repo == "" {
			continue
		}

		if !h.due(name, now) {
			continue
		}

		// Mid-restart or paused: leave it be rather than race the thing
		// already replacing it.
		if state, ok := h.Registry.GetState(name); !ok ||
			state == plugin.StateRestarting || state == plugin.StatePaused {
			continue
		}

		h.Manager.mu.RLock()
		managed, ok := h.Manager.plugins[name]
		var binary string
		if ok {
			binary = managed.pluginCfg.Binary
		}
		h.Manager.mu.RUnlock()

		if !ok || binary == "" {
			continue
		}

		if !managedPluginIsStale(ctx, name, repo, binary, h.Logger) {
			continue
		}

		h.update(ctx, name, repo)
	}

	return nil
}

// update fetches the newer build and restarts the plugin onto it.
//
// A failure here is logged and not returned: one plugin failing to update is
// not a failed job, and the plugin that is already running keeps running.
func (h *UpdateHandler) update(ctx context.Context, name, repo string) {
	fetchCtx, cancel := context.WithTimeout(ctx, PluginFetchTimeout)
	defer cancel()

	binary, err := fetchPlugin(fetchCtx, name, repo, h.Logger)
	if err != nil {
		h.Logger.Warnw("Could not fetch the newer plugin; keeping the installed one",
			"plugin", name, "repo", repo, "error", err)
		return
	}

	h.Logger.Infow("Restarting plugin onto the newer build",
		"plugin", name, "binary", binary)

	if err := h.Manager.RestartPlugin(ctx, name, nil, h.Registry, h.Services); err != nil {
		h.Logger.Errorw("Installed the newer plugin but could not restart onto it",
			"plugin", name, "binary", binary, "error", err)
	}
}
