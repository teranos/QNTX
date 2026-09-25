package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/schedule"
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
	return &ScheduleServer{
		store:     store,
		authToken: authToken,
		logger:    logger,
	}
}

// SetCallers hands the server the callers of the calls plugins are answering.
func (s *ScheduleServer) SetCallers(callers Callers) {
	s.callers.Store(&callers)
}

// callerOf is the caller of the open call a store token names.
func (s *ScheduleServer) callerOf(token string) (Caller, bool) {
	if callers := s.callers.Load(); callers != nil {
		return (*callers)(token)
	}
	return Caller{}, false
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
	var caller Caller
	if req.StoreToken != "" {
		var open bool
		caller, open = s.callerOf(req.StoreToken)
		if !open {
			return &protocol.CreateScheduleResponse{
				Success: false,
				Error:   fmt.Sprintf("store_token names no open sigil call, so schedule %s has no caller to remember", req.HandlerName),
			}, nil
		}
	}

	// Idempotent: if an active schedule for this handler, made by the same
	// caller in the same namespace, already exists, return it
	existing, err := s.store.GetActiveByHandlerName(req.HandlerName, caller.UserID, caller.Namespace)
	if err == nil && existing != nil {
		s.logger.Debugw("Schedule already exists for handler, returning existing",
			"schedule_id", existing.Id,
			"handler", req.HandlerName,
		)
		return &protocol.CreateScheduleResponse{
			Success:    true,
			ScheduleId: existing.Id,
		}, nil
	}

	// Generate schedule ID
	scheduleID, err := identity.GenerateASUID("AS", req.HandlerName, "schedule", "pulse")
	if err != nil {
		return &protocol.CreateScheduleResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to generate schedule ID: %v", err),
		}, nil
	}

	// Serialize metadata to JSON string
	var metadata string
	if len(req.Metadata) > 0 {
		metaBytes, err := json.Marshal(req.Metadata)
		if err != nil {
			return &protocol.CreateScheduleResponse{
				Success: false,
				Error:   fmt.Sprintf("failed to serialize metadata: %v", err),
			}, nil
		}
		metadata = string(metaBytes)
	}

	now := time.Now()
	nextRun := now.Add(time.Duration(req.IntervalSeconds) * time.Second)

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
