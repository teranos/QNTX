package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	errors "github.com/teranos/sacred-error"
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
	SendAsNodeTo(ctx context.Context, u services.MailRecipient, m services.NodeMail) (messageID, attestationID string, err error)
}

// notMailing is the node's mail while it has no mail service, and why: each
// mail asked of it is refused with that.
type notMailing struct{ why error }

func (n notMailing) SendAsNode(context.Context, string, services.NodeMail) (string, string, error) {
	return "", "", n.why
}

func (n notMailing) SendAsNodeTo(context.Context, services.MailRecipient, services.NodeMail) (string, string, error) {
	return "", "", n.why
}

type reportHandler struct{ server *QNTXServer }

func (h *reportHandler) Name() string { return reportHandlerName }

func (h *reportHandler) Execute(ctx context.Context, _ *async.Job) error {
	_, err := h.server.sendReport(ctx)
	return err
}

// sendReport gathers the week ending now and mails it to the ROOT User.
func (s *QNTXServer) sendReport(ctx context.Context) (*protocol.MailReport, error) {
	root, found, err := s.authHandler.RootUser()
	if err != nil {
		return nil, errors.Wrap(err, "the report has nobody to go to")
	}
	if !found {
		return nil, errors.New("nobody has claimed this node, so there is no ROOT User to report to")
	}

	reader, why := s.sentryReader(ctx)
	r := s.gatherReport(ctx, time.Now().UTC(), reader, why)
	mail, err := renderReport(r)
	if err != nil {
		return nil, errors.Wrap(err, "the report could not be rendered")
	}
	// "make the report also get delivered to the Node Room"
	//
	// "i do feel like the NODE Agent should be receiving this as well, so i receive it, and the NODE Agent at the
	// same time"
	roomErr := s.sayReportInNodeRoom(ctx, mail)
	if roomErr != nil {
		s.logger.Errorw("The weekly report was not said in the Node Room", "error", roomErr)
	}
	messageID, attestationID, err := s.nodeMailer.SendAsNode(ctx, root.ID, mail)
	sent := &protocol.MailReport{To: root.PrimaryEmail(), MessageId: messageID, AttestationId: attestationID}
	if err != nil {
		err = errors.Wrapf(err, "the report to the ROOT User %s was not sent", root.ID)
		if roomErr != nil {
			err = errors.WithSecondaryError(err, roomErr)
		}
		return sent, err
	}
	s.logger.Infow("Weekly report sent", "user", root.ID, "message_id", messageID, "attestation", attestationID)
	// The mail went: a report asked for again would mail it twice. That the Node Room was not said to is the error
	// line logged above, which next week's report counts among its error lines.
	return sent, nil
}

// sayReportInNodeRoom says the report in the Node Room of the one plugin that holds rooms.
func (s *QNTXServer) sayReportInNodeRoom(ctx context.Context, mail services.NodeMail) error {
	provider, rooms, refused := s.roomsOf("")
	if refused != nil {
		return errors.Newf("the report has no Node Room to be said in: %s", refused.GetSays())
	}
	return sayReportIn(ctx, rooms, provider, mail)
}

// sayReportIn says the report in the Node Room provider holds, as the node: what the mail is called, with the
// report whole as text and its graphs beside it.
//
// It names nobody as who says it. A room's provider says what nobody is named for as the node itself, under the
// name the node has there (qntx-node in switchai-py); a name it does not hold it takes for a person at the node,
// registers and brings into the room.
func sayReportIn(ctx context.Context, rooms protocol.RoomsServiceClient, provider string, mail services.NodeMail) error {
	target, err := nodeRoomForComments(ctx, rooms, provider)
	if err != nil {
		return errors.Wrap(err, "the report has no Node Room to be said in")
	}
	text, err := services.BodyOf(mail.Bodies, services.MailText)
	if err != nil {
		return errors.Wrap(err, "the report has no text to say in the Node Room")
	}
	files := []*protocol.RoomFile{{Name: strings.ReplaceAll(mail.Subject, " ", "-") + ".txt", Type: "text/plain", Data: []byte(text)}}
	for _, img := range mail.Inline {
		files = append(files, &protocol.RoomFile{Name: img.FileName, Type: img.ContentType, Data: img.Data})
	}
	if _, err := rooms.PostToRoom(ctx, &protocol.PostToRoomRequest{
		Provider: provider, Room: target.Room, Body: mail.Subject, Files: files,
	}); err != nil {
		return errors.Wrap(err, "the report was not said in the Node Room")
	}
	return nil
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
	if s.held == nil {
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
		MadeBy:          string(async.MadeByNoCaller),
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
