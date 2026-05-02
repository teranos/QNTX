package logger

import (
	"context"

	"go.uber.org/zap"
)

// Standard field names for consistent structured logging across QNTX.
// Use these constants instead of raw strings to ensure consistency.
const (
	// Identity and context
	FieldJobID     = "job_id"
	FieldRequestID = "request_id"
	FieldTraceID   = "trace_id"
	FieldUserID    = "user_id"
	FieldActorID   = "actor_id"

	// Components
	FieldComponent = "component"
	FieldPlugin    = "plugin"
	FieldService   = "service"

	// Operations
	FieldOperation = "operation"
	FieldMethod    = "method"
	FieldPath      = "path"
	FieldQuery     = "query"

	// Timing
	FieldDurationMS = "duration_ms"
	FieldStartTime  = "start_time"
	FieldEndTime    = "end_time"

	// Errors
	FieldError     = "error"
	FieldErrorCode = "error_code"
	FieldErrorType = "error_type"

	// Counts and sizes
	FieldCount      = "count"
	FieldSize       = "size"
	FieldBatchSize  = "batch_size"
	FieldTotalCount = "total_count"

	// Status
	FieldStatus  = "status"
	FieldHealthy = "healthy"
	FieldState   = "state"

	// Files and paths
	FieldFile   = "file"
	FieldLine   = "line"
	FieldBinary = "binary"

	// Network
	FieldAddress = "address"
	FieldPort    = "port"
	FieldHost    = "host"

	// QNTX-specific
	FieldSymbol      = "symbol"      // QNTX segment symbol (꩜, ✿, ❀, etc.)
	FieldAttestation = "attestation" // Attestation ID
	FieldPredicate   = "predicate"   // Ax predicate
	FieldSubject     = "subject"     // Ax subject
)

// Context keys for propagating logging context
type contextKey string

const (
	jobIDKey     contextKey = "logger_job_id"
	requestIDKey contextKey = "logger_request_id"
	traceIDKey   contextKey = "logger_trace_id"
	componentKey contextKey = "logger_component"
)

// WithJobID adds a job ID to the context for logging
func WithJobID(ctx context.Context, jobID string) context.Context {
	return context.WithValue(ctx, jobIDKey, jobID)
}

// WithRequestID adds a request ID to the context for logging
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// WithTraceID adds a trace ID to the context for logging
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// WithComponent adds a component name to the context for logging
func WithComponent(ctx context.Context, component string) context.Context {
	return context.WithValue(ctx, componentKey, component)
}

// FieldsFromContext extracts logging fields from context.
// Returns key-value pairs suitable for use with Infow/Errorw/etc.
func FieldsFromContext(ctx context.Context) []interface{} {
	var fields []interface{}

	if jobID, ok := ctx.Value(jobIDKey).(string); ok && jobID != "" {
		fields = append(fields, FieldJobID, jobID)
	}
	if requestID, ok := ctx.Value(requestIDKey).(string); ok && requestID != "" {
		fields = append(fields, FieldRequestID, requestID)
	}
	if traceID, ok := ctx.Value(traceIDKey).(string); ok && traceID != "" {
		fields = append(fields, FieldTraceID, traceID)
	}
	if component, ok := ctx.Value(componentKey).(string); ok && component != "" {
		fields = append(fields, FieldComponent, component)
	}

	return fields
}

// LoggerFromContext returns a logger with fields extracted from context.
// Use this to get a logger that automatically includes job_id, request_id, etc.
func LoggerFromContext(ctx context.Context) *zap.SugaredLogger {
	fields := FieldsFromContext(ctx)
	if len(fields) == 0 {
		return Logger
	}
	return Logger.With(fields...)
}

// ComponentLogger returns a named logger for a specific component.
// This is the preferred way to get a logger for dependency injection.
//
// Example:
//
//	type WorkerPool struct {
//	    logger *zap.SugaredLogger
//	}
//
//	func NewWorkerPool() *WorkerPool {
//	    return &WorkerPool{
//	        logger: logger.ComponentLogger("pulse.worker"),
//	    }
//	}
func ComponentLogger(name string) *zap.SugaredLogger {
	return Logger.Named(name)
}

// ChildLogger creates a child logger with additional context.
// Use for sub-operations that need extra context fields.
//
// Example:
//
//	jobLogger := logger.ChildLogger(baseLogger, "job_id", job.ID)
func ChildLogger(parent *zap.SugaredLogger, keysAndValues ...interface{}) *zap.SugaredLogger {
	return parent.With(keysAndValues...)
}
