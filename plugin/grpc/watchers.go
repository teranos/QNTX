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
func SetupPluginWatchers(db *sql.DB, pluginName string, registrations []*protocol.WatcherRegistration, logger *zap.SugaredLogger) error {
	if len(registrations) == 0 {
		return nil
	}

	logger.Infow("Setting up plugin watchers",
		"plugin", pluginName,
		"count", len(registrations),
	)

	ws := storage.NewWatcherStore(db)
	ctx := context.Background()

	for _, reg := range registrations {
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

		logger.Infow("Registered plugin watcher",
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
