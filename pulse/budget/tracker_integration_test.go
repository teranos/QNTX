package budget

import (
	"database/sql"
	"testing"
	"time"

	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertUsageRecord inserts a record into ai_model_usage table for testing
func insertUsageRecord(t *testing.T, db *sql.DB, timestamp time.Time, cost float64, success bool) {
	query := `
		INSERT INTO ai_model_usage (
			operation_type, entity_type, entity_id,
			model_name, model_provider,
			request_timestamp, tokens_used, cost, success
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	successInt := 0
	if success {
		successInt = 1
	}

	_, err := db.Exec(query,
		"test-operation",                        // operation_type
		"test-entity",                           // entity_type
		"test-id",                               // entity_id
		"test-model",                            // model_name
		"test-provider",                         // model_provider
		timestamp.Format("2006-01-02 15:04:05"), // request_timestamp
		100,                                     // tokens_used
		cost,                                    // cost
		successInt,                              // success
	)
	require.NoError(t, err)
}

// TestBudgetTracker_GetStatus tests retrieving current budget status from ai_model_usage
// Note: Tests calendar-based windows (today, this month). See issue #198 for sliding window enhancement.
func TestBudgetTracker_GetStatus(t *testing.T) {

	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   10.0,
		MonthlyBudgetUSD: 100.0,
		CostPerScoreUSD:  0.002,
	}

	tracker := NewTracker(db, config)

	// Insert some usage records for today
	now := time.Now()
	insertUsageRecord(t, db, now.Add(-1*time.Hour), 2.50, true)
	insertUsageRecord(t, db, now.Add(-30*time.Minute), 1.25, true)
	insertUsageRecord(t, db, now.Add(-10*time.Minute), 0.75, true)

	// Get status
	status, err := tracker.GetStatus()
	require.NoError(t, err)

	// Verify daily spend
	assert.Equal(t, 4.50, status.DailySpend, "Daily spend should sum to $4.50")
	assert.Equal(t, 5.50, status.DailyRemaining, "Daily remaining should be $5.50")
	assert.Equal(t, 3, status.DailyOps, "Should have 3 daily operations")

	// Verify monthly spend (same as daily for this test)
	assert.Equal(t, 4.50, status.MonthlySpend, "Monthly spend should sum to $4.50")
	assert.Equal(t, 95.50, status.MonthlyRemaining, "Monthly remaining should be $95.50")
	assert.Equal(t, 3, status.MonthlyOps, "Should have 3 monthly operations")
}

// TestBudgetTracker_GetStatus_NoUsage tests status with no prior usage
func TestBudgetTracker_GetStatus_NoUsage(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   5.0,
		MonthlyBudgetUSD: 50.0,
		CostPerScoreUSD:  0.001,
	}

	tracker := NewTracker(db, config)

	status, err := tracker.GetStatus()
	require.NoError(t, err)

	assert.Equal(t, 0.0, status.DailySpend, "Daily spend should be $0")
	assert.Equal(t, 5.0, status.DailyRemaining, "Daily remaining should be full budget")
	assert.Equal(t, 0, status.DailyOps, "Should have 0 operations")

	assert.Equal(t, 0.0, status.MonthlySpend, "Monthly spend should be $0")
	assert.Equal(t, 50.0, status.MonthlyRemaining, "Monthly remaining should be full budget")
	assert.Equal(t, 0, status.MonthlyOps, "Should have 0 operations")
}

// TestBudgetTracker_CheckBudget_WithinLimits tests budget check when within limits
func TestBudgetTracker_CheckBudget_WithinLimits(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   10.0,
		MonthlyBudgetUSD: 100.0,
		CostPerScoreUSD:  0.002,
	}

	tracker := NewTracker(db, config)

	// Insert some usage ($3.00 spent)
	now := time.Now()
	insertUsageRecord(t, db, now.Add(-1*time.Hour), 2.00, true)
	insertUsageRecord(t, db, now.Add(-30*time.Minute), 1.00, true)

	// Check if we can spend another $5.00 (should pass - total would be $8.00)
	err := tracker.CheckBudget(5.00)
	assert.NoError(t, err, "Should allow operation within budget")
}

// TestBudgetTracker_CheckBudget_ExceedsDailyLimit tests daily budget enforcement
// Note: Tests calendar-based daily limit. See issue #198 for sliding window enhancement.
func TestBudgetTracker_CheckBudget_ExceedsDailyLimit(t *testing.T) {

	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   5.0,
		MonthlyBudgetUSD: 100.0,
		CostPerScoreUSD:  0.002,
	}

	tracker := NewTracker(db, config)

	// Insert usage that brings us close to daily limit ($4.50 spent)
	now := time.Now()
	insertUsageRecord(t, db, now.Add(-1*time.Hour), 3.00, true)
	insertUsageRecord(t, db, now.Add(-30*time.Minute), 1.50, true)

	// Try to spend another $1.00 (would total $5.50 > $5.00 limit)
	err := tracker.CheckBudget(1.00)
	require.Error(t, err, "Should reject operation exceeding daily budget")
	assert.Contains(t, err.Error(), "daily budget would be exceeded")
	assert.Contains(t, err.Error(), "4.500") // Current spend
	assert.Contains(t, err.Error(), "1.000") // Estimated cost
	assert.Contains(t, err.Error(), "5.00")  // Limit
}

// TestBudgetTracker_CheckBudget_ExceedsMonthlyLimit tests monthly budget enforcement
func TestBudgetTracker_CheckBudget_ExceedsMonthlyLimit(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   20.0, // High daily limit
		MonthlyBudgetUSD: 50.0, // Low monthly limit
		CostPerScoreUSD:  0.002,
	}

	tracker := NewTracker(db, config)

	// Insert usage across multiple days in current month ($48.00 spent)
	now := time.Now()
	insertUsageRecord(t, db, now.AddDate(0, 0, -5), 15.00, true) // 5 days ago
	insertUsageRecord(t, db, now.AddDate(0, 0, -3), 18.00, true) // 3 days ago
	insertUsageRecord(t, db, now.Add(-1*time.Hour), 15.00, true) // Today

	// Try to spend another $5.00 (would total $53.00 > $50.00 limit)
	err := tracker.CheckBudget(5.00)
	require.Error(t, err, "Should reject operation exceeding monthly budget")
	assert.Contains(t, err.Error(), "monthly budget would be exceeded")
	assert.Contains(t, err.Error(), "48.000") // Current spend
	assert.Contains(t, err.Error(), "5.000")  // Estimated cost
	assert.Contains(t, err.Error(), "50.00")  // Limit
}

// TestBudgetTracker_CheckBudget_ExactLimit tests edge case at exact budget limit
func TestBudgetTracker_CheckBudget_ExactLimit(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   10.0,
		MonthlyBudgetUSD: 100.0,
		CostPerScoreUSD:  0.002,
	}

	tracker := NewTracker(db, config)

	// Insert usage ($7.00 spent)
	now := time.Now()
	insertUsageRecord(t, db, now.Add(-1*time.Hour), 7.00, true)

	// Check if we can spend exactly $3.00 (total = $10.00, exactly at limit)
	err := tracker.CheckBudget(3.00)
	assert.NoError(t, err, "Should allow operation that exactly reaches budget limit")

	// Check if we can spend $3.01 (would exceed by $0.01)
	err = tracker.CheckBudget(3.01)
	require.Error(t, err, "Should reject operation exceeding budget by even $0.01")
}

// TestBudgetTracker_EstimateOperationCost tests cost estimation
func TestBudgetTracker_EstimateOperationCost(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   10.0,
		MonthlyBudgetUSD: 100.0,
		CostPerScoreUSD:  0.0025, // $0.0025 per score
	}

	tracker := NewTracker(db, config)

	testCases := []struct {
		numScores    int
		expectedCost float64
	}{
		{1, 0.0025},
		{10, 0.025},
		{100, 0.25},
		{1000, 2.50},
		{0, 0.0},
	}

	for _, tc := range testCases {
		t.Run(string(rune(tc.numScores)), func(t *testing.T) {
			cost := tracker.EstimateOperationCost(tc.numScores)
			assert.Equal(t, tc.expectedCost, cost,
				"Estimating cost for %d scores should be $%.4f", tc.numScores, tc.expectedCost)
		})
	}
}

// TestBudgetTracker_UpdateDailyBudget tests updating daily budget limits
func TestBudgetTracker_UpdateDailyBudget(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   5.0,
		MonthlyBudgetUSD: 50.0,
		CostPerScoreUSD:  0.001,
	}

	tracker := NewTracker(db, config)

	// Update daily budget to $15.00
	err := tracker.UpdateDailyBudget(15.0)
	// Note: This will fail in tests because UpdateDailyBudget tries to persist to config.toml
	// For now, just verify the error handling
	if err != nil {
		// Expected in test environment - config persistence not available
		assert.Contains(t, err.Error(), "failed to persist budget to config")
	}

	// Verify in-memory config was updated even if persistence failed
	limits := tracker.GetBudgetLimits()
	assert.Equal(t, 15.0, limits.DailyBudgetUSD, "In-memory daily budget should be updated")
}

// TestBudgetTracker_UpdateMonthlyBudget tests updating monthly budget limits
func TestBudgetTracker_UpdateMonthlyBudget(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   5.0,
		MonthlyBudgetUSD: 50.0,
		CostPerScoreUSD:  0.001,
	}

	tracker := NewTracker(db, config)

	// Update monthly budget to $200.00
	err := tracker.UpdateMonthlyBudget(200.0)
	// Note: This will fail in tests because UpdateMonthlyBudget tries to persist to config.toml
	if err != nil {
		// Expected in test environment - config persistence not available
		assert.Contains(t, err.Error(), "failed to persist budget to config")
	}

	// Verify in-memory config was updated even if persistence failed
	limits := tracker.GetBudgetLimits()
	assert.Equal(t, 200.0, limits.MonthlyBudgetUSD, "In-memory monthly budget should be updated")
}

// TestBudgetTracker_UpdateBudget_NegativeValues tests validation of negative budgets
func TestBudgetTracker_UpdateBudget_NegativeValues(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   5.0,
		MonthlyBudgetUSD: 50.0,
		CostPerScoreUSD:  0.001,
	}

	tracker := NewTracker(db, config)

	// Try to set negative daily budget
	err := tracker.UpdateDailyBudget(-10.0)
	require.Error(t, err, "Should reject negative daily budget")
	assert.Contains(t, err.Error(), "daily budget cannot be negative")

	// Try to set negative monthly budget
	err = tracker.UpdateMonthlyBudget(-100.0)
	require.Error(t, err, "Should reject negative monthly budget")
	assert.Contains(t, err.Error(), "monthly budget cannot be negative")

	// Verify original budgets unchanged
	limits := tracker.GetBudgetLimits()
	assert.Equal(t, 5.0, limits.DailyBudgetUSD, "Daily budget should remain unchanged")
	assert.Equal(t, 50.0, limits.MonthlyBudgetUSD, "Monthly budget should remain unchanged")
}

// TestBudgetTracker_IgnoresFailedOperations tests that failed operations don't count toward budget
func TestBudgetTracker_IgnoresFailedOperations(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   10.0,
		MonthlyBudgetUSD: 100.0,
		CostPerScoreUSD:  0.002,
	}

	tracker := NewTracker(db, config)

	// Insert successful operations ($3.00)
	now := time.Now()
	insertUsageRecord(t, db, now.Add(-2*time.Hour), 1.50, true)
	insertUsageRecord(t, db, now.Add(-1*time.Hour), 1.50, true)

	// Insert failed operations (should be ignored)
	insertUsageRecord(t, db, now.Add(-90*time.Minute), 5.00, false)
	insertUsageRecord(t, db, now.Add(-45*time.Minute), 3.00, false)

	status, err := tracker.GetStatus()
	require.NoError(t, err)

	// Only successful operations count
	assert.Equal(t, 3.0, status.DailySpend, "Should only count successful operations")
	assert.Equal(t, 2, status.DailyOps, "Should only count 2 successful operations")
	assert.Equal(t, 7.0, status.DailyRemaining, "Remaining should be based on successful ops only")
}

// TestBudgetTracker_MultipleConcurrentOperations tests thread safety
func TestBudgetTracker_MultipleConcurrentOperations(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   100.0,
		MonthlyBudgetUSD: 1000.0,
		CostPerScoreUSD:  0.002,
	}

	tracker := NewTracker(db, config)

	// Test concurrent reads (GetBudgetLimits and EstimateOperationCost don't hit DB)
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			// GetBudgetLimits tests read lock
			limits := tracker.GetBudgetLimits()
			assert.Equal(t, 100.0, limits.DailyBudgetUSD)
			assert.Equal(t, 1000.0, limits.MonthlyBudgetUSD)

			// EstimateOperationCost tests concurrent reads
			cost := tracker.EstimateOperationCost(10)
			assert.Equal(t, 0.02, cost)

			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestBudgetTracker_ZeroBudget tests edge case with zero budget
func TestBudgetTracker_ZeroBudget(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   0.0, // Zero budget
		MonthlyBudgetUSD: 0.0,
		CostPerScoreUSD:  0.002,
	}

	tracker := NewTracker(db, config)

	// Any spend should be rejected
	err := tracker.CheckBudget(0.01)
	require.Error(t, err, "Should reject any spend with zero budget")
	assert.Contains(t, err.Error(), "daily budget would be exceeded")

	// Zero cost should be allowed
	err = tracker.CheckBudget(0.0)
	assert.NoError(t, err, "Zero cost should always be allowed")
}

// TestBudgetTracker_GetBudgetLimits tests retrieving current budget configuration
func TestBudgetTracker_GetBudgetLimits(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	config := BudgetConfig{
		DailyBudgetUSD:   12.50,
		MonthlyBudgetUSD: 250.75,
		CostPerScoreUSD:  0.0035,
	}

	tracker := NewTracker(db, config)

	limits := tracker.GetBudgetLimits()
	assert.Equal(t, 12.50, limits.DailyBudgetUSD)
	assert.Equal(t, 250.75, limits.MonthlyBudgetUSD)
	assert.Equal(t, 0.0035, limits.CostPerScoreUSD)
}
