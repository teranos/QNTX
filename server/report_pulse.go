package server

import (
	"context"
	"fmt"
	"time"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/QNTX/internal/sentryread"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/errors"
)

// The weekly report on a Pulse schedule (ADR-042), and the one act that sends
// it: gather the week, render it, mail it to the ROOT User.

const (
	reportHandlerName = "report.weekly"
	// Omitted from am.toml, the report is weekly. 0 is never.
	reportDefaultIntervalSeconds = 7 * 24 * 60 * 60
)

// nodeMailer is what sends the node's own mail: the mail service.
type nodeMailer interface {
	SendAsNode(ctx context.Context, userID string, m services.NodeMail) (messageID, attestationID string, err error)
}

type reportHandler struct{ server *QNTXServer }

func (h *reportHandler) Name() string { return reportHandlerName }

func (h *reportHandler) Execute(ctx context.Context, _ *async.Job) error {
	_, err := h.server.sendReport(ctx)
	return err
}

// reportSent is where the report went and what records it.
type reportSent struct {
	To            string `json:"to"`
	MessageID     string `json:"message_id"`
	AttestationID string `json:"attestation_id"`
}

// sendReport gathers the week ending now and mails it to the ROOT User.
func (s *QNTXServer) sendReport(ctx context.Context) (reportSent, error) {
	if s.nodeMailer == nil {
		return reportSent{}, errors.New("the mail service did not start, so the report is not sent")
	}
	root, found, err := s.authHandler.RootUser()
	if err != nil {
		return reportSent{}, errors.Wrap(err, "the report has nobody to go to")
	}
	if !found {
		return reportSent{}, errors.New("nobody has claimed this node, so there is no ROOT User to report to")
	}

	reader, why := s.sentryReader(ctx)
	r := s.gatherReport(ctx, time.Now().UTC(), reader, why)
	mail, err := renderReport(r)
	if err != nil {
		return reportSent{}, errors.Wrap(err, "the report could not be rendered")
	}
	messageID, attestationID, err := s.nodeMailer.SendAsNode(ctx, root.ID, mail)
	sent := reportSent{To: root.PrimaryEmail(), MessageID: messageID, AttestationID: attestationID}
	if err != nil {
		return sent, errors.Wrapf(err, "the report to the ROOT User %s was not sent", root.ID)
	}
	s.logger.Infow("Weekly report sent", "user", root.ID, "message_id", messageID, "attestation", attestationID)
	return sent, nil
}

// setupWeeklyReport reads Sentry's settings, registers the report and keeps its
// schedule: a new one runs at once, so a node that starts reporting sends its
// first report on its first tick.
func (s *QNTXServer) setupWeeklyReport(cfg *appcfg.Config) {
	s.sentryEnvironment = cfg.Sentry.Environment
	s.sentryConfig = cfg.Sentry
	if _, why := s.sentryReader(s.ctx); why != nil {
		s.logger.Infow("The weekly report leaves Sentry out until this is so", "reason", why)
	}
	if s.servicesManager != nil {
		if mail := s.servicesManager.MailServer(); mail != nil {
			s.nodeMailer = mail
		}
	}
	if s.daemon == nil || s.held == nil {
		return
	}
	s.daemon.Registry().Register(&reportHandler{server: s})

	interval := reportDefaultIntervalSeconds
	if cfg.Mail.Report.IntervalSeconds != nil {
		interval = *cfg.Mail.Report.IntervalSeconds
	}
	schedStore := s.held.ServedUniverse().Schedules()
	if interval == 0 {
		s.pauseExistingSchedule(schedStore, reportHandlerName)
		return
	}

	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("The weekly report is not scheduled: the schedules could not be listed", "error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName != reportHandlerName || j.State != schedule.StateActive {
			continue
		}
		if j.IntervalSeconds != int32(interval) {
			if err := schedStore.UpdateJobInterval(j.Id, interval); err != nil {
				s.logger.Errorw("The weekly report's interval was not changed", "job_id", j.Id, "interval_seconds", interval, "error", err)
			}
		}
		return
	}

	now := time.Now()
	job := &schedule.Job{
		Id:              fmt.Sprintf("SPJ_report_%d", now.Unix()),
		HandlerName:     reportHandlerName,
		IntervalSeconds: int32(interval),
		State:           schedule.StateActive,
		// The ticker compares next_run_at as text in local time.
		NextRunAt: now.Format(time.RFC3339),
	}
	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("The weekly report is not scheduled", "interval_seconds", interval, "error", err)
		return
	}
	s.logger.Infow("Scheduled the weekly report", "job_id", job.Id, "interval_seconds", interval)
}

// sentryReader is Sentry as am.toml names it, resolved now: a token created
// after the node started is read by the next report.
func (s *QNTXServer) sentryReader(ctx context.Context) (sentryread.Client, error) {
	return sentryReaderFrom(ctx, s.sentryConfig)
}

// sentryReaderFrom is a Sentry client from am.toml, or why there is none.
func sentryReaderFrom(ctx context.Context, cfg appcfg.SentryConfig) (sentryread.Client, error) {
	c := sentryread.Client{API: cfg.API, Organization: cfg.Organization, Project: cfg.Project}
	if cfg.ReadTokenRef != "" {
		token, err := secretref.Resolve(ctx, cfg.ReadTokenRef)
		if err != nil {
			// The reference is named, never the value it failed to fetch.
			return c, errors.Wrapf(err, "sentry.read_token %s did not resolve", cfg.ReadTokenRef)
		}
		c.Token = token
	}
	if err := c.Configured(); err != nil {
		return c, err
	}
	return c, nil
}
