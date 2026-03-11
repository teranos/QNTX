package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
)

// ScheduleServer implements the ScheduleService gRPC server
type ScheduleServer struct {
	protocol.UnimplementedScheduleServiceServer
	store     *schedule.Store
	authToken string
	logger    *zap.SugaredLogger
}

// NewScheduleServer creates a new schedule gRPC server
func NewScheduleServer(store *schedule.Store, authToken string, logger *zap.SugaredLogger) *ScheduleServer {
	return &ScheduleServer{
		store:     store,
		authToken: authToken,
		logger:    logger,
	}
}

// CreateSchedule creates a new recurring schedule in Pulse
func (s *ScheduleServer) CreateSchedule(ctx context.Context, req *protocol.CreateScheduleRequest) (*protocol.CreateScheduleResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.CreateScheduleResponse{
			Success: false,
			Error:   err.Error(),
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
		ID:              scheduleID,
		HandlerName:     req.HandlerName,
		IntervalSeconds: int(req.IntervalSeconds),
		Payload:         req.Payload,
		State:           schedule.StateActive,
		NextRunAt:       &nextRun,
		Metadata:        metadata,
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
	)

	return &protocol.CreateScheduleResponse{
		Success:    true,
		ScheduleId: scheduleID,
	}, nil
}

// PauseSchedule pauses an active schedule
func (s *ScheduleServer) PauseSchedule(ctx context.Context, req *protocol.PauseScheduleRequest) (*protocol.PauseScheduleResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.PauseScheduleResponse{
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
		return &protocol.ResumeScheduleResponse{
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
		return &protocol.DeleteScheduleResponse{
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
		return &protocol.GetScheduleResponse{
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
		Job:     scheduleJobToProto(job),
	}, nil
}

// scheduleJobToProto converts a schedule.Job to proto ScheduledJob
func scheduleJobToProto(job *schedule.Job) *protocol.ScheduledJob {
	pj := &protocol.ScheduledJob{
		Id:              job.ID,
		AtsCode:         job.ATSCode,
		HandlerName:     job.HandlerName,
		Payload:         job.Payload,
		SourceUrl:       job.SourceURL,
		IntervalSeconds: int32(job.IntervalSeconds),
		LastExecutionId: job.LastExecutionID,
		State:           job.State,
		Metadata:        job.Metadata,
		CreatedAt:       job.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       job.UpdatedAt.Format(time.RFC3339),
	}

	if job.NextRunAt != nil {
		pj.NextRunAt = job.NextRunAt.Format(time.RFC3339)
	}
	if job.LastRunAt != nil {
		pj.LastRunAt = job.LastRunAt.Format(time.RFC3339)
	}

	return pj
}

// protoToScheduleJob converts a proto ScheduledJob to schedule.Job
func protoToScheduleJob(pj *protocol.ScheduledJob, logger *zap.SugaredLogger) *schedule.Job {
	job := &schedule.Job{
		ID:              pj.Id,
		ATSCode:         pj.AtsCode,
		HandlerName:     pj.HandlerName,
		Payload:         pj.Payload,
		SourceURL:       pj.SourceUrl,
		IntervalSeconds: int(pj.IntervalSeconds),
		LastExecutionID: pj.LastExecutionId,
		State:           pj.State,
		Metadata:        pj.Metadata,
	}

	if pj.NextRunAt != "" {
		if t, err := time.Parse(time.RFC3339, pj.NextRunAt); err == nil {
			job.NextRunAt = &t
		} else {
			logger.Warnw("Failed to parse NextRunAt", "schedule_id", pj.Id, "value", pj.NextRunAt, "error", err)
		}
	}
	if pj.LastRunAt != "" {
		if t, err := time.Parse(time.RFC3339, pj.LastRunAt); err == nil {
			job.LastRunAt = &t
		} else {
			logger.Warnw("Failed to parse LastRunAt", "schedule_id", pj.Id, "value", pj.LastRunAt, "error", err)
		}
	}
	if pj.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, pj.CreatedAt); err == nil {
			job.CreatedAt = t
		} else {
			logger.Warnw("Failed to parse CreatedAt", "schedule_id", pj.Id, "value", pj.CreatedAt, "error", err)
		}
	}
	if pj.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, pj.UpdatedAt); err == nil {
			job.UpdatedAt = t
		} else {
			logger.Warnw("Failed to parse UpdatedAt", "schedule_id", pj.Id, "value", pj.UpdatedAt, "error", err)
		}
	}

	return job
}
