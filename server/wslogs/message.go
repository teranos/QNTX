package wslogs

import (
	"time"

	"go.uber.org/zap/zapcore"
)

// Message represents a log message for WebSocket transport
type Message struct {
	Level     string                 `json:"level"`            // "DEBUG", "INFO", "WARN", "ERROR"
	Timestamp time.Time              `json:"timestamp"`        // When the log was created
	Logger    string                 `json:"logger"`           // Logger name (e.g., "server")
	Message   string                 `json:"message"`          // Log message
	Fields    map[string]interface{} `json:"fields,omitempty"` // Structured fields
}

// Batch represents a collection of log messages from a single query
type Batch struct {
	Messages  []Message `json:"messages"`           // All log messages
	QueryID   string    `json:"query_id,omitempty"` // Query identifier
	Timestamp time.Time `json:"timestamp"`          // When batch was created
}

// FromZapEntry converts a zap log entry to our Message format
func FromZapEntry(entry zapcore.Entry, fields []zapcore.Field) Message {
	// Preallocate map with known size for better performance
	fieldsMap := make(map[string]interface{}, len(fields))

	// Convert zap fields to simple map for JSON serialization
	for _, f := range fields {
		// Handle different zap field types
		switch f.Type {
		case zapcore.StringType:
			fieldsMap[f.Key] = f.String
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
			fieldsMap[f.Key] = f.Integer
		case zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
			fieldsMap[f.Key] = f.Integer
		case zapcore.Float64Type, zapcore.Float32Type:
			fieldsMap[f.Key] = float64(f.Integer) // zap stores floats as integers internally
		case zapcore.BoolType:
			fieldsMap[f.Key] = f.Integer == 1
		case zapcore.DurationType:
			fieldsMap[f.Key] = time.Duration(f.Integer).String()
		case zapcore.TimeType:
			fieldsMap[f.Key] = time.Unix(0, f.Integer).Format(time.RFC3339)
		default:
			// For complex types, use the interface value
			fieldsMap[f.Key] = f.Interface
		}
	}

	return Message{
		Level:     entry.Level.String(),
		Timestamp: entry.Time,
		Logger:    entry.LoggerName,
		Message:   entry.Message,
		Fields:    fieldsMap,
	}
}
