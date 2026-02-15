package budget

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/teranos/QNTX/errors"
)

const defaultPeerSpendStaleness = 10 * time.Minute

// BudgetConfig contains budget limits for daily/weekly/monthly spend tracking.
//
// Uses pure sliding windows (24h/7d/30d) on ai_model_usage table to prevent boundary gaming.
// See docs/architecture/pulse-resource-coordination.md for multi-process coordination design.
type BudgetConfig struct {
	DailyBudgetUSD   float64
	WeeklyBudgetUSD  float64
	MonthlyBudgetUSD float64
	CostPerScoreUSD  float64

	// Cluster-level limits: enforced against aggregate spend across all nodes.
	// Effective limit = average of all nodes' configured cluster limits.
	// 0 = no cluster-level enforcement.
	ClusterDailyBudgetUSD   float64
	ClusterWeeklyBudgetUSD  float64
	ClusterMonthlyBudgetUSD float64
}

// Status represents current budget state
type Status struct {
	DailySpend       float64 `json:"daily_spend,omitempty"`
	WeeklySpend      float64 `json:"weekly_spend,omitempty"`
	MonthlySpend     float64 `json:"monthly_spend,omitempty"`
	DailyRemaining   float64 `json:"daily_remaining,omitempty"`
	WeeklyRemaining  float64 `json:"weekly_remaining,omitempty"`
	MonthlyRemaining float64 `json:"monthly_remaining,omitempty"`
	DailyOps         int     `json:"daily_ops,omitempty"`
	WeeklyOps        int     `json:"weekly_ops,omitempty"`
	MonthlyOps       int     `json:"monthly_ops,omitempty"`
}

// PeerSpend holds the last-known spend and cluster limit data from a remote peer.
type PeerSpend struct {
	DailyUSD   float64
	WeeklyUSD  float64
	MonthlyUSD float64

	ClusterDailyLimitUSD   float64
	ClusterWeeklyLimitUSD  float64
	ClusterMonthlyLimitUSD float64

	ReceivedAt time.Time
}

// Tracker tracks and enforces budget limits
type Tracker struct {
	store  *Store
	config BudgetConfig
	mu     sync.RWMutex // Protects config from concurrent read/write

	peerSpends     map[string]PeerSpend // peer name → last known spend
	peerMu         sync.RWMutex         // Protects peerSpends
	stalenessLimit time.Duration        // Ignore peer spends older than this
}

// NewTracker creates a new budget tracker
func NewTracker(db *sql.DB, config BudgetConfig) *Tracker {
	return &Tracker{
		store:          NewStore(db),
		config:         config,
		peerSpends:     make(map[string]PeerSpend),
		stalenessLimit: defaultPeerSpendStaleness,
	}
}

// GetStatus returns current budget status based on actual usage from ai_model_usage table
func (bt *Tracker) GetStatus() (*Status, error) {
	// Query actual daily spend from ai_model_usage
	dailySpend, dailyOps, err := bt.store.GetActualDailySpend()
	if err != nil {
		err = errors.Wrap(err, "failed to get daily spend from usage")
		return nil, err
	}

	// Query actual weekly spend from ai_model_usage
	weeklySpend, weeklyOps, err := bt.store.GetActualWeeklySpend()
	if err != nil {
		err = errors.Wrap(err, "failed to get weekly spend from usage")
		return nil, err
	}

	// Query actual monthly spend from ai_model_usage
	monthlySpend, monthlyOps, err := bt.store.GetActualMonthlySpend()
	if err != nil {
		err = errors.Wrap(err, "failed to get monthly spend from usage")
		return nil, err
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

// CheckBudget checks if we have budget available for an operation.
// Aggregates local spend with non-stale peer spends for distributed enforcement.
// Returns error if budget would be exceeded.
func (bt *Tracker) CheckBudget(estimatedCostUSD float64) error {
	status, err := bt.GetStatus()
	if err != nil {
		err = errors.Wrap(err, "failed to get budget status")
		err = errors.WithDetail(err, fmt.Sprintf("Estimated cost: $%.4f", estimatedCostUSD))
		return err
	}

	// Aggregate local + peer spends for org-wide budget enforcement
	dailySpend, weeklySpend, monthlySpend, _ := bt.AggregateSpend(
		status.DailySpend, status.WeeklySpend, status.MonthlySpend,
	)

	bt.mu.RLock()
	dailyBudget := bt.config.DailyBudgetUSD
	weeklyBudget := bt.config.WeeklyBudgetUSD
	monthlyBudget := bt.config.MonthlyBudgetUSD
	bt.mu.RUnlock()

	if dailySpend+estimatedCostUSD > dailyBudget {
		err := errors.Newf("daily budget would be exceeded: current $%.3f + estimated $%.3f > limit $%.2f",
			dailySpend, estimatedCostUSD, dailyBudget)
		err = errors.WithDetail(err, fmt.Sprintf("Daily spend: $%.4f (local $%.4f + peers $%.4f)", dailySpend, status.DailySpend, dailySpend-status.DailySpend))
		err = errors.WithDetail(err, fmt.Sprintf("Daily limit: $%.2f", dailyBudget))
		err = errors.WithDetail(err, fmt.Sprintf("Daily remaining: $%.4f", dailyBudget-dailySpend))
		return errors.WithHint(err, "increase daily budget in config or wait for the 24-hour window to reset")
	}

	if weeklySpend+estimatedCostUSD > weeklyBudget {
		err := errors.Newf("weekly budget would be exceeded: current $%.3f + estimated $%.3f > limit $%.2f",
			weeklySpend, estimatedCostUSD, weeklyBudget)
		err = errors.WithDetail(err, fmt.Sprintf("Weekly spend: $%.4f (local $%.4f + peers $%.4f)", weeklySpend, status.WeeklySpend, weeklySpend-status.WeeklySpend))
		err = errors.WithDetail(err, fmt.Sprintf("Weekly limit: $%.2f", weeklyBudget))
		err = errors.WithDetail(err, fmt.Sprintf("Weekly remaining: $%.4f", weeklyBudget-weeklySpend))
		return errors.WithHint(err, "increase weekly budget in config or wait for the 7-day rolling window to reset")
	}

	if monthlySpend+estimatedCostUSD > monthlyBudget {
		err := errors.Newf("monthly budget would be exceeded: current $%.3f + estimated $%.3f > limit $%.2f",
			monthlySpend, estimatedCostUSD, monthlyBudget)
		err = errors.WithDetail(err, fmt.Sprintf("Monthly spend: $%.4f (local $%.4f + peers $%.4f)", monthlySpend, status.MonthlySpend, monthlySpend-status.MonthlySpend))
		err = errors.WithDetail(err, fmt.Sprintf("Monthly limit: $%.2f", monthlyBudget))
		err = errors.WithDetail(err, fmt.Sprintf("Monthly remaining: $%.4f", monthlyBudget-monthlySpend))
		return errors.WithHint(err, "increase monthly budget in config or wait for the 30-day rolling window to reset")
	}

	// Cluster-level enforcement: average of all nodes' cluster limits vs aggregate spend.
	// Only enforced when this node has a non-zero cluster budget configured.
	if clusterDaily, clusterWeekly, clusterMonthly, nodes := bt.ClusterLimits(); nodes > 0 {
		if clusterDaily > 0 && dailySpend+estimatedCostUSD > clusterDaily {
			err := errors.Newf("cluster daily budget would be exceeded: aggregate $%.3f + estimated $%.3f > cluster limit $%.2f (%d nodes averaged)",
				dailySpend, estimatedCostUSD, clusterDaily, nodes)
			return errors.WithHint(err, "reduce spend across cluster nodes or increase cluster_daily_budget_usd")
		}
		if clusterWeekly > 0 && weeklySpend+estimatedCostUSD > clusterWeekly {
			err := errors.Newf("cluster weekly budget would be exceeded: aggregate $%.3f + estimated $%.3f > cluster limit $%.2f (%d nodes averaged)",
				weeklySpend, estimatedCostUSD, clusterWeekly, nodes)
			return errors.WithHint(err, "reduce spend across cluster nodes or increase cluster_weekly_budget_usd")
		}
		if clusterMonthly > 0 && monthlySpend+estimatedCostUSD > clusterMonthly {
			err := errors.Newf("cluster monthly budget would be exceeded: aggregate $%.3f + estimated $%.3f > cluster limit $%.2f (%d nodes averaged)",
				monthlySpend, estimatedCostUSD, clusterMonthly, nodes)
			return errors.WithHint(err, "reduce spend across cluster nodes or increase cluster_monthly_budget_usd")
		}
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

	return nil
}

// GetBudgetLimits returns the current budget configuration limits
func (bt *Tracker) GetBudgetLimits() BudgetConfig {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.config
}

// GetSpendSummary returns current local spend across all three windows.
// Implements sync.BudgetProvider so the sync protocol can include spend data.
func (bt *Tracker) GetSpendSummary() (daily, weekly, monthly float64, err error) {
	status, err := bt.GetStatus()
	if err != nil {
		return 0, 0, 0, err
	}
	return status.DailySpend, status.WeeklySpend, status.MonthlySpend, nil
}

// GetClusterLimits returns this node's configured cluster budget limits.
// Implements sync.BudgetProvider so limits are exchanged during sync.
func (bt *Tracker) GetClusterLimits() (daily, weekly, monthly float64) {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.config.ClusterDailyBudgetUSD, bt.config.ClusterWeeklyBudgetUSD, bt.config.ClusterMonthlyBudgetUSD
}

// SetPeerSpend records the last-known spend and cluster limits from a remote peer.
// Called after a successful outbound sync reconciliation.
func (bt *Tracker) SetPeerSpend(peerName string, daily, weekly, monthly, clusterDaily, clusterWeekly, clusterMonthly float64) {
	bt.peerMu.Lock()
	bt.peerSpends[peerName] = PeerSpend{
		DailyUSD:               daily,
		WeeklyUSD:              weekly,
		MonthlyUSD:             monthly,
		ClusterDailyLimitUSD:   clusterDaily,
		ClusterWeeklyLimitUSD:  clusterWeekly,
		ClusterMonthlyLimitUSD: clusterMonthly,
		ReceivedAt:             time.Now(),
	}
	bt.peerMu.Unlock()
}

// ClusterLimits computes the effective cluster budget by averaging this node's
// configured cluster limits with all non-stale peers' cluster limits.
// Returns (0,0,0,0) if this node has no cluster budget configured.
func (bt *Tracker) ClusterLimits() (daily, weekly, monthly float64, nodes int) {
	bt.mu.RLock()
	localDaily := bt.config.ClusterDailyBudgetUSD
	localWeekly := bt.config.ClusterWeeklyBudgetUSD
	localMonthly := bt.config.ClusterMonthlyBudgetUSD
	bt.mu.RUnlock()

	// No cluster enforcement if this node hasn't configured it
	if localDaily == 0 && localWeekly == 0 && localMonthly == 0 {
		return 0, 0, 0, 0
	}

	// Start with local node
	daily, weekly, monthly = localDaily, localWeekly, localMonthly
	nodes = 1

	bt.peerMu.RLock()
	defer bt.peerMu.RUnlock()

	now := time.Now()
	for _, ps := range bt.peerSpends {
		if bt.stalenessLimit > 0 && now.Sub(ps.ReceivedAt) > bt.stalenessLimit {
			continue
		}
		// Only count peers that also have cluster limits configured
		if ps.ClusterDailyLimitUSD == 0 && ps.ClusterWeeklyLimitUSD == 0 && ps.ClusterMonthlyLimitUSD == 0 {
			continue
		}
		daily += ps.ClusterDailyLimitUSD
		weekly += ps.ClusterWeeklyLimitUSD
		monthly += ps.ClusterMonthlyLimitUSD
		nodes++
	}

	// Average across all participating nodes
	daily /= float64(nodes)
	weekly /= float64(nodes)
	monthly /= float64(nodes)
	return
}

// AggregateSpend sums local spend with non-stale peer spends.
// Returns the aggregate totals and the number of non-stale peers included.
func (bt *Tracker) AggregateSpend(localDaily, localWeekly, localMonthly float64) (daily, weekly, monthly float64, peers int) {
	daily, weekly, monthly = localDaily, localWeekly, localMonthly

	bt.peerMu.RLock()
	defer bt.peerMu.RUnlock()

	now := time.Now()
	for _, ps := range bt.peerSpends {
		if bt.stalenessLimit > 0 && now.Sub(ps.ReceivedAt) > bt.stalenessLimit {
			continue
		}
		daily += ps.DailyUSD
		weekly += ps.WeeklyUSD
		monthly += ps.MonthlyUSD
		peers++
	}
	return
}
