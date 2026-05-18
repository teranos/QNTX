package grpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// SetupPluginWatchers creates or replaces watchers announced by a plugin.
// Called during plugin initialization to register plugin-declared watchers.
// Uses CreateOrReplace for idempotency — safe across plugin restarts.
// Prunes stale watchers that the plugin no longer declares.
// See ADR-018 for the full watcher lifecycle.
func SetupPluginWatchers(db *sql.DB, pluginName string, registrations []*protocol.WatcherRegistration, handlerNames []string, logger *zap.SugaredLogger) error {
	if len(registrations) > 0 {
		logger.Debugw("Setting up plugin watchers",
			"plugin", pluginName,
			"count", len(registrations),
		)
	}

	ws := storage.NewWatcherStore(db)
	ctx := context.Background()

	// Build set of watcher IDs the plugin currently declares
	declaredIDs := make(map[string]bool, len(registrations))
	for _, reg := range registrations {
		declaredIDs[fmt.Sprintf("%s-%s", pluginName, reg.Id)] = true
	}

	// Prune watchers with this plugin's prefix that are no longer declared
	prefix := pluginName + "-"
	existing, err := ws.List(ctx, false)
	if err != nil {
		return errors.Wrapf(err, "failed to list watchers for pruning plugin %s", pluginName)
	}
	var pruned int
	for _, w := range existing {
		if w.ActionType != storage.ActionTypePluginExecute {
			continue
		}
		if len(w.ID) <= len(prefix) || w.ID[:len(prefix)] != prefix {
			continue
		}
		if declaredIDs[w.ID] {
			continue
		}
		// Stale watcher — plugin no longer declares it
		if err := ws.Delete(ctx, w.ID); err != nil {
			logger.Warnw("Failed to prune stale plugin watcher",
				"plugin", pluginName,
				"watcher_id", w.ID,
				"error", err)
		} else {
			pruned++
		}
	}
	if pruned > 0 {
		logger.Infow("Pruned stale plugin watchers",
			"plugin", pluginName,
			"count", pruned)
	}

	// Build handler name set for validation
	declaredHandlers := make(map[string]bool, len(handlerNames))
	for _, h := range handlerNames {
		declaredHandlers[h] = true
	}

	// Register current watchers
	for _, reg := range registrations {
		if len(handlerNames) > 0 && !declaredHandlers[reg.HandlerName] {
			logger.Warnw("Watcher references undeclared handler_name — ExecuteJob will never be called",
				"plugin", pluginName,
				"watcher_id", reg.Id,
				"handler_name", reg.HandlerName,
				"declared_handlers", handlerNames,
			)
		}
		watcherID := fmt.Sprintf("%s-%s", pluginName, reg.Id)

		actionData, err := json.Marshal(watcher.PluginExecuteAction{
			PluginName:  pluginName,
			HandlerName: reg.HandlerName,
		})
		if err != nil {
			return errors.Wrapf(err, "failed to marshal action data for watcher %s", watcherID)
		}

		w := &storage.Watcher{
			ID:   watcherID,
			Name: fmt.Sprintf("%s/%s", pluginName, reg.Id),
			Filter: types.AxFilter{
				Subjects:   reg.Subjects,
				Predicates: reg.Predicates,
				Contexts:   reg.Contexts,
				Actors:     reg.Actors,
			},
			ActionType:        storage.ActionTypePluginExecute,
			ActionData:        string(actionData),
			MaxFiresPerSecond: int(reg.MaxFiresPerSecond),
			Enabled:           true,
		}

		if err := ws.CreateOrReplace(ctx, w); err != nil {
			return errors.Wrapf(err, "failed to create watcher %s for plugin %s", watcherID, pluginName)
		}

		logger.Debugw("Registered plugin watcher",
			"plugin", pluginName,
			"watcher_id", watcherID,
			"handler", reg.HandlerName,
			"predicates", reg.Predicates,
			"contexts", reg.Contexts,
			"max_fires_per_second", reg.MaxFiresPerSecond,
		)
	}

	return nil
}

// CountUnfilteredWatchers returns how many watcher registrations have no filter fields set.
func CountUnfilteredWatchers(watchers []*protocol.WatcherRegistration) int {
	count := 0
	for _, w := range watchers {
		if len(w.Subjects) == 0 && len(w.Predicates) == 0 && len(w.Contexts) == 0 && len(w.Actors) == 0 {
			count++
		}
	}
	return count
}

// WatcherNames extracts watcher IDs from registrations.
func WatcherNames(watchers []*protocol.WatcherRegistration) []string {
	names := make([]string, len(watchers))
	for i, w := range watchers {
		names[i] = w.GetId()
	}
	return names
}

// UnfilteredWatcherNames returns IDs of watchers with no filter fields set.
func UnfilteredWatcherNames(watchers []*protocol.WatcherRegistration) []string {
	var names []string
	for _, w := range watchers {
		if len(w.Subjects) == 0 && len(w.Predicates) == 0 && len(w.Contexts) == 0 && len(w.Actors) == 0 {
			names = append(names, w.GetId())
		}
	}
	return names
}

// ScheduleNames extracts handler names from schedule infos.
func ScheduleNames(schedules []*protocol.ScheduleInfo) []string {
	names := make([]string, len(schedules))
	for i, s := range schedules {
		names[i] = s.GetHandlerName()
	}
	return names
}
