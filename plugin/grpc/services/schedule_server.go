package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// Caller is who made a sigil call a plugin is answering, and the namespace
// they act in.
type Caller struct {
	UserID    string
	Namespace string
}

// Callers is the caller of an open call, by the token the node handed the
// plugin for it. False is no call open under it.
type Callers func(token string) (Caller, bool)

// ScheduleServer implements the ScheduleService gRPC server
type ScheduleServer struct {
	protocol.UnimplementedScheduleServiceServer
	store     *schedule.Store
	authToken string
	logger    *zap.SugaredLogger
	// Set after the service is serving, while plugins may already be calling.
	callers atomic.Pointer[Callers]
}

// NewScheduleServer creates a new schedule gRPC server
func NewScheduleServer(store *schedule.Store, authToken string, logger *zap.SugaredLogger) *ScheduleServer {
	s := &ScheduleServer{
		store:     store,
		authToken: authToken,
		logger:    logger,
	}
	s.SetCallers(noCaller)
	return s
}

// noCaller is the node before it hands its open calls over: it has handed no
// plugin a call's token yet, so no call is open.
func noCaller(string) (Caller, bool) { return Caller{}, false }

// SetCallers hands the server the callers of the calls plugins are answering.
func (s *ScheduleServer) SetCallers(callers Callers) {
	s.callers.Store(&callers)
}

// callerOf is the caller of the open call a store token names.
func (s *ScheduleServer) callerOf(token string) (Caller, bool) {
	return (*s.callers.Load())(token)
}

// CreateSchedule creates a new recurring schedule in Pulse
func (s *ScheduleServer) CreateSchedule(ctx context.Context, req *protocol.CreateScheduleRequest) (*protocol.CreateScheduleResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.CreateScheduleResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// "a schedule remembers who created it and where"
	caller, err := s.madeBy(req)
	if err != nil {
		return &protocol.CreateScheduleResponse{Success: false, Error: err.Error()}, nil //nolint:nilerr // the failure travels in the response payload
	}

	// Idempotent: if an active schedule for this handler, made by the same
	// caller in the same namespace, already exists, return it
	// The store says none is there by sql.ErrNoRows; any other failure is not
	// a schedule being absent, and creating one over it would make a second.
	existing, err := s.store.GetActiveByHandlerName(req.HandlerName, caller.UserID, caller.Namespace)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return &protocol.CreateScheduleResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to look for an active schedule of handler %s: %v", req.HandlerName, err),
		}, nil
	}
	if err == nil {
		s.logger.Debugw("Schedule already exists for handler, returning existing",
			"schedule_id", existing.Id,
			"handler", req.HandlerName,
		)
		return &protocol.CreateScheduleResponse{
			Success:    true,
			ScheduleId: existing.Id,
		}, nil
	}

	// An interval of 0 is no ticking, and a schedule ticks: it names how often.
	interval, err := named.CountOf("interval_seconds, how often schedule "+req.HandlerName+" repeats,", int(req.IntervalSeconds))
	if err != nil {
		return &protocol.CreateScheduleResponse{Success: false, Error: err.Error()}, nil //nolint:nilerr // the failure travels in the response payload
	}

	// Generate schedule ID
	scheduleID, err := identity.GenerateASUID("AS", req.HandlerName, "schedule", "pulse")
	if err != nil {
		return &protocol.CreateScheduleResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to generate schedule ID: %v", err),
		}, nil
	}

	// Metadata is kept as a JSON object: {} for a schedule given none.
	kept := make(map[string]string, len(req.Metadata))
	for key, value := range req.Metadata {
		kept[key] = value
	}
	metaBytes, err := json.Marshal(kept)
	if err != nil {
		return &protocol.CreateScheduleResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to serialize metadata: %v", err),
		}, nil
	}
	metadata := string(metaBytes)

	now := time.Now()
	nextRun := now.Add(time.Duration(interval.Int()) * time.Second)

	job := &schedule.Job{
		Id:              scheduleID,
		HandlerName:     req.HandlerName,
		IntervalSeconds: req.IntervalSeconds,
		Payload:         req.Payload,
		State:           schedule.StateActive,
		NextRunAt:       nextRun.Format(time.RFC3339),
		Metadata:        metadata,
		UserId:          caller.UserID,
		Namespace:       caller.Namespace,
	}

	if err := s.store.CreateJob(job); err != nil {
		return &protocol.CreateScheduleResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create schedule: %v", err),
		}, nil
	}

	s.logger.Infow("Schedule created via gRPC",
		"schedule_id", scheduleID,
		"handler", req.HandlerName,
		"interval_seconds", req.IntervalSeconds,
		"user", caller.UserID,
		"namespace", caller.Namespace,
	)

	return &protocol.CreateScheduleResponse{
		Success:    true,
		ScheduleId: scheduleID,
	}, nil
}

// madeBy is the caller of the sigil call a schedule is made in, or no caller
// for one the plugin made itself.
func (s *ScheduleServer) madeBy(req *protocol.CreateScheduleRequest) (Caller, error) {
	switch made := req.GetMade().(type) {
	case *protocol.CreateScheduleRequest_StoreToken:
		caller, open := s.callerOf(made.StoreToken)
		if !open {
			return Caller{}, errors.Newf("store_token names no open sigil call, so schedule %s has no caller to remember", req.HandlerName)
		}
		return caller, nil
	case *protocol.CreateScheduleRequest_ByPlugin:
		if !made.ByPlugin {
			return Caller{}, errors.Newf("by_plugin is false: schedule %s is made in a sigil call, or by the plugin", req.HandlerName)
		}
		return Caller{}, nil
	}
	return Caller{}, errors.Newf("schedule %s names who made it: the store_token of a sigil call, or by_plugin", req.HandlerName)
}

// PauseSchedule pauses an active schedule
func (s *ScheduleServer) PauseSchedule(ctx context.Context, req *protocol.PauseScheduleRequest) (*protocol.PauseScheduleResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.PauseScheduleResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	if err := s.store.UpdateJobState(req.ScheduleId, schedule.StatePaused); err != nil {
		return &protocol.PauseScheduleResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to pause schedule %s: %v", req.ScheduleId, err),
		}, nil
	}

	s.logger.Infow("Schedule paused via gRPC", "schedule_id", req.ScheduleId)

	return &protocol.PauseScheduleResponse{
		Success: true,
	}, nil
}

// ResumeSchedule resumes a paused schedule
func (s *ScheduleServer) ResumeSchedule(ctx context.Context, req *protocol.ResumeScheduleRequest) (*protocol.ResumeScheduleResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.ResumeScheduleResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	if err := s.store.UpdateJobState(req.ScheduleId, schedule.StateActive); err != nil {
		return &protocol.ResumeScheduleResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to resume schedule %s: %v", req.ScheduleId, err),
		}, nil
	}

	s.logger.Infow("Schedule resumed via gRPC", "schedule_id", req.ScheduleId)

	return &protocol.ResumeScheduleResponse{
		Success: true,
	}, nil
}

// DeleteSchedule soft-deletes a schedule
func (s *ScheduleServer) DeleteSchedule(ctx context.Context, req *protocol.DeleteScheduleRequest) (*protocol.DeleteScheduleResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.DeleteScheduleResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	if err := s.store.UpdateJobState(req.ScheduleId, schedule.StateDeleted); err != nil {
		return &protocol.DeleteScheduleResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to delete schedule %s: %v", req.ScheduleId, err),
		}, nil
	}

	s.logger.Infow("Schedule deleted via gRPC", "schedule_id", req.ScheduleId)

	return &protocol.DeleteScheduleResponse{
		Success: true,
	}, nil
}

// GetSchedule retrieves a schedule by ID
func (s *ScheduleServer) GetSchedule(ctx context.Context, req *protocol.GetScheduleRequest) (*protocol.GetScheduleResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.GetScheduleResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	job, err := s.store.GetJob(req.ScheduleId)
	if err != nil {
		return &protocol.GetScheduleResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to get schedule %s: %v", req.ScheduleId, err),
		}, nil
	}

	return &protocol.GetScheduleResponse{
		Success: true,
		Job:     job,
	}, nil
}
