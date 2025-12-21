package ix

// Logger is a minimal logging interface for progress emitters.
// This allows library users to integrate with any logging framework
// (zap, logrus, stdlib log, etc.) by implementing this interface.
//
// The interface uses structured logging with key-value pairs,
// similar to zap's SugaredLogger pattern.
type Logger interface {
	// Info logs an informational message with optional structured fields.
	// Fields should be provided as key-value pairs: key1, value1, key2, value2, ...
	Info(msg string, fields ...interface{})

	// Warn logs a warning message with optional structured fields.
	// Fields should be provided as key-value pairs: key1, value1, key2, value2, ...
	Warn(msg string, fields ...interface{})

	// Error logs an error message with optional structured fields.
	// Fields should be provided as key-value pairs: key1, value1, key2, value2, ...
	Error(msg string, fields ...interface{})
}

// NopLogger is a no-operation logger that discards all log messages.
// Use this when you don't need logging or want to disable it.
type NopLogger struct{}

// Info implements Logger.Info (no-op).
func (NopLogger) Info(msg string, fields ...interface{}) {}

// Warn implements Logger.Warn (no-op).
func (NopLogger) Warn(msg string, fields ...interface{}) {}

// Error implements Logger.Error (no-op).
func (NopLogger) Error(msg string, fields ...interface{}) {}

// NewNopLogger returns a logger that discards all log messages.
func NewNopLogger() Logger {
	return NopLogger{}
}
