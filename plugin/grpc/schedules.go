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

// pausedBy is the metadata key that says why a schedule is paused when the
// node paused it. A schedule paused any other way, by declaration or by hand,
// does not carry it, and nothing but a person wakes that one.
const pausedBy = "paused_by"

// pausedByDisable is the value for a plugin that was switched off.
const pausedByDisable = "plugin-disabled"

type pluginSchedule struct {
	id, handler, state, metadata string
}

// schedulesOf is every live schedule a plugin owns, found as the pruning in
// SetupPluginSchedules finds them: by the plugin in its metadata, or by the
// plugin's name in front of its handler.
func schedulesOf(db *sql.DB, pluginName string) (_ []pluginSchedule, err error) {
	rows, err := db.Query(`
		SELECT id, handler_name, state, metadata FROM scheduled_pulse_jobs
		WHERE state != 'deleted' AND (
			metadata LIKE '%' || ? || '%'
			OR handler_name LIKE ?
			OR handler_name LIKE ?
		)
	`, fmt.Sprintf(`"plugin":"%s"`, pluginName), pluginName+".%", pluginName+"/%")
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list schedules of plugin %s", pluginName)
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for schedulesOf") }()
	var owned []pluginSchedule
	for rows.Next() {
		var sc pluginSchedule
		var metadata sql.NullString
		if err := rows.Scan(&sc.id, &sc.handler, &sc.state, &metadata); err != nil {
			return nil, errors.Wrapf(err, "failed to scan a schedule of plugin %s", pluginName)
		}
		sc.metadata = metadata.String
		owned = append(owned, sc)
	}
	return owned, errors.Wrapf(rows.Err(), "failed to iterate schedules of plugin %s", pluginName)
}

// moveSchedule writes one schedule's state and metadata, and fails if the row
// is not there to take them.
func moveSchedule(db *sql.DB, sc pluginSchedule, state, metadata string) error {
	moved, err := db.Exec(`UPDATE scheduled_pulse_jobs SET state = ?, metadata = ?, updated_at = ? WHERE id = ?`,
		state, metadata, time.Now().UTC().Format(time.RFC3339), sc.id)
	if err != nil {
		return errors.Wrapf(err, "failed to move schedule %s to %s", sc.id, state)
	}
	return oneRow(moved)
}

// PausePluginSchedules pauses what a plugin has running, for a plugin that was
// switched off. A schedule that was already paused is left as it was, so
// switching the plugin on again does not wake what somebody paused.
func PausePluginSchedules(db *sql.DB, pluginName string, logger *zap.SugaredLogger) error {
	owned, err := schedulesOf(db, pluginName)
	if err != nil {
		return err
	}
	for _, sc := range owned {
		if sc.state != schedule.StateActive {
			continue
		}
		metadata := map[string]any{}
		if sc.metadata != "" {
			if err := json.Unmarshal([]byte(sc.metadata), &metadata); err != nil {
				return errors.Wrapf(err, "schedule %s of plugin %s has metadata that is not JSON, so it was not paused", sc.id, pluginName)
			}
		}
		metadata[pausedBy] = pausedByDisable
		marked, err := json.Marshal(metadata)
		if err != nil {
			return errors.Wrapf(err, "failed to mark schedule %s", sc.id)
		}
		if err := moveSchedule(db, sc, schedule.StatePaused, string(marked)); err != nil {
			return errors.Wrapf(err, "plugin %s is off and schedule %s keeps running", pluginName, sc.id)
		}
		logger.Infow("Paused schedule of a plugin that was switched off", "plugin", pluginName, "schedule_id", sc.id, "handler", sc.handler)
	}
	return nil
}

// resumePluginSchedules wakes what PausePluginSchedules paused, for a plugin
// that came back, and nothing else.
func resumePluginSchedules(db *sql.DB, pluginName string, logger *zap.SugaredLogger) error {
	owned, err := schedulesOf(db, pluginName)
	if err != nil {
		return err
	}
	for _, sc := range owned {
		if sc.state != schedule.StatePaused {
			continue
		}
		metadata := map[string]any{}
		if err := json.Unmarshal([]byte(sc.metadata), &metadata); err != nil || metadata[pausedBy] != pausedByDisable {
			continue
		}
		delete(metadata, pausedBy)
		unmarked, err := json.Marshal(metadata)
		if err != nil {
			return errors.Wrapf(err, "failed to unmark schedule %s", sc.id)
		}
		if err := moveSchedule(db, sc, schedule.StateActive, string(unmarked)); err != nil {
			return errors.Wrapf(err, "plugin %s is back and schedule %s stays paused", pluginName, sc.id)
		}
		logger.Infow("Resumed schedule of a plugin that came back", "plugin", pluginName, "schedule_id", sc.id, "handler", sc.handler)
	}
	return nil
}

// DeletePluginSchedules deletes every schedule of a plugin that was taken off
// the node. A name added again starts with none.
func DeletePluginSchedules(db *sql.DB, pluginName string, logger *zap.SugaredLogger) error {
	owned, err := schedulesOf(db, pluginName)
	if err != nil {
		return err
	}
	for _, sc := range owned {
		if err := moveSchedule(db, sc, schedule.StateDeleted, sc.metadata); err != nil {
			return errors.Wrapf(err, "plugin %s is archived and schedule %s keeps its row", pluginName, sc.id)
		}
		logger.Infow("Deleted schedule of an archived plugin", "plugin", pluginName, "schedule_id", sc.id, "handler", sc.handler)
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
