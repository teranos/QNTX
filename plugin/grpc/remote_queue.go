package grpc

import (
	"context"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// RemoteQueue is a gRPC client wrapper for async.Queue.
// It implements the plugin.QueueService interface for gRPC plugins.
type RemoteQueue struct {
	client    protocol.QueueServiceClient
	conn      *grpc.ClientConn
	authToken string
	logger    *zap.SugaredLogger
	ctx       context.Context // Parent context for cancellation
}

// NewRemoteQueue creates a gRPC client connection to the Queue service.
// The provided context is used for all gRPC operations and enables cancellation.
func NewRemoteQueue(ctx context.Context, endpoint string, authToken string, logger *zap.SugaredLogger) (*RemoteQueue, error) {
	conn, err := grpc.Dial(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	client := protocol.NewQueueServiceClient(conn)

	return &RemoteQueue{
		client:    client,
		conn:      conn,
		authToken: authToken,
		logger:    logger,
		ctx:       ctx,
	}, nil
}

// Close closes the gRPC connection.
func (r *RemoteQueue) Close() error {
	return r.conn.Close()
}

// Enqueue adds a new job to the queue via gRPC.
func (r *RemoteQueue) Enqueue(job *async.Job) error {
	protoJob, err := jobToProto(job)
	if err != nil {
		return errors.Wrap(err, "failed to convert job")
	}

	req := &protocol.EnqueueRequest{
		AuthToken: r.authToken,
		Job:       protoJob,
	}

	resp, err := r.client.Enqueue(r.ctx, req)
	if err != nil {
		return errors.Wrap(err, "failed to enqueue job")
	}

	if !resp.Success {
		return errors.Newf("enqueue failed: %s", resp.Error)
	}

	// Update job with server-assigned ID if provided
	if resp.JobId != "" {
		job.ID = resp.JobId
	}

	return nil
}

// GetJob retrieves a job by ID via gRPC.
func (r *RemoteQueue) GetJob(id string) (*async.Job, error) {
	req := &protocol.GetJobRequest{
		AuthToken: r.authToken,
		JobId:     id,
	}

	resp, err := r.client.GetJob(r.ctx, req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get job")
	}

	if !resp.Success {
		return nil, errors.Newf("get job failed: %s", resp.Error)
	}

	if resp.Job == nil {
		return nil, errors.New("job not found")
	}

	job, err := protoToJob(resp.Job)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert job")
	}
	return job, nil
}

// UpdateJob updates a job's state via gRPC.
func (r *RemoteQueue) UpdateJob(job *async.Job) error {
	protoJob, err := jobToProto(job)
	if err != nil {
		return errors.Wrap(err, "failed to convert job")
	}

	req := &protocol.UpdateJobRequest{
		AuthToken: r.authToken,
		Job:       protoJob,
	}

	resp, err := r.client.UpdateJob(r.ctx, req)
	if err != nil {
		return errors.Wrap(err, "failed to update job")
	}

	if !resp.Success {
		return errors.Newf("update job failed: %s", resp.Error)
	}

	return nil
}

// ListJobs lists jobs with optional status filter via gRPC.
func (r *RemoteQueue) ListJobs(status *async.JobStatus, limit int) ([]*async.Job, error) {
	req := &protocol.ListJobsRequest{
		AuthToken: r.authToken,
		Limit:     int32(limit),
	}

	// Add status filter if provided
	if status != nil {
		req.Status = string(*status)
	}

	resp, err := r.client.ListJobs(r.ctx, req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list jobs")
	}

	if !resp.Success {
		return nil, errors.Newf("list jobs failed: %s", resp.Error)
	}

	// Convert proto jobs to Go types
	jobs := make([]*async.Job, len(resp.Jobs))
	for i, protoJob := range resp.Jobs {
		job, err := protoToJob(protoJob)
		if err != nil {
			r.logger.Warnw("Failed to convert job", "error", err, "index", i)
			continue
		}
		jobs[i] = job
	}

	return jobs, nil
}
