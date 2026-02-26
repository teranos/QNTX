package grpc

import (
	"context"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// RemoteSchedule is a gRPC client wrapper for schedule.Store.
// It implements the plugin.ScheduleService interface for gRPC plugins.
type RemoteSchedule struct {
	client    protocol.ScheduleServiceClient
	conn      *grpc.ClientConn
	authToken string
	logger    *zap.SugaredLogger
	ctx       context.Context // Parent context for cancellation
}

// NewRemoteSchedule creates a gRPC client connection to the Schedule service.
func NewRemoteSchedule(ctx context.Context, endpoint string, authToken string, logger *zap.SugaredLogger) (*RemoteSchedule, error) {
	conn, err := grpc.Dial(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	client := protocol.NewScheduleServiceClient(conn)

	return &RemoteSchedule{
		client:    client,
		conn:      conn,
		authToken: authToken,
		logger:    logger,
		ctx:       ctx,
	}, nil
}

// Close closes the gRPC connection.
func (r *RemoteSchedule) Close() error {
	return r.conn.Close()
}

// Create creates a new recurring schedule and returns its ID.
func (r *RemoteSchedule) Create(handlerName string, intervalSecs int, payload []byte, metadata map[string]string) (string, error) {
	req := &protocol.CreateScheduleRequest{
		AuthToken:       r.authToken,
		HandlerName:     handlerName,
		IntervalSeconds: int32(intervalSecs),
		Payload:         payload,
		Metadata:        metadata,
	}

	resp, err := r.client.CreateSchedule(r.ctx, req)
	if err != nil {
		return "", errors.Wrapf(err, "failed to create schedule for handler %s", handlerName)
	}

	if !resp.Success {
		return "", errors.Newf("create schedule failed: %s", resp.Error)
	}

	return resp.ScheduleId, nil
}

// Pause pauses an active schedule.
func (r *RemoteSchedule) Pause(scheduleID string) error {
	req := &protocol.PauseScheduleRequest{
		AuthToken:  r.authToken,
		ScheduleId: scheduleID,
	}

	resp, err := r.client.PauseSchedule(r.ctx, req)
	if err != nil {
		return errors.Wrapf(err, "failed to pause schedule %s", scheduleID)
	}

	if !resp.Success {
		return errors.Newf("pause schedule failed: %s", resp.Error)
	}

	return nil
}

// Resume resumes a paused schedule.
func (r *RemoteSchedule) Resume(scheduleID string) error {
	req := &protocol.ResumeScheduleRequest{
		AuthToken:  r.authToken,
		ScheduleId: scheduleID,
	}

	resp, err := r.client.ResumeSchedule(r.ctx, req)
	if err != nil {
		return errors.Wrapf(err, "failed to resume schedule %s", scheduleID)
	}

	if !resp.Success {
		return errors.Newf("resume schedule failed: %s", resp.Error)
	}

	return nil
}

// Delete soft-deletes a schedule.
func (r *RemoteSchedule) Delete(scheduleID string) error {
	req := &protocol.DeleteScheduleRequest{
		AuthToken:  r.authToken,
		ScheduleId: scheduleID,
	}

	resp, err := r.client.DeleteSchedule(r.ctx, req)
	if err != nil {
		return errors.Wrapf(err, "failed to delete schedule %s", scheduleID)
	}

	if !resp.Success {
		return errors.Newf("delete schedule failed: %s", resp.Error)
	}

	return nil
}

// Get retrieves a schedule by ID.
func (r *RemoteSchedule) Get(scheduleID string) (*schedule.Job, error) {
	req := &protocol.GetScheduleRequest{
		AuthToken:  r.authToken,
		ScheduleId: scheduleID,
	}

	resp, err := r.client.GetSchedule(r.ctx, req)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get schedule %s", scheduleID)
	}

	if !resp.Success {
		return nil, errors.Newf("get schedule failed: %s", resp.Error)
	}

	if resp.Job == nil {
		return nil, errors.Newf("schedule not found: %s", scheduleID)
	}

	return protoToScheduleJob(resp.Job), nil
}
