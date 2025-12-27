package budget

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TestTracker_ReadsFromActualUsage verifies that Tracker reads actual spend from ai_model_usage
func TestTracker_ReadsFromActualUsage(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Given: 3 API calls totaling $3.50 recorded in ai_model_usage
	today := time.Now()
	insertUsage(t, db, today, 1.50) // Call 1
	insertUsage(t, db, today, 1.00) // Call 2
	insertUsage(t, db, today, 1.00) // Call 3

	// Create budget tracker with $5 daily limit
	config := BudgetConfig{
		DailyBudgetUSD:   5.00,
		MonthlyBudgetUSD: 30.00,
		CostPerScoreUSD:  0.002,
	}
	tracker := NewTracker(db, config)

	// When: GetStatus() called
	status, err := tracker.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus() error = %v", err)
	}

	// Then: Returns DailySpend=$3.50, DailyRemaining=$1.50
	expectedSpend := 3.50
	expectedRemaining := 1.50
	tolerance := 0.01

	if abs(status.DailySpend-expectedSpend) > tolerance {
		t.Errorf("DailySpend = $%.2f, want $%.2f", status.DailySpend, expectedSpend)
	}
	if abs(status.DailyRemaining-expectedRemaining) > tolerance {
		t.Errorf("DailyRemaining = $%.2f, want $%.2f", status.DailyRemaining, expectedRemaining)
	}
}

// TestTracker_EnforcesDailyLimit verifies that budget enforcement blocks jobs when daily limit exceeded
func TestTracker_EnforcesDailyLimit(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Given: $4.50 spent today in ai_model_usage
	today := time.Now()
	insertUsage(t, db, today, 4.50)

	// Create budget tracker with $5 daily limit
	config := BudgetConfig{
		DailyBudgetUSD:   5.00,
		MonthlyBudgetUSD: 30.00,
		CostPerScoreUSD:  0.002,
	}
	tracker := NewTracker(db, config)

	// When: CheckBudget($1.00) called (would exceed $5.00 limit)
	err := tracker.CheckBudget(1.00)

	// Then: Error "daily budget would be exceeded"
	if err == nil {
		t.Fatal("CheckBudget() should return error when daily limit exceeded")
	}
	if !contains(err.Error(), "daily budget would be exceeded") {
		t.Errorf("Expected 'daily budget would be exceeded' error, got: %v", err)
	}
}

// TestTracker_AllowsWithinLimits verifies that jobs are allowed when within budget
func TestTracker_AllowsWithinLimits(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Given: $2.00 spent today
	today := time.Now()
	insertUsage(t, db, today, 2.00)

	// Create budget tracker with $5 daily limit
	config := BudgetConfig{
		DailyBudgetUSD:   5.00,
		MonthlyBudgetUSD: 30.00,
		CostPerScoreUSD:  0.002,
	}
	tracker := NewTracker(db, config)

	// When: CheckBudget($1.00) called (within limits)
	err := tracker.CheckBudget(1.00)

	// Then: Succeeds (no error)
	if err != nil {
		t.Errorf("CheckBudget() should succeed when within limits, got error: %v", err)
	}
}

// TestTracker_EnforcesMonthlyLimit verifies that monthly budget limit is enforced
func TestTracker_EnforcesMonthlyLimit(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Given: $28 spent this month in ai_model_usage
	now := time.Now()

	// Spread across multiple days this month
	for i := 1; i <= 28; i++ {
		dayOfMonth := time.Date(now.Year(), now.Month(), min(i, daysInMonth(now)), 12, 0, 0, 0, time.UTC)
		insertUsage(t, db, dayOfMonth, 1.00) // $1 per day for 28 days
	}

	// Create budget tracker with $30 monthly limit
	config := BudgetConfig{
		DailyBudgetUSD:   10.00, // High daily limit
		MonthlyBudgetUSD: 30.00,
		CostPerScoreUSD:  0.002,
	}
	tracker := NewTracker(db, config)

	// When: CheckBudget($5.00) called (would exceed $30.00 monthly limit)
	err := tracker.CheckBudget(5.00)

	// Then: Error "monthly budget would be exceeded"
	if err == nil {
		t.Fatal("CheckBudget() should return error when monthly limit exceeded")
	}
	if !contains(err.Error(), "monthly budget would be exceeded") {
		t.Errorf("Expected 'monthly budget would be exceeded' error, got: %v", err)
	}
}

// TestTracker_MultipleJobsCounted verifies that all jobs' usage is correctly summed
func TestTracker_MultipleJobsCounted(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Given: Job A cost $2.50, Job B cost $1.50 (both today)
	today := time.Now()
	insertUsage(t, db, today, 2.50) // Job A
	insertUsage(t, db, today, 1.50) // Job B

	// Create budget tracker with $5 daily limit
	config := BudgetConfig{
		DailyBudgetUSD:   5.00,
		MonthlyBudgetUSD: 30.00,
		CostPerScoreUSD:  0.002,
	}
	tracker := NewTracker(db, config)

	// When: Job C calls CheckBudget($2.00)
	// Total would be: $2.50 + $1.50 + $2.00 = $6.00 > $5.00
	err := tracker.CheckBudget(2.00)

	// Then: Blocked (daily budget exceeded)
	if err == nil {
		t.Fatal("CheckBudget() should block Job C when combined spend exceeds limit")
	}
	if !contains(err.Error(), "daily budget would be exceeded") {
		t.Errorf("Expected daily budget error, got: %v", err)
	}
}

// Helper functions

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

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

func insertUsage(t *testing.T, db *sql.DB, timestamp time.Time, costUSD float64) {
	t.Helper()

	query := `
		INSERT INTO ai_model_usage (
			model_provider, model_name, operation_type, tokens_used, cost,
			success, request_timestamp, entity_type, entity_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := db.Exec(query,
		"openrouter",
		"anthropic/claude-3.5-sonnet",
		"test-operation",
		1000,       // tokens
		costUSD,    // cost
		1,          // success
		timestamp,  // request_timestamp
		"test",     // entity_type
		"test-id",  // entity_id
		time.Now(), // created_at
	)

	if err != nil {
		t.Fatalf("Failed to insert usage record: %v", err)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func daysInMonth(t time.Time) int {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
