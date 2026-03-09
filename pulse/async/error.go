package async

import (
	"fmt"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/logger"
	"go.uber.org/zap"
)

// RetryableError marks an error for retry and returns a wrapped error.
// If max retries exceeded, returns a final error.
func RetryableError(queue *Queue, job *Job, operation string, err error, log *zap.SugaredLogger) error {
	// Use provided logger (with job context already configured by caller)

	if job.RetryCount < MaxRetries {
		job.RetryCount++
		job.Error = fmt.Sprintf("%s (retry %d/%d): %v", operation, job.RetryCount, MaxRetries, err)
		job.Status = JobStatusQueued // Re-enqueue for retry
		if updateErr := queue.UpdateJob(job); updateErr != nil {
			log.Warnw("Failed to update job for retry",
				"error", updateErr,
			)
		} else {
			logger.AddPulseSymbol(log).Infow("Retry scheduled",
				"retry_count", job.RetryCount,
				"max_retries", MaxRetries,
				"operation", operation,
			)
		}
		return errors.Wrap(err, "retriable")
	}
	logger.AddPulseSymbol(log).Warnw("Max retries exceeded",
		"max_retries", MaxRetries,
		"operation", operation,
	)
	return errors.Wrapf(err, "%s after %d retries", operation, MaxRetries)
}
