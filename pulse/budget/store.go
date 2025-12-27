// Package budget provides budget tracking and spend recording for Pulse operations.
package budget

import (
	"database/sql"
	"fmt"
	"time"
)

// Record represents a budget tracking record in the database
type Record struct {
	Date            string  // "2025-11-23" for daily, "2025-11" for monthly
	Type            string  // "daily" or "monthly"
	SpendUSD        float64 // Current spend in USD
	OperationsCount int     // Number of operations performed
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Store handles persistence of budget data
type Store struct {
	db *sql.DB
}

// NewStore creates a new budget store
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// GetBudget retrieves the budget record for a given date and type
func (s *Store) GetBudget(date, budgetType string) (*Record, error) {
	query := `
		SELECT date, type, spend_usd, operations_count, created_at, updated_at
		FROM pulse_budget
		WHERE date = ? AND type = ?
	`

	var record Record
	err := s.db.QueryRow(query, date, budgetType).Scan(
		&record.Date,
		&record.Type,
		&record.SpendUSD,
		&record.OperationsCount,
		&record.CreatedAt,
		&record.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		// No record exists yet, return empty record
		return &Record{
			Date:            date,
			Type:            budgetType,
			SpendUSD:        0,
			OperationsCount: 0,
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get budget record: %w", err)
	}

	return &record, nil
}

// UpdateBudget updates or creates a budget record
func (s *Store) UpdateBudget(record *Record) error {
	query := `
		INSERT INTO pulse_budget (date, type, spend_usd, operations_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			spend_usd = excluded.spend_usd,
			operations_count = excluded.operations_count,
			updated_at = excluded.updated_at
	`

	record.UpdatedAt = time.Now()

	_, err := s.db.Exec(query,
		record.Date,
		record.Type,
		record.SpendUSD,
		record.OperationsCount,
		record.CreatedAt,
		record.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update budget record: %w", err)
	}

	return nil
}

// RecordSpend adds spending to a budget record atomically
// Uses SQL-level atomic increment to avoid read-modify-write race conditions
func (s *Store) RecordSpend(date, budgetType string, costUSD float64) error {
	query := `
		INSERT INTO pulse_budget (date, type, spend_usd, operations_count, created_at, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			spend_usd = spend_usd + ?,
			operations_count = operations_count + 1,
			updated_at = ?
	`

	now := time.Now()
	_, err := s.db.Exec(query,
		date,
		budgetType,
		costUSD,
		now,
		now,
		costUSD, // Increment value for conflict case
		now,
	)

	if err != nil {
		return fmt.Errorf("failed to record spend: %w", err)
	}

	return nil
}

// GetDailyBudget is a convenience method for getting today's daily budget
func (s *Store) GetDailyBudget() (*Record, error) {
	today := time.Now().Format("2006-01-02")
	return s.GetBudget(today, "daily")
}

// GetMonthlyBudget is a convenience method for getting current month's budget
func (s *Store) GetMonthlyBudget() (*Record, error) {
	currentMonth := time.Now().Format("2006-01")
	return s.GetBudget(currentMonth, "monthly")
}

// RecordDailySpend records spending to today's daily budget
func (s *Store) RecordDailySpend(costUSD float64) error {
	today := time.Now().Format("2006-01-02")
	return s.RecordSpend(today, "daily", costUSD)
}

// RecordMonthlySpend records spending to current month's budget
func (s *Store) RecordMonthlySpend(costUSD float64) error {
	currentMonth := time.Now().Format("2006-01")
	return s.RecordSpend(currentMonth, "monthly", costUSD)
}

// GetActualDailySpend queries ai_model_usage table for today's actual spend
// TODO(#198): Use sliding 24-hour window instead of calendar day to prevent midnight gaming
// Current: Resets at midnight (allows spending full budget at 11:59 PM, then again at 12:01 AM)
// Desired: WHERE request_timestamp >= datetime('now', '-24 hours')
func (s *Store) GetActualDailySpend() (totalCost float64, opCount int, err error) {
	query := `
		SELECT
			COALESCE(SUM(cost), 0) as total_cost,
			COUNT(*) as operation_count
		FROM ai_model_usage
		WHERE DATE(request_timestamp) = DATE('now')
			AND success = 1
	`

	err = s.db.QueryRow(query).Scan(&totalCost, &opCount)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query daily spend: %w", err)
	}

	return totalCost, opCount, nil
}

// GetActualMonthlySpend queries ai_model_usage table for this month's actual spend
// TODO(#198): Use sliding 30-day window instead of calendar month to prevent month-boundary gaming
// Current: Resets on 1st of month (allows full budget on Jan 31, then again on Feb 1)
// Desired: WHERE request_timestamp >= datetime('now', '-30 days')
func (s *Store) GetActualMonthlySpend() (totalCost float64, opCount int, err error) {
	query := `
		SELECT
			COALESCE(SUM(cost), 0) as total_cost,
			COUNT(*) as operation_count
		FROM ai_model_usage
		WHERE strftime('%Y-%m', request_timestamp) = strftime('%Y-%m', 'now')
			AND success = 1
	`

	err = s.db.QueryRow(query).Scan(&totalCost, &opCount)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query monthly spend: %w", err)
	}

	return totalCost, opCount, nil
}
