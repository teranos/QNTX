package grpc

import (
	"context"
	"crypto/subtle"
	"fmt"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

// QueueServer implements the QueueService gRPC server
type QueueServer struct {
	protocol.UnimplementedQueueServiceServer
	queue     *async.Queue
	authToken string
	logger    *zap.SugaredLogger
}

// NewQueueServer creates a new queue gRPC server
func NewQueueServer(queue *async.Queue, authToken string, logger *zap.SugaredLogger) *QueueServer {
	return &QueueServer{
		queue:     queue,
		authToken: authToken,
		logger:    logger,
	}
}

// validateAuth checks the authentication token
func (s *QueueServer) validateAuth(token string) error {
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
		return fmt.Errorf("invalid authentication token")
	}
	return nil
}

// Enqueue adds a new job to the queue
func (s *QueueServer) Enqueue(ctx context.Context, req *protocol.EnqueueRequest) (*protocol.EnqueueResponse, error) {
	if err := s.validateAuth(req.AuthToken); err != nil {
		return &protocol.EnqueueResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Convert protobuf job to async.Job
	job, err := protoToJob(req.Job)
	if err != nil {
		return &protocol.EnqueueResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to convert job: %v", err),
		}, nil
	}

	// Enqueue the job
	if err := s.queue.Enqueue(job); err != nil {
		return &protocol.EnqueueResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to enqueue job: %v", err),
		}, nil
	}

	s.logger.Infow("Job enqueued via gRPC", "id", job.ID, "handler", job.HandlerName)

	return &protocol.EnqueueResponse{
		Success: true,
		JobId:   job.ID,
	}, nil
}

// GetJob retrieves a job by ID
func (s *QueueServer) GetJob(ctx context.Context, req *protocol.GetJobRequest) (*protocol.GetJobResponse, error) {
	if err := s.validateAuth(req.AuthToken); err != nil {
		return &protocol.GetJobResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Get the job
	job, err := s.queue.GetJob(req.JobId)
	if err != nil {
		return &protocol.GetJobResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to get job: %v", err),
		}, nil
	}

	if job == nil {
		return &protocol.GetJobResponse{
			Success: false,
			Error:   "job not found",
		}, nil
	}

	// Convert to protobuf
	protoJob, err := jobToProto(job)
	if err != nil {
		return &protocol.GetJobResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to convert job: %v", err),
		}, nil
	}

	return &protocol.GetJobResponse{
		Success: true,
		Job:     protoJob,
	}, nil
}

// UpdateJob updates a job's status and progress
func (s *QueueServer) UpdateJob(ctx context.Context, req *protocol.UpdateJobRequest) (*protocol.UpdateJobResponse, error) {
	if err := s.validateAuth(req.AuthToken); err != nil {
		return &protocol.UpdateJobResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Convert protobuf job to async.Job
	job, err := protoToJob(req.Job)
	if err != nil {
		return &protocol.UpdateJobResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to convert job: %v", err),
		}, nil
	}

	// Update the job
	if err := s.queue.UpdateJob(job); err != nil {
		return &protocol.UpdateJobResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to update job: %v", err),
		}, nil
	}

	s.logger.Infow("Job updated via gRPC", "id", job.ID, "status", job.Status)

	return &protocol.UpdateJobResponse{
		Success: true,
	}, nil
}

// ListJobs lists jobs with optional status filter
func (s *QueueServer) ListJobs(ctx context.Context, req *protocol.ListJobsRequest) (*protocol.ListJobsResponse, error) {
	if err := s.validateAuth(req.AuthToken); err != nil {
		return &protocol.ListJobsResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	limit := int(req.Limit)
	if limit == 0 {
		limit = 100 // Default limit
	}

	// List jobs
	var jobs []*async.Job
	var err error

	if req.Status != "" {
		status := async.JobStatus(req.Status)
		jobs, err = s.queue.ListJobs(&status, limit)
	} else {
		jobs, err = s.queue.ListJobs(nil, limit)
	}

	if err != nil {
		return &protocol.ListJobsResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to list jobs: %v", err),
		}, nil
	}

	// Convert to protobuf
	protoJobs := make([]*protocol.Job, len(jobs))
	for i, job := range jobs {
		protoJob, err := jobToProto(job)
		if err != nil {
			return &protocol.ListJobsResponse{
				Success: false,
				Error:   fmt.Sprintf("failed to convert job: %v", err),
			}, nil
		}
		protoJobs[i] = protoJob
	}

	return &protocol.ListJobsResponse{
		Success: true,
		Jobs:    protoJobs,
	}, nil
}

// Helper functions for conversion

func protoToJob(proto *protocol.Job) (*async.Job, error) {
	var pulseState *async.PulseState
	if proto.PulseState != nil {
		pulseState = &async.PulseState{
			CallsThisMinute: int(proto.PulseState.CallsThisMinute),
			CallsRemaining:  int(proto.PulseState.CallsRemaining),
			SpendToday:      proto.PulseState.SpendToday,
			SpendThisMonth:  proto.PulseState.SpendThisMonth,
			BudgetRemaining: proto.PulseState.BudgetRemaining,
			IsPaused:        proto.PulseState.IsPaused,
			PauseReason:     proto.PulseState.PauseReason,
		}
	}

	job := &async.Job{
		ID:           proto.Id,
		HandlerName:  proto.HandlerName,
		Payload:      proto.Payload,
		Source:       proto.Source,
		Status:       async.JobStatus(proto.Status),
		Progress: async.Progress{
			Current: int(proto.Progress.Current),
			Total:   int(proto.Progress.Total),
		},
		CostEstimate: proto.CostEstimate,
		CostActual:   proto.CostActual,
		PulseState:   pulseState,
		Error:        proto.Error,
		ParentJobID:  proto.ParentJobId,
		RetryCount:   int(proto.RetryCount),
		CreatedAt:    time.Unix(proto.CreatedAt, 0),
	}

	if proto.StartedAt != 0 {
		t := time.Unix(proto.StartedAt, 0)
		job.StartedAt = &t
	}

	if proto.CompletedAt != 0 {
		t := time.Unix(proto.CompletedAt, 0)
		job.CompletedAt = &t
	}

	return job, nil
}

func jobToProto(job *async.Job) (*protocol.Job, error) {
	var pulseState *protocol.PulseState
	if job.PulseState != nil {
		pulseState = &protocol.PulseState{
			CallsThisMinute: int32(job.PulseState.CallsThisMinute),
			CallsRemaining:  int32(job.PulseState.CallsRemaining),
			SpendToday:      job.PulseState.SpendToday,
			SpendThisMonth:  job.PulseState.SpendThisMonth,
			BudgetRemaining: job.PulseState.BudgetRemaining,
			IsPaused:        job.PulseState.IsPaused,
			PauseReason:     job.PulseState.PauseReason,
		}
	}

	protoJob := &protocol.Job{
		Id:          job.ID,
		HandlerName: job.HandlerName,
		Payload:     job.Payload,
		Source:      job.Source,
		Status:      string(job.Status),
		Progress: &protocol.Progress{
			Current: int32(job.Progress.Current),
			Total:   int32(job.Progress.Total),
		},
		CostEstimate: job.CostEstimate,
		CostActual:   job.CostActual,
		PulseState:   pulseState,
		Error:        job.Error,
		ParentJobId:  job.ParentJobID,
		RetryCount:   int32(job.RetryCount),
		CreatedAt:    job.CreatedAt.Unix(),
	}

	if job.StartedAt != nil {
		protoJob.StartedAt = job.StartedAt.Unix()
	}

	if job.CompletedAt != nil {
		protoJob.CompletedAt = job.CompletedAt.Unix()
	}

	return protoJob, nil
}
