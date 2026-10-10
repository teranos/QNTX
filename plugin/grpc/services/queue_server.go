package services

import (
	"context"
	"fmt"
	"time"

	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/errors"
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

// Enqueue adds a new job to the queue
func (s *QueueServer) Enqueue(ctx context.Context, req *protocol.EnqueueRequest) (*protocol.EnqueueResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.EnqueueResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Convert protobuf job to async.Job
	job, err := ProtoToJob(req.Job)
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
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.GetJobResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
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

	// Convert to protobuf
	protoJob, err := JobToProto(job)
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
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.UpdateJobResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Convert protobuf job to async.Job
	job, err := ProtoToJob(req.Job)
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
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.ListJobsResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// A listing names how many jobs it wants: 0 is none.
	jobs, err := s.listed(req)
	if err != nil {
		return &protocol.ListJobsResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to list jobs: %v", err),
		}, nil
	}

	// Convert to protobuf
	protoJobs := make([]*protocol.Job, len(jobs))
	for i, job := range jobs {
		protoJob, err := JobToProto(job)
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

// listed is the jobs a listing names: in one status, or in every one.
func (s *QueueServer) listed(req *protocol.ListJobsRequest) ([]*async.Job, error) {
	limit := int(req.GetLimit())
	switch which := req.GetWhich().(type) {
	case *protocol.ListJobsRequest_Status:
		given, err := named.TextOf("status", which.Status)
		if err != nil {
			return nil, err
		}
		status := async.JobStatus(given.String())
		return s.queue.ListJobs(&status, limit)
	case *protocol.ListJobsRequest_EveryStatus:
		if !which.EveryStatus {
			return nil, errors.New("every_status is false: a listing names a status, or every status")
		}
		return s.queue.ListJobs(nil, limit)
	}
	return nil, errors.New("a listing names which jobs: a status, or every_status")
}

// Helper functions for conversion

func ProtoToJob(proto *protocol.Job) (*async.Job, error) {
	pulseState, err := pulseStateOf(proto)
	if err != nil {
		return nil, err
	}
	startedAt, err := momentOf(proto.GetId(), "started", proto.GetStarted())
	if err != nil {
		return nil, err
	}
	completedAt, err := momentOf(proto.GetId(), "completed", proto.GetCompleted())
	if err != nil {
		return nil, err
	}

	job := &async.Job{
		ID:          proto.Id,
		HandlerName: proto.HandlerName,
		Payload:     proto.Payload,
		Source:      proto.Source,
		Status:      async.JobStatus(proto.Status),
		Progress: async.Progress{
			Current: int(proto.Progress.Current),
			Total:   int(proto.Progress.Total),
		},
		PulseState:  pulseState,
		Error:       proto.Error,
		ParentJobID: proto.ParentJobId,
		RetryCount:  int(proto.RetryCount),
		CreatedAt:   time.Unix(proto.CreatedAt, 0),
		StartedAt:   startedAt,
		CompletedAt: completedAt,
	}

	return job, nil
}

// pulseStateOf is a job's pacing as async holds it: none for a job that says
// it carries none.
func pulseStateOf(proto *protocol.Job) (*async.PulseState, error) {
	switch pulse := proto.GetPulse().(type) {
	case *protocol.Job_PulseState:
		return &async.PulseState{
			CallsThisMinute: int(pulse.PulseState.GetCallsThisMinute()),
			CallsRemaining:  int(pulse.PulseState.GetCallsRemaining()),
			IsPaused:        pulse.PulseState.GetIsPaused(),
			PauseReason:     pulse.PulseState.GetPauseReason(),
		}, nil
	case *protocol.Job_NoPulseState:
		if !pulse.NoPulseState {
			return nil, errors.Newf("job %s: no_pulse_state is false: a job carries its pulse state, or none", proto.GetId())
		}
		return nil, nil
	}
	return nil, errors.Newf("job %s names no pulse state: pulse_state, or no_pulse_state", proto.GetId())
}

// momentOf is when a job started or completed, as async holds it: no time for
// a job that says it has not yet.
func momentOf(id, what string, moment any) (*time.Time, error) {
	switch at := moment.(type) {
	case *protocol.Job_StartedAt:
		t := time.Unix(at.StartedAt, 0)
		return &t, nil
	case *protocol.Job_CompletedAt:
		t := time.Unix(at.CompletedAt, 0)
		return &t, nil
	case *protocol.Job_NotStarted:
		if !at.NotStarted {
			return nil, errors.Newf("job %s: not_started is false: a job names when it started, or that it has not", id)
		}
		return nil, nil
	case *protocol.Job_NotCompleted:
		if !at.NotCompleted {
			return nil, errors.Newf("job %s: not_completed is false: a job names when it completed, or that it has not", id)
		}
		return nil, nil
	}
	return nil, errors.Newf("job %s does not say whether it %s", id, what)
}

func JobToProto(job *async.Job) (*protocol.Job, error) {

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
		Pulse:       &protocol.Job_NoPulseState{NoPulseState: true},
		Error:       job.Error,
		ParentJobId: job.ParentJobID,
		RetryCount:  int32(job.RetryCount),
		CreatedAt:   job.CreatedAt.Unix(),
		Started:     &protocol.Job_NotStarted{NotStarted: true},
		Completed:   &protocol.Job_NotCompleted{NotCompleted: true},
	}

	// async holds a job with no pulse state, and one not yet started or
	// completed, by a nil; on the wire each is named.
	if job.PulseState != nil {
		protoJob.Pulse = &protocol.Job_PulseState{PulseState: &protocol.PulseState{
			CallsThisMinute: int32(job.PulseState.CallsThisMinute),
			CallsRemaining:  int32(job.PulseState.CallsRemaining),
			IsPaused:        job.PulseState.IsPaused,
			PauseReason:     job.PulseState.PauseReason,
		}}
	}
	if job.StartedAt != nil {
		protoJob.Started = &protocol.Job_StartedAt{StartedAt: job.StartedAt.Unix()}
	}
	if job.CompletedAt != nil {
		protoJob.Completed = &protocol.Job_CompletedAt{CompletedAt: job.CompletedAt.Unix()}
	}

	return protoJob, nil
}
