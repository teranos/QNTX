package grpc

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/teranos/QNTX/internal/sqlclose"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	errors "github.com/teranos/sacred-error"
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
		if s.IntervalSeconds < 0 {
			return errors.Newf("plugin %s schedules %s every %d seconds: an interval is not fewer than none", pluginName, s.HandlerName, s.IntervalSeconds)
		}
		declaredHandlers[PluginHandlerName(pluginName, s.HandlerName)] = true
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
		pruned, err := db.Exec(`UPDATE scheduled_pulse_jobs SET state = 'deleted', updated_at = ? WHERE id = ?`, time.Now(), id)
		if err != nil {
			return errors.Wrapf(err, "failed to prune stale schedule %s for plugin %s — it will keep running", id, pluginName)
		}
		if err := oneRow(pruned); err != nil {
			return errors.Wrapf(err, "failed to prune stale schedule %s for plugin %s — it will keep running", id, pluginName)
		}
		logger.Infow("Pruned stale plugin schedule", "plugin", pluginName, "schedule_id", id)
	}

	for _, s := range schedules {

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
				updated, err := db.Exec(`
					UPDATE scheduled_pulse_jobs
					SET interval_seconds = ?, updated_at = ?
					WHERE id = ?
				`, s.IntervalSeconds, time.Now(), existingID)
				if err != nil {
					return errors.Wrapf(err, "failed to update schedule interval for handler %s", s.HandlerName)
				}
				if err := oneRow(updated); err != nil {
					return errors.Wrapf(err, "failed to update the interval of schedule %s for handler %s", existingID, s.HandlerName)
				}
			} else {
				logger.Debugw("Schedule already exists with same interval",
					"plugin", pluginName,
					"handler", s.HandlerName,
				)
			}
		}
	}

	return resumePluginSchedules(db, pluginName, logger)
}

// 「[Sonnet 5.5, Fable 5.2] A schedule is a plugin's by its handler: the plugin's name in front of it, as」
// 「[Sonnet 5.5, Fable 5.2] PluginHandlerName writes it, which is what sends a tick to that plugin. What」
// 「[Sonnet 5.5, Fable 5.2] a schedule's metadata says is whatever its maker wrote, and proves nothing.」
const ownedBy = `(substr(handler_name, 1, length(?)) = ? OR substr(handler_name, 1, length(?)) = ?)`

// 「[Sonnet 5.5, Fable 5.2] owned is the arguments ownedBy takes for one plugin: its name before a」
// 「[Sonnet 5.5, Fable 5.2] slash, and before a dot, as older handlers were written.」
func owned(pluginName string) []any {
	return []any{pluginName + "/", pluginName + "/", pluginName + ".", pluginName + "."}
}

// 「[Sonnet 5.5, Fable 5.2] movePluginSchedules moves every schedule of a plugin from one state to」
// 「[Sonnet 5.5, Fable 5.2] another in one statement, so no schedule is moved and another left: all of」
// 「[Sonnet 5.5, Fable 5.2] them, or none. It answers how many moved.」
func movePluginSchedules(db *sql.DB, pluginName, from, to string) (int64, error) {
	query := `UPDATE scheduled_pulse_jobs SET state = ?, updated_at = ? WHERE ` + ownedBy
	args := append([]any{to, time.Now().UTC().Format(time.RFC3339)}, owned(pluginName)...)
	if from == "" {
		query += ` AND state != ?`
		args = append(args, to)
	} else {
		query += ` AND state = ?`
		args = append(args, from)
	}
	moved, err := db.Exec(query, args...)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to move the schedules of plugin %s to %s", pluginName, to)
	}
	count, err := moved.RowsAffected()
	return count, errors.Wrapf(err, "the schedules of plugin %s moved to %s, and how many is not known", pluginName, to)
}

// 「[Sonnet 5.5, Fable 5.2] PausePluginSchedules takes what a plugin has running out of the ticker's」
// 「[Sonnet 5.5, Fable 5.2] sight, for a plugin that was switched off: active becomes inactive, the」
// 「[Sonnet 5.5, Fable 5.2] state of a schedule whose plugin is off. A schedule somebody paused is」
// 「[Sonnet 5.5, Fable 5.2] paused still, and switching the plugin on again does not wake it.」
func PausePluginSchedules(db *sql.DB, pluginName string, logger *zap.SugaredLogger) error {
	count, err := movePluginSchedules(db, pluginName, schedule.StateActive, schedule.StateInactive)
	if err != nil {
		return errors.Wrapf(err, "plugin %s is off and its schedules keep running", pluginName)
	}
	logger.Infow("The schedules of a plugin that was switched off are inactive", "plugin", pluginName, "schedules", count)
	return nil
}

// 「[Sonnet 5.5, Fable 5.2] resumePluginSchedules wakes what PausePluginSchedules took out, for a plugin」
// 「[Sonnet 5.5, Fable 5.2] that came back, and nothing else: a schedule paused by hand since is paused,」
// 「[Sonnet 5.5, Fable 5.2] not inactive, and stays so.」
func resumePluginSchedules(db *sql.DB, pluginName string, logger *zap.SugaredLogger) error {
	count, err := movePluginSchedules(db, pluginName, schedule.StateInactive, schedule.StateActive)
	if err != nil {
		return errors.Wrapf(err, "plugin %s is back and its schedules stay inactive", pluginName)
	}
	if count > 0 {
		logger.Infow("The schedules of a plugin that came back run again", "plugin", pluginName, "schedules", count)
	}
	return nil
}

// 「[Sonnet 5.5, Fable 5.2] DeletePluginSchedules deletes every schedule of a plugin that was taken off」
// 「[Sonnet 5.5, Fable 5.2] the node. A name added again starts with none.」
func DeletePluginSchedules(db *sql.DB, pluginName string, logger *zap.SugaredLogger) error {
	count, err := movePluginSchedules(db, pluginName, "", schedule.StateDeleted)
	if err != nil {
		return errors.Wrapf(err, "plugin %s is archived and its schedules keep their rows", pluginName)
	}
	logger.Infow("The schedules of an archived plugin are deleted", "plugin", pluginName, "schedules", count)
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
		MadeBy:          string(async.MadeByNoCaller),
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
