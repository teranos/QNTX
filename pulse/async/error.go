package async

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// ErrorCode represents the classification of an error
type ErrorCode string

const (
	ErrorCodeFileNotFound    ErrorCode = "file_not_found"
	ErrorCodeParseError      ErrorCode = "parse_error"
	ErrorCodeNetworkError    ErrorCode = "network_error"
	ErrorCodeDatabaseError   ErrorCode = "database_error"
	ErrorCodeValidationError ErrorCode = "validation_error"
	ErrorCodeAIError         ErrorCode = "ai_error"
	ErrorCodeTimeout         ErrorCode = "timeout"
	ErrorCodeUnknown         ErrorCode = "unknown"
)

// ErrorContext provides structured error information for job failures
type ErrorContext struct {
	Stage       string    // Where the error occurred
	Code        ErrorCode // Error classification
	Message     string    // Human-readable message
	Retryable   bool      // Can the job be retried?
	Recoverable bool      // Can continue processing other items?
}

// ClassifyError categorizes an error based on its message and stage
func ClassifyError(stage string, err error) ErrorContext {
	if err == nil {
		return ErrorContext{
			Stage:       stage,
			Code:        ErrorCodeUnknown,
			Message:     "unknown error",
			Retryable:   false,
			Recoverable: false,
		}
	}

	errMsg := err.Error()
	errLower := strings.ToLower(errMsg)

	ctx := ErrorContext{
		Stage:   stage,
		Message: errMsg,
	}

	// Classify based on error message patterns
	switch {
	case strings.Contains(errLower, "no such file") || strings.Contains(errLower, "file not found"):
		ctx.Code = ErrorCodeFileNotFound
		ctx.Retryable = false
		ctx.Recoverable = true

	case strings.Contains(errLower, "parse") || strings.Contains(errLower, "unmarshal") || strings.Contains(errLower, "invalid json"):
		ctx.Code = ErrorCodeParseError
		ctx.Retryable = false
		ctx.Recoverable = true

	case strings.Contains(errLower, "network") || strings.Contains(errLower, "connection") || strings.Contains(errLower, "timeout"):
		ctx.Code = ErrorCodeNetworkError
		ctx.Retryable = true
		ctx.Recoverable = true

	case strings.Contains(errLower, "database") || strings.Contains(errLower, "sql"):
		ctx.Code = ErrorCodeDatabaseError
		ctx.Retryable = true
		ctx.Recoverable = false

	case strings.Contains(errLower, "validation") || strings.Contains(errLower, "invalid"):
		ctx.Code = ErrorCodeValidationError
		ctx.Retryable = false
		ctx.Recoverable = true

	case strings.Contains(errLower, "ai") || strings.Contains(errLower, "model") || strings.Contains(errLower, "llm"):
		ctx.Code = ErrorCodeAIError
		ctx.Retryable = true
		ctx.Recoverable = true

	case strings.Contains(errLower, "deadline exceeded") || strings.Contains(errLower, "timed out"):
		ctx.Code = ErrorCodeTimeout
		ctx.Retryable = true
		ctx.Recoverable = true

	default:
		ctx.Code = ErrorCodeUnknown
		ctx.Retryable = true
		ctx.Recoverable = false
	}

	return ctx
}

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
			log.Infow("꩜ Retry scheduled",
				"retry_count", job.RetryCount,
				"max_retries", MaxRetries,
				"operation", operation,
			)
		}
		return fmt.Errorf("retriable: %w", err)
	}
	log.Warnw("꩜ Max retries exceeded",
		"max_retries", MaxRetries,
		"operation", operation,
	)
	return fmt.Errorf("%s after %d retries: %w", operation, MaxRetries, err)
}
