package wslogs

import (
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
)

func TestFromZapEntry(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name       string
		entry      zapcore.Entry
		fields     []zapcore.Field
		wantLevel  string
		wantLogger string
		wantMsg    string
		wantFields map[string]interface{}
	}{
		{
			name: "basic entry with no fields",
			entry: zapcore.Entry{
				Level:      zapcore.InfoLevel,
				Time:       testTime,
				LoggerName: "test.logger",
				Message:    "Test message",
			},
			fields:     []zapcore.Field{},
			wantLevel:  "info",
			wantLogger: "test.logger",
			wantMsg:    "Test message",
			wantFields: map[string]interface{}{},
		},
		{
			name: "entry with string field",
			entry: zapcore.Entry{
				Level:      zapcore.DebugLevel,
				Time:       testTime,
				LoggerName: "server",
				Message:    "Processing query",
			},
			fields: []zapcore.Field{
				{Key: "query_id", Type: zapcore.StringType, String: "q_12345"},
			},
			wantLevel:  "debug",
			wantLogger: "server",
			wantMsg:    "Processing query",
			wantFields: map[string]interface{}{
				"query_id": "q_12345",
			},
		},
		{
			name: "entry with integer fields",
			entry: zapcore.Entry{
				Level:      zapcore.WarnLevel,
				Time:       testTime,
				LoggerName: "graph.query",
				Message:    "High node count",
			},
			fields: []zapcore.Field{
				{Key: "nodes", Type: zapcore.Int64Type, Integer: 1000},
				{Key: "links", Type: zapcore.Int32Type, Integer: 5000},
			},
			wantLevel:  "warn",
			wantLogger: "graph.query",
			wantMsg:    "High node count",
			wantFields: map[string]interface{}{
				"nodes": int64(1000),
				"links": int64(5000),
			},
		},
		{
			name: "entry with bool field",
			entry: zapcore.Entry{
				Level:      zapcore.InfoLevel,
				Time:       testTime,
				LoggerName: "graph.websocket",
				Message:    "Connection status",
			},
			fields: []zapcore.Field{
				{Key: "connected", Type: zapcore.BoolType, Integer: 1},
				{Key: "authenticated", Type: zapcore.BoolType, Integer: 0},
			},
			wantLevel:  "info",
			wantLogger: "graph.websocket",
			wantMsg:    "Connection status",
			wantFields: map[string]interface{}{
				"connected":     true,
				"authenticated": false,
			},
		},
		{
			name: "entry with duration field",
			entry: zapcore.Entry{
				Level:      zapcore.InfoLevel,
				Time:       testTime,
				LoggerName: "graph.performance",
				Message:    "Query completed",
			},
			fields: []zapcore.Field{
				{Key: "duration", Type: zapcore.DurationType, Integer: int64(time.Second * 2)},
			},
			wantLevel:  "info",
			wantLogger: "graph.performance",
			wantMsg:    "Query completed",
			wantFields: map[string]interface{}{
				"duration": "2s",
			},
		},
		{
			name: "entry with mixed field types",
			entry: zapcore.Entry{
				Level:      zapcore.InfoLevel,
				Time:       testTime,
				LoggerName: "graph.complex",
				Message:    "Complex log",
			},
			fields: []zapcore.Field{
				{Key: "user", Type: zapcore.StringType, String: "alice"},
				{Key: "count", Type: zapcore.Int64Type, Integer: 42},
				{Key: "enabled", Type: zapcore.BoolType, Integer: 1},
				{Key: "elapsed", Type: zapcore.DurationType, Integer: int64(time.Millisecond * 500)},
			},
			wantLevel:  "info",
			wantLogger: "graph.complex",
			wantMsg:    "Complex log",
			wantFields: map[string]interface{}{
				"user":    "alice",
				"count":   int64(42),
				"enabled": true,
				"elapsed": "500ms",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := FromZapEntry(tt.entry, tt.fields)

			if msg.Level != tt.wantLevel {
				t.Errorf("FromZapEntry().Level = %q, want %q", msg.Level, tt.wantLevel)
			}
			if msg.Logger != tt.wantLogger {
				t.Errorf("FromZapEntry().Logger = %q, want %q", msg.Logger, tt.wantLogger)
			}
			if msg.Message != tt.wantMsg {
				t.Errorf("FromZapEntry().Message = %q, want %q", msg.Message, tt.wantMsg)
			}
			if !msg.Timestamp.Equal(testTime) {
				t.Errorf("FromZapEntry().Timestamp = %v, want %v", msg.Timestamp, testTime)
			}

			// Check fields
			if len(msg.Fields) != len(tt.wantFields) {
				t.Errorf("FromZapEntry().Fields has %d items, want %d", len(msg.Fields), len(tt.wantFields))
			}

			for key, wantVal := range tt.wantFields {
				gotVal, exists := msg.Fields[key]
				if !exists {
					t.Errorf("FromZapEntry().Fields missing key %q", key)
					continue
				}
				if gotVal != wantVal {
					t.Errorf("FromZapEntry().Fields[%q] = %v (type %T), want %v (type %T)",
						key, gotVal, gotVal, wantVal, wantVal)
				}
			}
		})
	}
}

func TestFromZapEntry_AllIntegerTypes(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name      string
		fieldType zapcore.FieldType
		integer   int64
		wantValue int64
	}{
		{name: "Int8Type", fieldType: zapcore.Int8Type, integer: 127, wantValue: 127},
		{name: "Int16Type", fieldType: zapcore.Int16Type, integer: 32767, wantValue: 32767},
		{name: "Int32Type", fieldType: zapcore.Int32Type, integer: 2147483647, wantValue: 2147483647},
		{name: "Int64Type", fieldType: zapcore.Int64Type, integer: 9223372036854775807, wantValue: 9223372036854775807},
		{name: "Uint8Type", fieldType: zapcore.Uint8Type, integer: 255, wantValue: 255},
		{name: "Uint16Type", fieldType: zapcore.Uint16Type, integer: 65535, wantValue: 65535},
		{name: "Uint32Type", fieldType: zapcore.Uint32Type, integer: 4294967295, wantValue: 4294967295},
		{name: "Uint64Type", fieldType: zapcore.Uint64Type, integer: 1234567890, wantValue: 1234567890},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := zapcore.Entry{
				Level:      zapcore.InfoLevel,
				Time:       testTime,
				LoggerName: "test",
				Message:    "test",
			}
			fields := []zapcore.Field{
				{Key: "value", Type: tt.fieldType, Integer: tt.integer},
			}

			msg := FromZapEntry(entry, fields)

			if val, ok := msg.Fields["value"].(int64); !ok || val != tt.wantValue {
				t.Errorf("FromZapEntry() with %s: Fields[\"value\"] = %v, want %v", tt.name, msg.Fields["value"], tt.wantValue)
			}
		})
	}
}

func TestFromZapEntry_UnknownFieldType(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	entry := zapcore.Entry{
		Level:      zapcore.InfoLevel,
		Time:       testTime,
		LoggerName: "test",
		Message:    "test",
	}

	// Use a field type that doesn't have explicit handling (e.g., ArrayType)
	complexValue := map[string]int{"a": 1, "b": 2}
	fields := []zapcore.Field{
		{Key: "complex", Type: zapcore.ReflectType, Interface: complexValue},
	}

	msg := FromZapEntry(entry, fields)

	// For unknown types, it should use Interface value
	if val, ok := msg.Fields["complex"].(map[string]int); !ok {
		t.Errorf("FromZapEntry() with unknown type: Fields[\"complex\"] type = %T, want map[string]int", msg.Fields["complex"])
	} else if len(val) != 2 {
		t.Errorf("FromZapEntry() with unknown type: Fields[\"complex\"] length = %d, want 2", len(val))
	}
}

func TestFromZapEntry_EmptyFields(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	entry := zapcore.Entry{
		Level:      zapcore.InfoLevel,
		Time:       testTime,
		LoggerName: "test",
		Message:    "test message",
	}

	msg := FromZapEntry(entry, []zapcore.Field{})

	if msg.Fields == nil {
		t.Error("FromZapEntry().Fields should not be nil")
	}
	if len(msg.Fields) != 0 {
		t.Errorf("FromZapEntry().Fields should be empty, got %d items", len(msg.Fields))
	}
}

func TestFromZapEntry_NilFields(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	entry := zapcore.Entry{
		Level:      zapcore.InfoLevel,
		Time:       testTime,
		LoggerName: "test",
		Message:    "test message",
	}

	msg := FromZapEntry(entry, nil)

	if msg.Fields == nil {
		t.Error("FromZapEntry().Fields should not be nil")
	}
	if len(msg.Fields) != 0 {
		t.Errorf("FromZapEntry().Fields should be empty, got %d items", len(msg.Fields))
	}
}

func TestBatchStructure(t *testing.T) {
	// Test that Batch struct can be created and has expected fields
	testTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	messages := []Message{
		{
			Level:     "INFO",
			Timestamp: testTime,
			Logger:    "test",
			Message:   "message 1",
			Fields:    map[string]interface{}{"id": 1},
		},
		{
			Level:     "DEBUG",
			Timestamp: testTime.Add(time.Second),
			Logger:    "test",
			Message:   "message 2",
			Fields:    map[string]interface{}{"id": 2},
		},
	}

	batch := Batch{
		Messages:  messages,
		QueryID:   "q_12345",
		Timestamp: testTime,
	}

	if len(batch.Messages) != 2 {
		t.Errorf("Batch.Messages length = %d, want 2", len(batch.Messages))
	}
	if batch.QueryID != "q_12345" {
		t.Errorf("Batch.QueryID = %q, want %q", batch.QueryID, "q_12345")
	}
	if !batch.Timestamp.Equal(testTime) {
		t.Errorf("Batch.Timestamp = %v, want %v", batch.Timestamp, testTime)
	}
}
