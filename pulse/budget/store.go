// Package budget provides budget tracking for Pulse operations.
// Uses pure sliding windows (24h/7d/30d) on ai_model_usage table for accurate budget enforcement.
package budget

import (
	"database/sql"
	"fmt"
)

// Store handles budget queries against ai_model_usage table
type Store struct {
	db *sql.DB
}

// NewStore creates a new budget store
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// getActualSpend queries ai_model_usage for spend within a sliding time window
// Reduces repetition across GetActualDailySpend, GetActualWeeklySpend, GetActualMonthlySpend
func (s *Store) getActualSpend(window string, period string) (totalCost float64, opCount int, err error) {
	query := fmt.Sprintf(`
		SELECT
			COALESCE(SUM(cost), 0) as total_cost,
			COUNT(*) as operation_count
		FROM ai_model_usage
		WHERE request_timestamp >= datetime('now', '%s')
			AND success = 1
	`, window)

	err = s.db.QueryRow(query).Scan(&totalCost, &opCount)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query %s spend: %w", period, err)
	}

	return totalCost, opCount, nil
}

// GetActualDailySpend queries ai_model_usage table for last 24 hours of spend
// Uses sliding 24-hour window to prevent midnight gaming
func (s *Store) GetActualDailySpend() (totalCost float64, opCount int, err error) {
	return s.getActualSpend("-24 hours", "daily")
}

// GetActualWeeklySpend queries ai_model_usage table for last 7 days of spend
// Uses sliding 7-day window to prevent week-boundary gaming
func (s *Store) GetActualWeeklySpend() (totalCost float64, opCount int, err error) {
	return s.getActualSpend("-7 days", "weekly")
}

// GetActualMonthlySpend queries ai_model_usage table for last 30 days of spend
// Uses sliding 30-day window to prevent month-boundary gaming
func (s *Store) GetActualMonthlySpend() (totalCost float64, opCount int, err error) {
	return s.getActualSpend("-30 days", "monthly")
}
