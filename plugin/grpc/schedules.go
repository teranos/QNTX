package grpc

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/teranos/QNTX/internal/sqlclose"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// SetupPluginSchedules creates or updates Pulse schedules announced by a plugin.
// Called during plugin initialization to register plugin-announced schedules.
func SetupPluginSchedules(db *sql.DB, pluginName string, schedules []*protocol.ScheduleInfo, logger *zap.SugaredLogger) (err error) {
	// No early return on an empty list. Declaring nothing is a declaration:
	// it says this plugin schedules nothing now, and the pruning below is
	// what withdraws everything it used to.
	logger.Debugw("Setting up plugin schedules",
		"plugin", pluginName,
		"count", len(schedules),
	)

	// Build set of namespaced handler names this plugin currently declares
	declaredHandlers := make(map[string]bool, len(schedules))
	for _, s := range schedules {
		if s.IntervalSeconds > 0 || s.EnabledByDefault {
			declaredHandlers[PluginHandlerName(pluginName, s.HandlerName)] = true
		}
	}

	// Prune stale schedules owned by this plugin that are no longer declared.
	// Match on metadata {"plugin":"<name>"} OR handler_name prefix "name." or "name/"
	// (the latter catches orphans from the old un-namespaced naming convention).
	pluginMeta := fmt.Sprintf(`"plugin":"%s"`, pluginName)
	pluginDotPrefix := pluginName + ".%"
	pluginSlashPrefix := pluginName + "/%"
	rows, err := db.Query(`
		SELECT id, handler_name FROM scheduled_pulse_jobs
		WHERE state != 'deleted' AND (
			metadata LIKE '%' || ? || '%'
			OR handler_name LIKE ?
			OR handler_name LIKE ?
		)
	`, pluginMeta, pluginDotPrefix, pluginSlashPrefix)
	if err != nil {
		return errors.Wrapf(err, "failed to list schedules for pruning plugin %s", pluginName)
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for SetupPluginSchedules") }()

	var staleIDs []string
	for rows.Next() {
		var id, handlerName string
		if err := rows.Scan(&id, &handlerName); err != nil {
			return errors.Wrapf(err, "failed to scan schedule row for plugin %s", pluginName)
		}
		if !declaredHandlers[handlerName] {
			staleIDs = append(staleIDs, id)
		}
	}
	if err := rows.Err(); err != nil {
		return errors.Wrapf(err, "failed to iterate schedules for plugin %s", pluginName)
	}

	// A prune that fails leaves a schedule running that nothing declares any
	// more, and a warning is the only trace. That is how a weekly job kept
	// firing after its decorator was deleted, so it fails the setup instead.
	for _, id := range staleIDs {
		if _, err := db.Exec(`UPDATE scheduled_pulse_jobs SET state = 'deleted', updated_at = ? WHERE id = ?`, time.Now(), id); err != nil {
			return errors.Wrapf(err, "failed to prune stale schedule %s for plugin %s — it will keep running", id, pluginName)
		}
		logger.Infow("Pruned stale plugin schedule", "plugin", pluginName, "schedule_id", id)
	}

	for _, s := range schedules {
		// Skip disabled schedules (interval <= 0 and not enabled by default)
		if s.IntervalSeconds <= 0 && !s.EnabledByDefault {
			logger.Debugw("Skipping disabled schedule",
				"plugin", pluginName,
				"handler", s.HandlerName,
			)
			continue
		}

		// Check if schedule already exists.
		//
		// Tombstones must not count. Pruning above marks a no-longer-declared
		// handler 'deleted' rather than removing the row, so a plugin that
		// drops a handler and later reinstates it would match its own
		// tombstone, take the update branch, and never run again. That was
		// unreachable while the store was :memory: — every boot started
		// clean — and became permanent the moment it went to disk.
		var existingID string
		var existingInterval int
		namespacedHandler := PluginHandlerName(pluginName, s.HandlerName)
		err := db.QueryRow(`
			SELECT id, interval_seconds
			FROM scheduled_pulse_jobs
			WHERE handler_name = ? AND state != 'deleted'
		`, namespacedHandler).Scan(&existingID, &existingInterval)

		if err == sql.ErrNoRows {
			// Create new schedule
			if err := createPluginSchedule(db, pluginName, s, logger); err != nil {
				return errors.Wrapf(err, "failed to create schedule for handler %s", s.HandlerName)
			}
		} else if err != nil {
			return errors.Wrapf(err, "failed to check existing schedule for handler %s", s.HandlerName)
		} else {
			// Schedule exists - update interval if changed
			if existingInterval != int(s.IntervalSeconds) {
				logger.Infow("Updating schedule interval",
					"plugin", pluginName,
					"handler", s.HandlerName,
					"old_interval", existingInterval,
					"new_interval", s.IntervalSeconds,
				)
				_, err := db.Exec(`
					UPDATE scheduled_pulse_jobs
					SET interval_seconds = ?, updated_at = ?
					WHERE id = ?
				`, s.IntervalSeconds, time.Now(), existingID)
				if err != nil {
					return errors.Wrapf(err, "failed to update schedule interval for handler %s", s.HandlerName)
				}
			} else {
				logger.Debugw("Schedule already exists with same interval",
					"plugin", pluginName,
					"handler", s.HandlerName,
				)
			}
		}
	}

	return nil
}

// createPluginSchedule creates a new schedule.Job for a plugin-announced schedule.
func createPluginSchedule(db *sql.DB, pluginName string, s *protocol.ScheduleInfo, logger *zap.SugaredLogger) error {
	// Generate schedule ID
	jobID, err := identity.GenerateASUID(
		"AS",
		fmt.Sprintf("plugin:%s:%s", pluginName, s.HandlerName),
		"scheduled",
		"pulse",
	)
	if err != nil {
		return errors.Wrap(err, "failed to generate schedule ID")
	}

	// Determine initial state
	state := schedule.StatePaused
	if s.EnabledByDefault {
		state = schedule.StateActive
	}

	// Build metadata with plugin info
	metadata := map[string]string{
		"plugin":      pluginName,
		"description": s.Description,
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return errors.Wrap(err, "failed to marshal schedule metadata")
	}

	// Through the store, not around it. This wrote eleven of the fourteen
	// columns directly, so a plugin schedule and a user schedule were two
	// different rows and only one of them went through CreateJob.
	now := time.Now()
	nextRunAt := now // For immediate first run
	if err := schedule.NewStore(db).CreateJob(&schedule.Job{
		Id:              jobID,
		HandlerName:     PluginHandlerName(pluginName, s.HandlerName),
		IntervalSeconds: s.IntervalSeconds,
		NextRunAt:       nextRunAt.Format(time.RFC3339),
		State:           state,
		Metadata:        string(metadataJSON),
		CreatedAt:       now.Format(time.RFC3339),
		UpdatedAt:       now.Format(time.RFC3339),
	}); err != nil {
		return errors.Wrapf(err, "failed to insert schedule %s", jobID)
	}

	logger.Infow("Created plugin schedule",
		"plugin", pluginName,
		"handler", s.HandlerName,
		"job_id", jobID,
		"interval", s.IntervalSeconds,
		"state", state,
	)

	return nil
}
