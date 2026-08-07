package server

import (
	"fmt"
	"time"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
)

// SetupPluginUpdateSchedule registers the handler that keeps installed plugins
// level with what their repos publish, and schedules it.
//
// Without it, reconciliation happens only at startup, so a release published
// while QNTX is running waits for a restart that may never come.
//
// The schedule fires at the shortest cadence the handler uses; the handler
// decides per plugin whether that plugin is due, so one job covers every
// plugin whatever pace each has settled into.
func (s *QNTXServer) SetupPluginUpdateSchedule(manager *grpc.PluginManager, registry *plugin.Registry) {
	if manager == nil || registry == nil || s.daemon == nil || s.db == nil {
		return
	}

	handler := &grpc.UpdateHandler{
		Manager:  manager,
		Registry: registry,
		Services: s.GetServices(),
		Logger:   s.logger.Named("plugin-update"),
	}

	s.daemon.Registry().Register(handler)
	s.logger.Infow("Registered plugin update handler")

	ensureUpdateSchedule(
		schedule.NewStore(s.db),
		int(grpc.UpdatePollInterval.Seconds()),
		time.Now(),
		s.logger,
	)
}

// ensureUpdateSchedule makes the store hold one active plugin.update job at the
// current interval. Separate from the wiring above so it can be exercised
// against a store alone.
func ensureUpdateSchedule(schedStore *schedule.Store, interval int, now time.Time, logger *zap.SugaredLogger) {
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		logger.Errorw("Failed to list scheduled jobs for plugin update idempotency check",
			"handler_name", grpc.UpdateHandlerName, "error", err)
		return
	}
	next := now.Add(time.Duration(interval) * time.Second)

	for _, j := range existing {
		if j.HandlerName == grpc.UpdateHandlerName && j.State == schedule.StateActive {
			if j.IntervalSeconds != int32(interval) {
				if err := schedStore.UpdateJobInterval(j.Id, interval); err != nil {
					logger.Errorw("Failed to update plugin update schedule interval",
						"job_id", j.Id, "error", err)
				} else {
					logger.Infow("Updated plugin update schedule interval",
						"job_id", j.Id, "interval_seconds", interval)
				}
			}
			// A job with no next run was never selectable and never will be:
			// next_run_at is written after an execution it cannot reach.
			if j.NextRunAt == "" {
				if err := schedStore.UpdateJobNextRun(j.Id, next); err != nil {
					logger.Errorw("Failed to give the plugin update schedule a next run",
						"job_id", j.Id, "error", err)
				} else {
					logger.Infow("Put the plugin update schedule back in the schedule",
						"job_id", j.Id, "next_run_at", next.Format(time.RFC3339))
				}
			}
			return
		}
	}

	// One interval out, rather than now: resolvePlugin has just reconciled at
	// startup. Unset is not "later", it is never.
	job := &schedule.Job{
		Id:              fmt.Sprintf("SPJ_plugin_update_%d", now.Unix()),
		HandlerName:     grpc.UpdateHandlerName,
		IntervalSeconds: int32(interval),
		NextRunAt:       next.Format(time.RFC3339),
		State:           schedule.StateActive,
	}

	if err := schedStore.CreateJob(job); err != nil {
		logger.Errorw("Failed to create plugin update schedule",
			"interval_seconds", interval, "error", err)
		return
	}

	logger.Infow("Auto-created plugin update schedule",
		"job_id", job.Id, "interval_seconds", interval)
}
