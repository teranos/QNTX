package tracker

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	_ "github.com/mattn/go-sqlite3"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Create the ai_model_usage table
	createTableSQL := `
	CREATE TABLE ai_model_usage (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		operation_type TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		model_name TEXT NOT NULL,
		model_provider TEXT NOT NULL,
		model_config TEXT,
		request_timestamp DATETIME NOT NULL,
		response_timestamp DATETIME,
		tokens_used INTEGER,
		cost REAL,
		success BOOLEAN NOT NULL,
		error_message TEXT,
		metadata TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`

	if _, err := db.Exec(createTableSQL); err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	return db
}

func TestNewUsageTracker(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	tracker := NewUsageTracker(db, 1)

	if tracker == nil {
		t.Fatal("NewUsageTracker returned nil")
	}

	if tracker.db != db {
		t.Error("UsageTracker database not set correctly")
	}

	if tracker.verbosity != 1 {
		t.Errorf("Expected verbosity 1, got %d", tracker.verbosity)
	}
}

func TestTrackUsage(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	tracker := NewUsageTracker(db, 1)

	now := time.Now()
	responseTime := now.Add(2 * time.Second)
	tokens := 150
	cost := 0.05

	usage := &ModelUsage{
		OperationType:     "persona-extraction",
		EntityType:        "event",
		EntityID:          "123",
		ModelName:         "gpt-4o-mini",
		ModelProvider:     "openrouter",
		ModelConfig:       NewModelConfig(float64Ptr(0.2), intPtr(2000)),
		RequestTimestamp:  now,
		ResponseTimestamp: &responseTime,
		TokensUsed:        &tokens,
		Cost:              &cost,
		Success:           true,
		ErrorMessage:      nil,
		Metadata:          NewUsageMetadata(UsageMetadata{OperationDetail: "Test event"}),
	}

	err := tracker.TrackUsage(usage)
	if err != nil {
		t.Fatalf("TrackUsage failed: %v", err)
	}

	// Verify the record was inserted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM ai_model_usage").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count records: %v", err)
	}

	if count != 1 {
		t.Errorf("Expected 1 record, got %d", count)
	}

	// Verify the record details
	var storedUsage ModelUsage
	row := db.QueryRow(`
		SELECT operation_type, entity_type, entity_id, model_name, model_provider,
		       tokens_used, cost, success
		FROM ai_model_usage WHERE id = 1`)

	err = row.Scan(&storedUsage.OperationType, &storedUsage.EntityType, &storedUsage.EntityID,
		&storedUsage.ModelName, &storedUsage.ModelProvider, &storedUsage.TokensUsed,
		&storedUsage.Cost, &storedUsage.Success)
	if err != nil {
		t.Fatalf("Failed to retrieve stored usage: %v", err)
	}

	if storedUsage.OperationType != "persona-extraction" {
		t.Errorf("Expected operation_type 'persona-extraction', got '%s'", storedUsage.OperationType)
	}
	if storedUsage.ModelName != "gpt-4o-mini" {
		t.Errorf("Expected model_name 'gpt-4o-mini', got '%s'", storedUsage.ModelName)
	}
	if *storedUsage.TokensUsed != 150 {
		t.Errorf("Expected tokens_used 150, got %d", *storedUsage.TokensUsed)
	}
	if *storedUsage.Cost != 0.05 {
		t.Errorf("Expected cost 0.05, got %f", *storedUsage.Cost)
	}
	if !storedUsage.Success {
		t.Error("Expected success to be true")
	}
}

func TestTrackUsageWithError(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	tracker := NewUsageTracker(db, 1)

	now := time.Now()
	errorMsg := "API key invalid"

	usage := &ModelUsage{
		OperationType:    "contact-scoring",
		EntityType:       "contact",
		EntityID:         "contact-456",
		ModelName:        "claude-3-haiku",
		ModelProvider:    "openrouter",
		RequestTimestamp: now,
		Success:          false,
		ErrorMessage:     &errorMsg,
	}

	err := tracker.TrackUsage(usage)
	if err != nil {
		t.Fatalf("TrackUsage failed: %v", err)
	}

	// Verify error tracking
	var storedSuccess bool
	var storedErrorMsg sql.NullString
	err = db.QueryRow("SELECT success, error_message FROM ai_model_usage WHERE id = 1").Scan(&storedSuccess, &storedErrorMsg)
	if err != nil {
		t.Fatalf("Failed to retrieve error record: %v", err)
	}

	if storedSuccess {
		t.Error("Expected success to be false for error case")
	}
	if !storedErrorMsg.Valid || storedErrorMsg.String != "API key invalid" {
		t.Errorf("Expected error message 'API key invalid', got '%s'", storedErrorMsg.String)
	}
}

func TestGetUsageStats(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	tracker := NewUsageTracker(db, 1)

	// Insert test data
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	testUsages := []*ModelUsage{
		{
			OperationType:    "persona-extraction",
			EntityType:       "event",
			EntityID:         "1",
			ModelName:        "gpt-4o-mini",
			ModelProvider:    "openrouter",
			RequestTimestamp: oneHourAgo,
			TokensUsed:       intPtr(100),
			Cost:             float64Ptr(0.02),
			Success:          true,
		},
		{
			OperationType:    "contact-scoring",
			EntityType:       "contact",
			EntityID:         "2",
			ModelName:        "claude-3-haiku",
			ModelProvider:    "openrouter",
			RequestTimestamp: oneHourAgo,
			TokensUsed:       intPtr(150),
			Cost:             float64Ptr(0.03),
			Success:          true,
		},
		{
			OperationType:    "contact-scoring",
			EntityType:       "contact",
			EntityID:         "3",
			ModelName:        "gpt-4o-mini",
			ModelProvider:    "openrouter",
			RequestTimestamp: oneHourAgo,
			TokensUsed:       intPtr(0),
			Cost:             float64Ptr(0.0),
			Success:          false,
		},
	}

	for _, usage := range testUsages {
		if err := tracker.TrackUsage(usage); err != nil {
			t.Fatalf("Failed to insert test usage: %v", err)
		}
	}

	// Test usage stats since 2 hours ago (should include all records)
	twoHoursAgo := now.Add(-2 * time.Hour)
	stats, err := tracker.GetUsageStats(twoHoursAgo)
	if err != nil {
		t.Fatalf("GetUsageStats failed: %v", err)
	}

	if stats.TotalRequests != 3 {
		t.Errorf("Expected 3 total requests, got %d", stats.TotalRequests)
	}
	if stats.SuccessfulRequests != 2 {
		t.Errorf("Expected 2 successful requests, got %d", stats.SuccessfulRequests)
	}
	if stats.TotalTokens != 250 {
		t.Errorf("Expected 250 total tokens, got %d", stats.TotalTokens)
	}
	if stats.TotalCost != 0.05 {
		t.Errorf("Expected total cost 0.05, got %f", stats.TotalCost)
	}
	if stats.UniqueModels != 2 {
		t.Errorf("Expected 2 unique models, got %d", stats.UniqueModels)
	}

	expectedSuccessRate := float64(2) / float64(3)
	if abs(stats.SuccessRate-expectedSuccessRate) > 0.001 {
		t.Errorf("Expected success rate %f, got %f", expectedSuccessRate, stats.SuccessRate)
	}

	// Test usage stats since 30 minutes ago (should include none)
	thirtyMinutesAgo := now.Add(-30 * time.Minute)
	recentStats, err := tracker.GetUsageStats(thirtyMinutesAgo)
	if err != nil {
		t.Fatalf("GetUsageStats for recent period failed: %v", err)
	}

	if recentStats.TotalRequests != 0 {
		t.Errorf("Expected 0 recent requests, got %d", recentStats.TotalRequests)
	}
	if recentStats.TotalTokens != 0 {
		t.Errorf("Expected 0 recent tokens, got %d", recentStats.TotalTokens)
	}
	if recentStats.TotalCost != 0 {
		t.Errorf("Expected 0 recent cost, got %f", recentStats.TotalCost)
	}
}

func TestGetModelBreakdown(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	tracker := NewUsageTracker(db, 1)

	// Insert test data with different models
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	responseTime := oneHourAgo.Add(2 * time.Second)

	testUsages := []*ModelUsage{
		{
			OperationType:     "persona-extraction",
			EntityType:        "event",
			EntityID:          "1",
			ModelName:         "gpt-4o-mini",
			ModelProvider:     "openrouter",
			RequestTimestamp:  oneHourAgo,
			ResponseTimestamp: &responseTime,
			TokensUsed:        intPtr(100),
			Cost:              float64Ptr(0.02),
			Success:           true,
		},
		{
			OperationType:     "persona-extraction",
			EntityType:        "event",
			EntityID:          "2",
			ModelName:         "gpt-4o-mini",
			ModelProvider:     "openrouter",
			RequestTimestamp:  oneHourAgo,
			ResponseTimestamp: &responseTime,
			TokensUsed:        intPtr(200),
			Cost:              float64Ptr(0.04),
			Success:           true,
		},
		{
			OperationType:     "contact-scoring",
			EntityType:        "contact",
			EntityID:          "3",
			ModelName:         "claude-3-haiku",
			ModelProvider:     "openrouter",
			RequestTimestamp:  oneHourAgo,
			ResponseTimestamp: &responseTime,
			TokensUsed:        intPtr(150),
			Cost:              float64Ptr(0.03),
			Success:           true,
		},
	}

	for _, usage := range testUsages {
		if err := tracker.TrackUsage(usage); err != nil {
			t.Fatalf("Failed to insert test usage: %v", err)
		}
	}

	// Get model breakdown
	twoHoursAgo := now.Add(-2 * time.Hour)
	breakdown, err := tracker.GetModelBreakdown(twoHoursAgo)
	if err != nil {
		t.Fatalf("GetModelBreakdown failed: %v", err)
	}

	if len(breakdown) != 2 {
		t.Errorf("Expected 2 models in breakdown, got %d", len(breakdown))
	}

	// Find GPT-4o-mini breakdown (should be first due to higher cost)
	var gptBreakdown *ModelBreakdown
	for _, mb := range breakdown {
		if mb.ModelName == "gpt-4o-mini" {
			gptBreakdown = &mb
			break
		}
	}

	if gptBreakdown == nil {
		t.Fatal("Could not find gpt-4o-mini in breakdown")
	}

	if gptBreakdown.RequestCount != 2 {
		t.Errorf("Expected 2 requests for gpt-4o-mini, got %d", gptBreakdown.RequestCount)
	}
	if gptBreakdown.TotalTokens != 300 {
		t.Errorf("Expected 300 total tokens for gpt-4o-mini, got %d", gptBreakdown.TotalTokens)
	}
	if gptBreakdown.TotalCost != 0.06 {
		t.Errorf("Expected total cost 0.06 for gpt-4o-mini, got %f", gptBreakdown.TotalCost)
	}
	if gptBreakdown.AvgResponseTimeMs == nil {
		t.Error("Expected non-nil avg response time")
	} else if abs(*gptBreakdown.AvgResponseTimeMs-2000) > 1 {
		t.Errorf("Expected avg response time ~2000ms, got %f", *gptBreakdown.AvgResponseTimeMs)
	}
}

func TestNewModelConfig(t *testing.T) {
	// Test with both parameters
	temp := 0.7
	maxTokens := 1000
	config := NewModelConfig(&temp, &maxTokens)

	if config == nil {
		t.Fatal("NewModelConfig returned nil")
	}

	// Should contain valid JSON
	if *config == "" {
		t.Error("Expected non-empty config string")
	}

	// Test with nil parameters
	nilConfig := NewModelConfig(nil, nil)
	if nilConfig != nil {
		t.Error("Expected nil config for nil parameters")
	}

	// Test with one parameter
	tempOnlyConfig := NewModelConfig(&temp, nil)
	if tempOnlyConfig == nil {
		t.Error("Expected non-nil config with temperature only")
	}
}

func TestNewUsageMetadata(t *testing.T) {
	metadata := UsageMetadata{
		OperationDetail: "Test operation",
		InputLength:     intPtr(100),
		OutputLength:    intPtr(50),
	}

	metadataStr := NewUsageMetadata(metadata)

	if metadataStr == nil {
		t.Fatal("NewUsageMetadata returned nil")
	}

	// Should contain valid JSON
	if *metadataStr == "" {
		t.Error("Expected non-empty metadata string")
	}
}

// --- Sqlmock Tests ---
// Minimal sqlmock tests to verify database operations and SQL query structure

func TestTrackUsage_Sqlmock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer db.Close()

	tracker := NewUsageTracker(db, 1)

	now := time.Now()
	tokens := 100
	cost := 0.02

	usage := &ModelUsage{
		OperationType:    "test-operation",
		EntityType:       "test-entity",
		EntityID:         "123",
		ModelName:        "gpt-4o-mini",
		ModelProvider:    "openrouter",
		RequestTimestamp: now,
		TokensUsed:       &tokens,
		Cost:             &cost,
		Success:          true,
	}

	// Expect the exact INSERT query
	mock.ExpectExec(`INSERT INTO ai_model_usage`).
		WithArgs(
			usage.OperationType,
			usage.EntityType,
			usage.EntityID,
			usage.ModelName,
			usage.ModelProvider,
			sqlmock.AnyArg(), // model_config
			usage.RequestTimestamp,
			sqlmock.AnyArg(), // response_timestamp
			usage.TokensUsed,
			usage.Cost,
			usage.Success,
			sqlmock.AnyArg(), // error_message
			sqlmock.AnyArg(), // metadata
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = tracker.TrackUsage(usage)
	if err != nil {
		t.Errorf("TrackUsage failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetUsageStats_Sqlmock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer db.Close()

	tracker := NewUsageTracker(db, 1)

	since := time.Now().Add(-1 * time.Hour)

	// Mock the expected query and return values
	rows := sqlmock.NewRows([]string{
		"total_requests",
		"successful_requests",
		"total_tokens",
		"total_cost",
		"unique_models",
	}).AddRow(10, 8, 1500, 0.50, 3)

	mock.ExpectQuery(`SELECT.*FROM ai_model_usage WHERE request_timestamp`).
		WithArgs(since).
		WillReturnRows(rows)

	stats, err := tracker.GetUsageStats(since)
	if err != nil {
		t.Errorf("GetUsageStats failed: %v", err)
	}

	if stats.TotalRequests != 10 {
		t.Errorf("Expected 10 total requests, got %d", stats.TotalRequests)
	}
	if stats.SuccessfulRequests != 8 {
		t.Errorf("Expected 8 successful requests, got %d", stats.SuccessfulRequests)
	}
	if stats.TotalTokens != 1500 {
		t.Errorf("Expected 1500 total tokens, got %d", stats.TotalTokens)
	}
	if stats.TotalCost != 0.50 {
		t.Errorf("Expected 0.50 total cost, got %f", stats.TotalCost)
	}
	if stats.UniqueModels != 3 {
		t.Errorf("Expected 3 unique models, got %d", stats.UniqueModels)
	}

	expectedSuccessRate := float64(8) / float64(10)
	if abs(stats.SuccessRate-expectedSuccessRate) > 0.001 {
		t.Errorf("Expected success rate %f, got %f", expectedSuccessRate, stats.SuccessRate)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestTrackUsageWithError_Sqlmock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer db.Close()

	tracker := NewUsageTracker(db, 1)

	now := time.Now()
	errorMsg := "API rate limit exceeded"

	usage := &ModelUsage{
		OperationType:    "contact-scoring",
		EntityType:       "contact",
		EntityID:         "456",
		ModelName:        "claude-3-haiku",
		ModelProvider:    "openrouter",
		RequestTimestamp: now,
		Success:          false,
		ErrorMessage:     &errorMsg,
	}

	// Expect INSERT with error fields populated
	mock.ExpectExec(`INSERT INTO ai_model_usage`).
		WithArgs(
			usage.OperationType,
			usage.EntityType,
			usage.EntityID,
			usage.ModelName,
			usage.ModelProvider,
			sqlmock.AnyArg(), // model_config (nil)
			usage.RequestTimestamp,
			sqlmock.AnyArg(), // response_timestamp (nil)
			sqlmock.AnyArg(), // tokens_used (nil)
			sqlmock.AnyArg(), // cost (nil)
			false,            // success = false
			&errorMsg,        // error_message
			sqlmock.AnyArg(), // metadata (nil)
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = tracker.TrackUsage(usage)
	if err != nil {
		t.Errorf("TrackUsage failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetModelBreakdown_Sqlmock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}
	defer db.Close()

	tracker := NewUsageTracker(db, 1)

	since := time.Now().Add(-2 * time.Hour)

	// Mock the expected query with multiple models
	avgResponseTime1 := 2000.0
	avgResponseTime2 := 1500.0

	rows := sqlmock.NewRows([]string{
		"model_name",
		"model_provider",
		"request_count",
		"total_tokens",
		"total_cost",
		"avg_response_time_ms",
	}).
		AddRow("gpt-4o-mini", "openrouter", 2, 300, 0.06, avgResponseTime1).
		AddRow("claude-3-haiku", "openrouter", 1, 150, 0.03, avgResponseTime2)

	mock.ExpectQuery(`SELECT.*FROM ai_model_usage WHERE request_timestamp.*AND success.*GROUP BY model_name, model_provider ORDER BY total_cost DESC`).
		WithArgs(since).
		WillReturnRows(rows)

	breakdown, err := tracker.GetModelBreakdown(since)
	if err != nil {
		t.Errorf("GetModelBreakdown failed: %v", err)
	}

	if len(breakdown) != 2 {
		t.Errorf("Expected 2 models in breakdown, got %d", len(breakdown))
	}

	// Verify first model (highest cost - gpt-4o-mini)
	if breakdown[0].ModelName != "gpt-4o-mini" {
		t.Errorf("Expected first model to be gpt-4o-mini, got %s", breakdown[0].ModelName)
	}
	if breakdown[0].RequestCount != 2 {
		t.Errorf("Expected 2 requests for gpt-4o-mini, got %d", breakdown[0].RequestCount)
	}
	if breakdown[0].TotalTokens != 300 {
		t.Errorf("Expected 300 total tokens, got %d", breakdown[0].TotalTokens)
	}
	if breakdown[0].TotalCost != 0.06 {
		t.Errorf("Expected 0.06 total cost, got %f", breakdown[0].TotalCost)
	}
	if breakdown[0].AvgResponseTimeMs == nil {
		t.Error("Expected non-nil avg response time")
	} else if *breakdown[0].AvgResponseTimeMs != 2000.0 {
		t.Errorf("Expected 2000.0 avg response time, got %f", *breakdown[0].AvgResponseTimeMs)
	}

	// Verify second model (lower cost - claude-3-haiku)
	if breakdown[1].ModelName != "claude-3-haiku" {
		t.Errorf("Expected second model to be claude-3-haiku, got %s", breakdown[1].ModelName)
	}
	if breakdown[1].TotalCost != 0.03 {
		t.Errorf("Expected 0.03 total cost, got %f", breakdown[1].TotalCost)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

// TODO: Add comprehensive sqlmock tests for edge cases and error handling:
// - Test TrackUsage database error handling (connection failures, constraint violations)
// - Test GetUsageStats with zero records (verify success rate calculation)
// - Test GetModelBreakdown with multiple models and sorting order
// - Test concurrent TrackUsage calls (race conditions)
// - Test NULL handling for optional fields (response_timestamp, tokens_used, cost)
// - Test very large token counts and costs (overflow/precision)
// - Test invalid timestamps (future dates, very old dates)
// - Test SQL injection prevention in model names and entity IDs
// - Benchmark tests for bulk insert performance
// - Test transaction rollback scenarios

// TODO: Add integration tests for real-world usage patterns:
// - Track multiple operations across different time periods
// - Verify cost aggregation accuracy over extended periods
// - Test timezone handling in timestamps
// - Test model breakdown ordering (by cost DESC)
// - Validate JSON serialization of ModelConfig and UsageMetadata

// Helper functions for test data creation
func intPtr(i int) *int {
	return &i
}

func float64Ptr(f float64) *float64 {
	return &f
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
