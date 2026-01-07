package budget

import (
	"database/sql"
	"sync"

	"github.com/teranos/QNTX/errors"
)

// BudgetConfig contains budget limits for daily/weekly/monthly spend tracking.
//
// Uses pure sliding windows (24h/7d/30d) on ai_model_usage table to prevent boundary gaming.
// See docs/architecture/pulse-resource-coordination.md for multi-process coordination design.
type BudgetConfig struct {
	DailyBudgetUSD   float64
	WeeklyBudgetUSD  float64
	MonthlyBudgetUSD float64
	CostPerScoreUSD  float64
}

// Status represents current budget state
type Status struct {
	DailySpend       float64 `json:"daily_spend"`
	WeeklySpend      float64 `json:"weekly_spend"`
	MonthlySpend     float64 `json:"monthly_spend"`
	DailyRemaining   float64 `json:"daily_remaining"`
	WeeklyRemaining  float64 `json:"weekly_remaining"`
	MonthlyRemaining float64 `json:"monthly_remaining"`
	DailyOps         int     `json:"daily_ops"`
	WeeklyOps        int     `json:"weekly_ops"`
	MonthlyOps       int     `json:"monthly_ops"`
}

// Tracker tracks and enforces budget limits
type Tracker struct {
	store  *Store
	config BudgetConfig
	mu     sync.RWMutex // Protects config from concurrent read/write
}

// NewTracker creates a new budget tracker
func NewTracker(db *sql.DB, config BudgetConfig) *Tracker {
	return &Tracker{
		store:  NewStore(db),
		config: config,
	}
}

// GetStatus returns current budget status based on actual usage from ai_model_usage table
func (bt *Tracker) GetStatus() (*Status, error) {
	// Query actual daily spend from ai_model_usage
	dailySpend, dailyOps, err := bt.store.GetActualDailySpend()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get daily spend from usage")
	}

	// Query actual weekly spend from ai_model_usage
	weeklySpend, weeklyOps, err := bt.store.GetActualWeeklySpend()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get weekly spend from usage")
	}

	// Query actual monthly spend from ai_model_usage
	monthlySpend, monthlyOps, err := bt.store.GetActualMonthlySpend()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get monthly spend from usage")
	}

	bt.mu.RLock()
	dailyBudget := bt.config.DailyBudgetUSD
	weeklyBudget := bt.config.WeeklyBudgetUSD
	monthlyBudget := bt.config.MonthlyBudgetUSD
	bt.mu.RUnlock()

	return &Status{
		DailySpend:       dailySpend,
		WeeklySpend:      weeklySpend,
		MonthlySpend:     monthlySpend,
		DailyRemaining:   dailyBudget - dailySpend,
		WeeklyRemaining:  weeklyBudget - weeklySpend,
		MonthlyRemaining: monthlyBudget - monthlySpend,
		DailyOps:         dailyOps,
		WeeklyOps:        weeklyOps,
		MonthlyOps:       monthlyOps,
	}, nil
}

// CheckBudget checks if we have budget available for an operation
// Returns error if budget would be exceeded
func (bt *Tracker) CheckBudget(estimatedCostUSD float64) error {
	status, err := bt.GetStatus()
	if err != nil {
		return errors.Wrap(err, "failed to get budget status")
	}

	bt.mu.RLock()
	dailyBudget := bt.config.DailyBudgetUSD
	weeklyBudget := bt.config.WeeklyBudgetUSD
	monthlyBudget := bt.config.MonthlyBudgetUSD
	bt.mu.RUnlock()

	if status.DailySpend+estimatedCostUSD > dailyBudget {
		err := errors.Newf("daily budget would be exceeded: current $%.3f + estimated $%.3f > limit $%.2f",
			status.DailySpend, estimatedCostUSD, dailyBudget)
		return errors.WithHint(err, "increase daily budget in config or wait for the 24-hour window to reset")
	}

	if weeklyBudget > 0 && status.WeeklySpend+estimatedCostUSD > weeklyBudget {
		err := errors.Newf("weekly budget would be exceeded: current $%.3f + estimated $%.3f > limit $%.2f",
			status.WeeklySpend, estimatedCostUSD, weeklyBudget)
		return errors.WithHint(err, "increase weekly budget in config or wait for the 7-day rolling window to reset")
	}

	if status.MonthlySpend+estimatedCostUSD > monthlyBudget {
		err := errors.Newf("monthly budget would be exceeded: current $%.3f + estimated $%.3f > limit $%.2f",
			status.MonthlySpend, estimatedCostUSD, monthlyBudget)
		return errors.WithHint(err, "increase monthly budget in config or wait for the 30-day rolling window to reset")
	}

	return nil
}

// EstimateOperationCost estimates the cost of performing N operations
func (bt *Tracker) EstimateOperationCost(numOperations int) float64 {
	bt.mu.RLock()
	costPerOperation := bt.config.CostPerScoreUSD
	bt.mu.RUnlock()
	return float64(numOperations) * costPerOperation
}

// UpdateDailyBudget updates the daily budget limit at runtime and persists to config.toml
func (bt *Tracker) UpdateDailyBudget(newBudgetUSD float64) error {
	if newBudgetUSD < 0 {
		err := errors.Newf("daily budget cannot be negative: %.2f", newBudgetUSD)
		return errors.WithHint(err, "specify a non-negative budget value (e.g., 5.00 for $5/day)")
	}

	// Update in-memory config
	bt.mu.Lock()
	bt.config.DailyBudgetUSD = newBudgetUSD
	bt.mu.Unlock()

	// TODO: Make config persistence optional via callback interface
	// See handoff.md Decision 4: Config Persistence
	// Persist to config.toml
	// if err := am.UpdatePulseDailyBudget(newBudgetUSD); err != nil {
	// 	return fmt.Errorf("failed to persist budget to config: %w", err)
	// }

	return nil
}

// UpdateWeeklyBudget updates the weekly budget limit at runtime and persists to config.toml
func (bt *Tracker) UpdateWeeklyBudget(newBudgetUSD float64) error {
	if newBudgetUSD < 0 {
		err := errors.Newf("weekly budget cannot be negative: %.2f", newBudgetUSD)
		return errors.WithHint(err, "specify a non-negative budget value (e.g., 35.00 for $35/week)")
	}

	// Update in-memory config
	bt.mu.Lock()
	bt.config.WeeklyBudgetUSD = newBudgetUSD
	bt.mu.Unlock()

	// TODO: Make config persistence optional via callback interface
	// See handoff.md Decision 4: Config Persistence
	// Persist to config.toml
	// if err := am.UpdatePulseWeeklyBudget(newBudgetUSD); err != nil {
	// 	return fmt.Errorf("failed to persist budget to config: %w", err)
	// }

	return nil
}

// UpdateMonthlyBudget updates the monthly budget limit at runtime and persists to config.toml
func (bt *Tracker) UpdateMonthlyBudget(newBudgetUSD float64) error {
	if newBudgetUSD < 0 {
		err := errors.Newf("monthly budget cannot be negative: %.2f", newBudgetUSD)
		return errors.WithHint(err, "specify a non-negative budget value (e.g., 100.00 for $100/month)")
	}

	// Update in-memory config
	bt.mu.Lock()
	bt.config.MonthlyBudgetUSD = newBudgetUSD
	bt.mu.Unlock()

	// TODO: Make config persistence optional via callback interface
	// See handoff.md Decision 4: Config Persistence
	// Persist to config.toml
	// if err := am.UpdatePulseMonthlyBudget(newBudgetUSD); err != nil {
	// 	return fmt.Errorf("failed to persist budget to config: %w", err)
	// }

	return nil
}

// GetBudgetLimits returns the current budget configuration limits
func (bt *Tracker) GetBudgetLimits() BudgetConfig {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.config
}
