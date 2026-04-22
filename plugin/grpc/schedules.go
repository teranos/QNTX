package grpc

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
)

// SetupPluginSchedules creates or updates Pulse schedules announced by a plugin.
// Called during plugin initialization to register plugin-announced schedules.
func SetupPluginSchedules(db *sql.DB, pluginName string, schedules []*protocol.ScheduleInfo, logger *zap.SugaredLogger) error {
	if len(schedules) == 0 {
		return nil
	}

	logger.Infow("Setting up plugin schedules",
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
	// Metadata contains {"plugin":"<name>"} — match on that.
	pluginMeta := fmt.Sprintf(`"plugin":"%s"`, pluginName)
	rows, err := db.Query(`
		SELECT id, handler_name FROM scheduled_pulse_jobs
		WHERE state != 'deleted' AND metadata LIKE '%' || ? || '%'
	`, pluginMeta)
	if err != nil {
		return errors.Wrapf(err, "failed to list schedules for pruning plugin %s", pluginName)
	}
	defer rows.Close()

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

	for _, id := range staleIDs {
		if _, err := db.Exec(`UPDATE scheduled_pulse_jobs SET state = 'deleted', updated_at = ? WHERE id = ?`, time.Now(), id); err != nil {
			logger.Warnw("Failed to prune stale plugin schedule", "plugin", pluginName, "schedule_id", id, "error", err)
		} else {
			logger.Infow("Pruned stale plugin schedule", "plugin", pluginName, "schedule_id", id)
		}
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

		// Check if schedule already exists
		var existingID string
		var existingInterval int
		namespacedHandler := PluginHandlerName(pluginName, s.HandlerName)
		err := db.QueryRow(`
			SELECT id, interval_seconds
			FROM scheduled_pulse_jobs
			WHERE handler_name = ?
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
	// Generate schedule ID using vanity-id
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

	now := time.Now()
	nextRunAt := now // For immediate first run

	// Insert schedule
	_, err = db.Exec(`
		INSERT INTO scheduled_pulse_jobs (
			id, ats_code, handler_name, payload, source_url,
			interval_seconds, next_run_at, state, metadata,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		jobID,
		s.AtsCode,
		PluginHandlerName(pluginName, s.HandlerName),
		nil, // No payload for plugin schedules
		"",  // No source URL
		s.IntervalSeconds,
		nextRunAt,
		state,
		string(metadataJSON),
		now,
		now,
	)
	if err != nil {
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
