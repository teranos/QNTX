//go:build ignore
// +build ignore

// TODO: This test file depends on LogCapturingEmitter and TaskLogStore which are part of
// the Pulse system (async job infrastructure). These components write to the task_logs table
// and will be extracted to QNTX when Pulse code is migrated.
//
// Dependencies needed:
// - LogCapturingEmitter
// - LogStore interface
// - TaskLogStore implementation (currently in storage/log_store.go)
//
// Once Pulse extraction is complete, remove the build ignore tags and update the imports.
//
// The tests themselves are valuable and should be preserved - they verify the
// LogCapturingEmitter wrapper correctly logs to the database while passing through
// to the underlying ProgressEmitter.

package storage

import (
	"database/sql"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/teranos/QNTX/ats/ix"
	"github.com/teranos/QNTX/db"
	// TODO: Update import when LogCapturingEmitter is migrated to QNTX
)

// mockEmitter implements ProgressEmitter for testing
type mockEmitter struct {
	infoCalls  []string
	stageCalls []struct{ stage, message string }
	errorCalls []struct {
		stage string
		err   error
	}
	completeCalls []map[string]interface{}
}

func (m *mockEmitter) EmitInfo(message string) {
	m.infoCalls = append(m.infoCalls, message)
}

func (m *mockEmitter) EmitStage(stage string, message string) {
	m.stageCalls = append(m.stageCalls, struct{ stage, message string }{stage, message})
}

func (m *mockEmitter) EmitError(stage string, err error) {
	m.errorCalls = append(m.errorCalls, struct {
		stage string
		err   error
	}{stage, err})
}

func (m *mockEmitter) EmitComplete(summary map[string]interface{}) {
	m.completeCalls = append(m.completeCalls, summary)
}

func (m *mockEmitter) EmitProgress(count int, metadata map[string]interface{}) {
	// Not tracked in mock for now
}

func (m *mockEmitter) EmitAttestations(count int, entities []ix.AttestationEntity) {
	// Not tracked in mock for now
}

// setupTestDBForIx creates an in-memory database with real migrations.
// Uses db.Migrate() to ensure test schema matches production schema.
func setupTestDBForIx(t *testing.T) *sql.DB {
	testDB := qntxtest.CreateTestDB(t)

	// Apply real migrations (includes task_logs from migration 008)
	if err := db.Migrate(testDB, nil); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	return testDB
}

// TestLogCapturingEmitter_EmitInfo verifies info logs are captured
func TestLogCapturingEmitter_EmitInfo(t *testing.T) {
	db := setupTestDBForIx(t)
	defer testDB.Close()

	mock := &mockEmitter{}
	logStore := NewTaskLogStore(db)
	emitter := ats.NewLogCapturingEmitter(mock, logStore, "JB_test123")

	emitter.EmitInfo("Test info message")

	// Verify passthrough to underlying emitter
	if len(mock.infoCalls) != 1 {
		t.Fatalf("Expected 1 info call, got %d", len(mock.infoCalls))
	}
	if mock.infoCalls[0] != "Test info message" {
		t.Errorf("Expected 'Test info message', got '%s'", mock.infoCalls[0])
	}

	// Verify log was written to database
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM task_logs WHERE job_id = ?", "JB_test123").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query task_logs: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 log entry, got %d", count)
	}

	// Verify log content
	var level, message string
	err = db.QueryRow("SELECT level, message FROM task_logs WHERE job_id = ?", "JB_test123").Scan(&level, &message)
	if err != nil {
		t.Fatalf("Failed to query log entry: %v", err)
	}
	if level != "info" {
		t.Errorf("Expected level 'info', got '%s'", level)
	}
	if message != "Test info message" {
		t.Errorf("Expected message 'Test info message', got '%s'", message)
	}
}

// TestLogCapturingEmitter_EmitStage verifies stage transitions are captured
func TestLogCapturingEmitter_EmitStage(t *testing.T) {
	db := setupTestDBForIx(t)
	defer testDB.Close()

	mock := &mockEmitter{}
	logStore := NewTaskLogStore(db)
	emitter := ats.NewLogCapturingEmitter(mock, logStore, "JB_test123")

	emitter.EmitStage("fetch_data", "Fetching data source")

	// Verify passthrough
	if len(mock.stageCalls) != 1 {
		t.Fatalf("Expected 1 stage call, got %d", len(mock.stageCalls))
	}
	if mock.stageCalls[0].stage != "fetch_data" {
		t.Errorf("Expected stage 'fetch_data', got '%s'", mock.stageCalls[0].stage)
	}

	// NOTE: Cannot verify internal emitter.stage field (unexported)
	// The stage is verified via mock passthrough and database log entry instead

	// Verify log was written with stage context
	var stage sql.NullString
	err := db.QueryRow("SELECT stage FROM task_logs WHERE job_id = ?", "JB_test123").Scan(&stage)
	if err != nil {
		t.Fatalf("Failed to query log entry: %v", err)
	}
	if !stage.Valid || stage.String != "fetch_data" {
		t.Errorf("Expected stage 'fetch_data', got '%s' (valid: %v)", stage.String, stage.Valid)
	}
}

// TestLogCapturingEmitter_MultipleStages verifies logs track stage context changes
func TestLogCapturingEmitter_MultipleStages(t *testing.T) {
	db := setupTestDBForIx(t)
	defer testDB.Close()

	mock := &mockEmitter{}
	logStore := NewTaskLogStore(db)
	emitter := ats.NewLogCapturingEmitter(mock, logStore, "JB_test123")

	// Simulate multi-stage execution
	emitter.EmitStage("fetch_data", "Fetching data")
	emitter.EmitInfo("HTTP GET https://example.com/data")
	emitter.EmitStage("extract_content", "Extracting content")
	emitter.EmitInfo("LLM extraction in progress")
	emitter.EmitStage("persist_data", "Persisting to database")

	// Verify 5 log entries (3 stage transitions + 2 info messages)
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM task_logs WHERE job_id = ?", "JB_test123").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count logs: %v", err)
	}
	if count != 5 {
		t.Errorf("Expected 5 log entries, got %d", count)
	}

	// Verify stages are tracked correctly in order
	rows, err := db.Query("SELECT stage FROM task_logs WHERE job_id = ? ORDER BY id", "JB_test123")
	if err != nil {
		t.Fatalf("Failed to query stages: %v", err)
	}
	defer rows.Close()

	expectedStages := []string{"fetch_data", "fetch_data", "extract_content", "extract_content", "persist_data"}
	var stages []string
	for rows.Next() {
		var stage sql.NullString
		if err := rows.Scan(&stage); err != nil {
			t.Fatalf("Failed to scan stage: %v", err)
		}
		if stage.Valid {
			stages = append(stages, stage.String)
		} else {
			stages = append(stages, "")
		}
	}

	if len(stages) != len(expectedStages) {
		t.Fatalf("Expected %d stages, got %d", len(expectedStages), len(stages))
	}

	for i, expected := range expectedStages {
		if stages[i] != expected {
			t.Errorf("Stage %d: expected '%s', got '%s'", i, expected, stages[i])
		}
	}
}

// TestLogCapturingEmitter_ErrorHandling verifies errors don't break job execution
func TestLogCapturingEmitter_ErrorHandling(t *testing.T) {
	// Use invalid database to force write errors
	db := qntxtest.CreateTestDB(t)
	db.Close() // Close immediately to make writes fail

	mock := &mockEmitter{}
	logStore := NewTaskLogStore(db)
	emitter := ats.NewLogCapturingEmitter(mock, logStore, "JB_test123")

	// This should not panic even though database writes will fail
	emitter.EmitInfo("This should not crash")

	// Verify passthrough still works
	// Should have 2 calls: 1 database error warning + 1 original message
	if len(mock.infoCalls) != 2 {
		t.Errorf("Expected 2 info calls (1 warning + 1 original), got %d", len(mock.infoCalls))
	}

	// Verify first call is the database error warning (emitted during writeLog)
	if len(mock.infoCalls) > 0 && !containsSubstring(mock.infoCalls[0], "Failed to persist log to database") {
		t.Errorf("Expected first call to be database error warning, got: %s", mock.infoCalls[0])
	}

	// Verify second call is the original message (passthrough)
	if len(mock.infoCalls) > 1 && mock.infoCalls[1] != "This should not crash" {
		t.Errorf("Expected second call to be original message, got: %s", mock.infoCalls[1])
	}
}

// TestLogCapturingEmitter_Timestamps verifies timestamps are recorded
func TestLogCapturingEmitter_Timestamps(t *testing.T) {
	db := setupTestDBForIx(t)
	defer testDB.Close()

	mock := &mockEmitter{}
	logStore := NewTaskLogStore(db)
	emitter := ats.NewLogCapturingEmitter(mock, logStore, "JB_test123")

	before := time.Now().Add(-1 * time.Second) // 1 second tolerance
	emitter.EmitInfo("Test message")
	after := time.Now().Add(1 * time.Second)

	// Query timestamp
	var timestamp string
	err := db.QueryRow("SELECT timestamp FROM task_logs WHERE job_id = ?", "JB_test123").Scan(&timestamp)
	if err != nil {
		t.Fatalf("Failed to query timestamp: %v", err)
	}

	// Parse timestamp
	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t.Fatalf("Failed to parse timestamp '%s': %v", timestamp, err)
	}

	// Verify timestamp is within expected range (with tolerance)
	if ts.Before(before) || ts.After(after) {
		t.Errorf("Timestamp %s is outside expected range [%s, %s]", ts, before, after)
	}
}

// Helper function
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
