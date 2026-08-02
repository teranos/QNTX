package server

import (
	"fmt"
	"time"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/pulse/schedule"
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

	schedStore := schedule.NewStore(s.db)
	interval := int(grpc.UpdatePollInterval.Seconds())

	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs for plugin update idempotency check",
			"handler_name", grpc.UpdateHandlerName, "error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName == grpc.UpdateHandlerName && j.State == schedule.StateActive {
			if j.IntervalSeconds != interval {
				if err := schedStore.UpdateJobInterval(j.ID, interval); err != nil {
					s.logger.Errorw("Failed to update plugin update schedule interval",
						"job_id", j.ID, "error", err)
				} else {
					s.logger.Infow("Updated plugin update schedule interval",
						"job_id", j.ID, "interval_seconds", interval)
				}
			}
			return
		}
	}

	// No first run: resolvePlugin has just reconciled everything at startup.
	job := &schedule.Job{
		ID:              fmt.Sprintf("SPJ_plugin_update_%d", time.Now().Unix()),
		HandlerName:     grpc.UpdateHandlerName,
		IntervalSeconds: interval,
		State:           schedule.StateActive,
	}

	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create plugin update schedule",
			"interval_seconds", interval, "error", err)
		return
	}

	s.logger.Infow("Auto-created plugin update schedule",
		"job_id", job.ID, "interval_seconds", interval)
}
