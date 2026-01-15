package storage

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/ats/types"
)

// ==============================================================================
// Test Domain: Neural Activity Tracking
// ==============================================================================
// Models neural activity periods with temporal aggregation for neuroscience research.
// Subjects: Neurons (e.g., CA1_PYRAMIDAL_N042)
// Predicates: activity_duration_s (duration of neural activity in seconds)
// Metadata: start_time, end_time, duration_s, activity_type, brain_region
//
// Use case: "Find neurons with >1.0s total activity in CA1 region in last 2 hours"
// Query: ax activity_duration_s over 1.0s in "CA1 pyramidal" since last 2h
// ==============================================================================

// mockNeuralQueryExpander provides neural domain-specific predicates for testing
type mockNeuralQueryExpander struct{}

func (m *mockNeuralQueryExpander) ExpandPredicate(predicate string, values []string) []ats.PredicateExpansion {
	// Simple passthrough for testing
	var expansions []ats.PredicateExpansion
	for _, value := range values {
		expansions = append(expansions, ats.PredicateExpansion{
			Predicate: predicate,
			Context:   value,
		})
	}
	return expansions
}

func (m *mockNeuralQueryExpander) GetNumericPredicates() []string {
	return []string{"activity_duration_s"}
}

func (m *mockNeuralQueryExpander) GetNaturalLanguagePredicates() []string {
	return []string{}
}

// setupNeuralActivityTestDB creates test database with neural activity attestations
func setupNeuralActivityTestDB(t *testing.T) *sql.DB {
	testDB := qntxtest.CreateTestDB(t)
	store := NewSQLStore(testDB, nil)

	now := time.Now()
	attestationTime := now.Add(-30 * time.Minute) // Recorded 30 minutes ago

	// Neuron 1: CA1_PYRAMIDAL_N042 - 3 activity periods totaling 1.5s (ABOVE threshold)
	// Period 1: 0.5s burst at T-2h
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N042",
		activityID:   "n042_activity1",
		startTime:    now.Add(-2 * time.Hour),
		endTime:      now.Add(-2*time.Hour + 500*time.Millisecond),
		durationS:    0.5,
		activityType: "spike_burst",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	// Period 2: 0.6s sustained activity at T-1h
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N042",
		activityID:   "n042_activity2",
		startTime:    now.Add(-1 * time.Hour),
		endTime:      now.Add(-1*time.Hour + 600*time.Millisecond),
		durationS:    0.6,
		activityType: "theta_oscillation",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	// Period 3: 0.4s recent activity at T-10min
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N042",
		activityID:   "n042_activity3",
		startTime:    now.Add(-10 * time.Minute),
		endTime:      now.Add(-10*time.Minute + 400*time.Millisecond),
		durationS:    0.4,
		activityType: "gamma_synchrony",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})
	// Total for N042: 0.5 + 0.6 + 0.4 = 1.5s (ABOVE 1.0s threshold)

	// Neuron 2: CA1_PYRAMIDAL_N043 - 2 activity periods totaling 0.8s (BELOW threshold)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N043",
		activityID:   "n043_activity1",
		startTime:    now.Add(-90 * time.Minute),
		endTime:      now.Add(-90*time.Minute + 500*time.Millisecond),
		durationS:    0.5,
		activityType: "spike_burst",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N043",
		activityID:   "n043_activity2",
		startTime:    now.Add(-45 * time.Minute),
		endTime:      now.Add(-45*time.Minute + 300*time.Millisecond),
		durationS:    0.3,
		activityType: "theta_oscillation",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})
	// Total for N043: 0.5 + 0.3 = 0.8s (BELOW 1.0s threshold)

	// Neuron 3: CA3_PYRAMIDAL_N001 - 4 activity periods totaling 2.1s (ABOVE threshold, different region)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA3_PYRAMIDAL_N001",
		activityID:   "n001_activity1",
		startTime:    now.Add(-2 * time.Hour),
		endTime:      now.Add(-2*time.Hour + 600*time.Millisecond),
		durationS:    0.6,
		activityType: "spike_burst",
		region:       "hippocampus_ca3",
		attestedAt:   attestationTime,
	})

	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA3_PYRAMIDAL_N001",
		activityID:   "n001_activity2",
		startTime:    now.Add(-90 * time.Minute),
		endTime:      now.Add(-90*time.Minute + 700*time.Millisecond),
		durationS:    0.7,
		activityType: "theta_oscillation",
		region:       "hippocampus_ca3",
		attestedAt:   attestationTime,
	})

	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA3_PYRAMIDAL_N001",
		activityID:   "n001_activity3",
		startTime:    now.Add(-30 * time.Minute),
		endTime:      now.Add(-30*time.Minute + 500*time.Millisecond),
		durationS:    0.5,
		activityType: "gamma_synchrony",
		region:       "hippocampus_ca3",
		attestedAt:   attestationTime,
	})

	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA3_PYRAMIDAL_N001",
		activityID:   "n001_activity4",
		startTime:    now.Add(-5 * time.Minute),
		endTime:      now.Add(-5*time.Minute + 300*time.Millisecond),
		durationS:    0.3,
		activityType: "spike_burst",
		region:       "hippocampus_ca3",
		attestedAt:   attestationTime,
	})
	// Total for N001: 0.6 + 0.7 + 0.5 + 0.3 = 2.1s (ABOVE 1.0s threshold)

	return testDB
}

type neuralActivity struct {
	neuronID     string
	activityID   string
	startTime    time.Time
	endTime      time.Time
	durationS    float64
	activityType string
	region       string
	attestedAt   time.Time
}

func createActivityAttestation(t *testing.T, store *SQLStore, activity *neuralActivity) {
	metadata := map[string]interface{}{
		"start_time":    activity.startTime.Format(time.RFC3339),
		"end_time":      activity.endTime.Format(time.RFC3339),
		"duration_s":    fmt.Sprintf("%.1f", activity.durationS),
		"activity_type": activity.activityType,
		"brain_region":  activity.region,
	}

	err := store.CreateAttestation(&types.As{
		ID:         activity.activityID,
		Subjects:   []string{activity.neuronID},
		Predicates: []string{"activity_duration_s"},
		Contexts:   []string{fmt.Sprintf("%.1f", activity.durationS)},
		Actors:     []string{"openbci:recording"},
		Timestamp:  activity.attestedAt,
		Source:     "neural_recording",
		Attributes: metadata,
		CreatedAt:  activity.attestedAt,
	})
	require.NoError(t, err)
}

func formatDuration(seconds float64) string {
	if seconds == float64(int(seconds)) {
		return string(rune(int(seconds)))
	}
	return string(rune(int(seconds * 10)))
}

// ==============================================================================
// PHASE 1: Basic Aggregation (MVP - Jan 15 Demo)
// ==============================================================================

// TestTemporalAggregation_SimpleSum tests basic duration aggregation across multiple activity periods
// Query: ax activity_duration_s over 1.0s
// Expected: CA1_PYRAMIDAL_N042 (1.5s) and CA3_PYRAMIDAL_N001 (2.1s) match
//           CA1_PYRAMIDAL_N043 (0.8s) does NOT match
func TestTemporalAggregation_SimpleSum(t *testing.T) {
	t.Skip("Phase 1: Basic aggregation - implement buildOverComparisonFilter with GROUP BY SUM")

	db := setupNeuralActivityTestDB(t)
	queryStore := NewSQLQueryStore(db, &mockNeuralQueryExpander{})

	// Query: ax activity_duration_s over 1.0s
	filter := types.AxFilter{
		OverComparison: &types.OverFilter{
			Value:    1.0,
			Unit:     "s",
			Operator: "over",
		},
	}

	results, err := queryStore.ExecuteAxQuery(context.Background(), filter)
	require.NoError(t, err)

	// Should return activity attestations for neurons with total >= 1.0s
	// N042: 3 attestations (0.5s + 0.6s + 0.4s = 1.5s) ✓
	// N043: 0 attestations (0.5s + 0.3s = 0.8s) ✗
	// N001: 4 attestations (0.6s + 0.7s + 0.5s + 0.3s = 2.1s) ✓
	assert.Equal(t, 7, len(results), "Expected 7 activity attestations (3 for N042 + 4 for N001)")

	// Group results by neuron
	neuronActivityCount := make(map[string]int)
	for _, result := range results {
		neuronActivityCount[result.Subjects[0]]++
	}

	assert.Equal(t, 3, neuronActivityCount["CA1_PYRAMIDAL_N042"], "N042 should have 3 activity periods")
	assert.Equal(t, 4, neuronActivityCount["CA3_PYRAMIDAL_N001"], "N001 should have 4 activity periods")
	assert.Equal(t, 0, neuronActivityCount["CA1_PYRAMIDAL_N043"], "N043 should NOT match (below threshold)")
}

// ==============================================================================
// PHASE 2: Temporal Filtering with `since` (MVP - Jan 15 Demo)
// ==============================================================================

// setupTemporalFilteringTestDB creates test database with activities both inside and outside time window
func setupTemporalFilteringTestDB(t *testing.T) *sql.DB {
	testDB := qntxtest.CreateTestDB(t)
	store := NewSQLStore(testDB, nil)

	now := time.Now()
	attestationTime := now.Add(-30 * time.Minute)

	// Neuron N042: Has old activity (2h ago, filtered out) and recent activity
	// Old activity at T-2h: 0.5s (OUTSIDE 1h window, should be filtered)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N042",
		activityID:   "n042_old_activity",
		startTime:    now.Add(-2 * time.Hour),
		endTime:      now.Add(-2*time.Hour + 500*time.Millisecond),
		durationS:    0.5,
		activityType: "spike_burst",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	// Recent activity at T-30min: 0.6s (INSIDE 1h window, should be counted)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N042",
		activityID:   "n042_recent_activity",
		startTime:    now.Add(-30 * time.Minute),
		endTime:      now.Add(-30*time.Minute + 600*time.Millisecond),
		durationS:    0.6,
		activityType: "theta_oscillation",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})
	// N042 total in last 1h: 0.6s (BELOW 1.0s threshold after filtering)

	// Neuron N043: Similar pattern
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N043",
		activityID:   "n043_old_activity",
		startTime:    now.Add(-90 * time.Minute),
		endTime:      now.Add(-90*time.Minute + 300*time.Millisecond),
		durationS:    0.3,
		activityType: "spike_burst",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N043",
		activityID:   "n043_recent_activity",
		startTime:    now.Add(-20 * time.Minute),
		endTime:      now.Add(-20*time.Minute + 600*time.Millisecond),
		durationS:    0.6,
		activityType: "gamma_synchrony",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})
	// N043 total in last 1h: 0.6s (BELOW 1.0s threshold)

	// Neuron N001: Has enough recent activity to exceed threshold
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA3_PYRAMIDAL_N001",
		activityID:   "n001_recent1",
		startTime:    now.Add(-45 * time.Minute),
		endTime:      now.Add(-45*time.Minute + 800*time.Millisecond),
		durationS:    0.8,
		activityType: "theta_oscillation",
		region:       "hippocampus_ca3",
		attestedAt:   attestationTime,
	})

	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA3_PYRAMIDAL_N001",
		activityID:   "n001_recent2",
		startTime:    now.Add(-10 * time.Minute),
		endTime:      now.Add(-10*time.Minute + 500*time.Millisecond),
		durationS:    0.5,
		activityType: "gamma_synchrony",
		region:       "hippocampus_ca3",
		attestedAt:   attestationTime,
	})
	// N001 total in last 1h: 0.8s + 0.5s = 1.3s (ABOVE 1.0s threshold)

	return testDB
}

// TestTemporalAggregation_WithSinceFilter tests temporal window filtering before aggregation
// Query: ax activity_duration_s over 1.0s since last 1h
// Expected: Only N001 matches (1.3s in last hour)
//           N042 and N043 excluded (their recent activity < 1.0s, old activity filtered out)
func TestTemporalAggregation_WithSinceFilter(t *testing.T) {
	t.Skip("Phase 2: Temporal filtering - implement buildMetadataTemporalFilters")

	db := setupTemporalFilteringTestDB(t)
	queryStore := NewSQLQueryStore(db, &mockNeuralQueryExpander{})

	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	// Query: ax activity_duration_s over 1.0s since last 1h
	filter := types.AxFilter{
		OverComparison: &types.OverFilter{
			Value:    1.0,
			Unit:     "s",
			Operator: "over",
		},
		TimeStart: &oneHourAgo, // Filter by start_time >= 1 hour ago
	}

	results, err := queryStore.ExecuteAxQuery(context.Background(), filter)
	require.NoError(t, err)

	// Should only return N001's 2 recent attestations
	// N042: old activity filtered out, recent = 0.6s (below threshold) ✗
	// N043: old activity filtered out, recent = 0.6s (below threshold) ✗
	// N001: 2 recent activities = 1.3s (above threshold) ✓
	assert.Equal(t, 2, len(results), "Expected 2 activity attestations (both for N001)")

	// Verify all results are for N001
	for _, result := range results {
		assert.Equal(t, []string{"CA3_PYRAMIDAL_N001"}, result.Subjects)
		assert.Equal(t, []string{"activity_duration_s"}, result.Predicates)
	}
}

// ==============================================================================
// PHASE 2: Semantic Context Matching with Weighted Aggregation
// ==============================================================================

// setupSemanticMatchingTestDB creates test database with neurons in different regions/types
// for testing semantic distance-based weighted aggregation
func setupSemanticMatchingTestDB(t *testing.T) *sql.DB {
	testDB := qntxtest.CreateTestDB(t)
	store := NewSQLStore(testDB, nil)

	now := time.Now()
	attestationTime := now.Add(-30 * time.Minute)

	// Neuron 1: CA1_PYRAMIDAL_N042 - Exact match to "CA1 pyramidal" query
	// 1.0s activity, semantic weight: 1.0 (exact match)
	// Weighted contribution: 1.0s × 1.0 = 1.0s
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N042",
		activityID:   "ca1_pyr_activity",
		startTime:    now.Add(-45 * time.Minute),
		endTime:      now.Add(-45*time.Minute + 1000*time.Millisecond),
		durationS:    1.0,
		activityType: "theta_oscillation",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	// Neuron 2: CA1_INTERNEURON_N010 - Same region (CA1), different cell type
	// 0.8s activity, semantic weight: 0.7 (related but not exact)
	// Weighted contribution: 0.8s × 0.7 = 0.56s
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_INTERNEURON_N010",
		activityID:   "ca1_inter_activity",
		startTime:    now.Add(-40 * time.Minute),
		endTime:      now.Add(-40*time.Minute + 800*time.Millisecond),
		durationS:    0.8,
		activityType: "gamma_synchrony",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	// Neuron 3: CA3_PYRAMIDAL_N001 - Adjacent region (CA3), same cell type
	// 1.2s activity, semantic weight: 0.5 (moderately related)
	// Weighted contribution: 1.2s × 0.5 = 0.6s
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA3_PYRAMIDAL_N001",
		activityID:   "ca3_pyr_activity",
		startTime:    now.Add(-50 * time.Minute),
		endTime:      now.Add(-50*time.Minute + 1200*time.Millisecond),
		durationS:    1.2,
		activityType: "spike_burst",
		region:       "hippocampus_ca3",
		attestedAt:   attestationTime,
	})

	// Neuron 4: V1_STELLATE_N020 - Completely different region/type (visual cortex)
	// 2.0s activity, semantic weight: 0.1 (very distant, likely excluded)
	// Weighted contribution: 2.0s × 0.1 = 0.2s (or excluded entirely)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "V1_STELLATE_N020",
		activityID:   "v1_stellate_activity",
		startTime:    now.Add(-35 * time.Minute),
		endTime:      now.Add(-35*time.Minute + 2000*time.Millisecond),
		durationS:    2.0,
		activityType: "orientation_tuning",
		region:       "visual_cortex_v1",
		attestedAt:   attestationTime,
	})

	return testDB
}

// TestTemporalAggregation_SemanticWeightedSum tests weighted aggregation based on semantic distance
// Query: ax activity_duration_s over 1.5s in "CA1 pyramidal"
//
// Semantic expansion (via query expander):
//   "CA1 pyramidal" expands to:
//     - CA1_PYRAMIDAL_* (weight: 1.0, exact match)
//     - CA1_INTERNEURON_* (weight: 0.7, same region different type)
//     - CA3_PYRAMIDAL_* (weight: 0.5, adjacent region same type)
//     - V1_STELLATE_* (weight: 0.1, very distant - likely excluded)
//
// Weighted aggregation:
//   CA1_PYRAMIDAL_N042:   1.0s × 1.0 = 1.0s
//   CA1_INTERNEURON_N010: 0.8s × 0.7 = 0.56s
//   CA3_PYRAMIDAL_N001:   1.2s × 0.5 = 0.6s
//   V1_STELLATE_N020:     2.0s × 0.1 = 0.2s (or excluded)
//   Total: ~2.16s (or 1.96s if V1 excluded)
//
// Expected: Query matches because weighted sum >= 1.5s threshold
func TestTemporalAggregation_SemanticWeightedSum(t *testing.T) {
	t.Skip("Phase 2: Semantic matching - implement query expander with semantic weights + weighted SUM in storage")

	db := setupSemanticMatchingTestDB(t)
	queryStore := NewSQLQueryStore(db, &mockNeuralQueryExpander{})

	// Query: ax activity_duration_s over 1.5s in "CA1 pyramidal"
	// Note: The "in" clause would be expanded by query expander before reaching storage
	// For now, we'll simulate the expanded filter with semantic weights
	filter := types.AxFilter{
		OverComparison: &types.OverFilter{
			Value:    1.5,
			Unit:     "s",
			Operator: "over",
		},
		// TODO: Add SemanticWeights field to AxFilter
		// SemanticWeights: map[string]float64{
		//     "CA1_PYRAMIDAL_N042": 1.0,
		//     "CA1_INTERNEURON_N010": 0.7,
		//     "CA3_PYRAMIDAL_N001": 0.5,
		//     "V1_STELLATE_N020": 0.1,
		// },
	}

	results, err := queryStore.ExecuteAxQuery(context.Background(), filter)
	require.NoError(t, err)

	// Should return attestations for neurons that contribute to weighted sum >= 1.5s
	// Expected: 3-4 attestations (CA1_PYR + CA1_INTER + CA3_PYR, maybe V1)
	assert.GreaterOrEqual(t, len(results), 3, "Expected at least 3 attestations")

	// Verify weighted aggregation logic
	// Total weighted duration should be >= 1.5s
	totalWeighted := 0.0
	for _, result := range results {
		// Extract duration and apply semantic weight
		// Implementation will depend on how weights are stored/passed
		_ = result
	}
	assert.GreaterOrEqual(t, totalWeighted, 1.5, "Weighted sum should exceed threshold")
}

// ==============================================================================
// PHASE 2: Combined Temporal + Semantic Filtering
// ==============================================================================

// setupCombinedFilteringTestDB creates test database with semantic matches having both old and recent activity
func setupCombinedFilteringTestDB(t *testing.T) *sql.DB {
	testDB := qntxtest.CreateTestDB(t)
	store := NewSQLStore(testDB, nil)

	now := time.Now()
	attestationTime := now.Add(-30 * time.Minute)

	// CA1_PYRAMIDAL_N042: Has both old (filtered) and recent (counted) activity
	// Old activity: 0.6s at T-2h (OUTSIDE 1h window, filtered out)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N042",
		activityID:   "n042_old",
		startTime:    now.Add(-2 * time.Hour),
		endTime:      now.Add(-2*time.Hour + 600*time.Millisecond),
		durationS:    0.6,
		activityType: "spike_burst",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	// Recent activity: 0.5s at T-30min (INSIDE 1h window, weighted 1.0)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N042",
		activityID:   "n042_recent",
		startTime:    now.Add(-30 * time.Minute),
		endTime:      now.Add(-30*time.Minute + 500*time.Millisecond),
		durationS:    0.5,
		activityType: "theta_oscillation",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})
	// N042 weighted contribution (recent only): 0.5s × 1.0 = 0.5s

	// CA1_INTERNEURON_N010: Only recent activity
	// Recent activity: 0.4s at T-45min (INSIDE 1h window, weighted 0.7)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_INTERNEURON_N010",
		activityID:   "n010_recent",
		startTime:    now.Add(-45 * time.Minute),
		endTime:      now.Add(-45*time.Minute + 400*time.Millisecond),
		durationS:    0.4,
		activityType: "gamma_synchrony",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})
	// N010 weighted contribution: 0.4s × 0.7 = 0.28s

	// CA3_PYRAMIDAL_N001: Has both old and recent activity
	// Old activity: 0.8s at T-90min (OUTSIDE 1h window, filtered out)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA3_PYRAMIDAL_N001",
		activityID:   "n001_old",
		startTime:    now.Add(-90 * time.Minute),
		endTime:      now.Add(-90*time.Minute + 800*time.Millisecond),
		durationS:    0.8,
		activityType: "spike_burst",
		region:       "hippocampus_ca3",
		attestedAt:   attestationTime,
	})

	// Recent activity: 0.4s at T-20min (INSIDE 1h window, weighted 0.5)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA3_PYRAMIDAL_N001",
		activityID:   "n001_recent",
		startTime:    now.Add(-20 * time.Minute),
		endTime:      now.Add(-20*time.Minute + 400*time.Millisecond),
		durationS:    0.4,
		activityType: "theta_oscillation",
		region:       "hippocampus_ca3",
		attestedAt:   attestationTime,
	})
	// N001 weighted contribution (recent only): 0.4s × 0.5 = 0.2s

	// Total weighted recent activity: 0.5 + 0.28 + 0.2 = 0.98s (BELOW 1.0s threshold)

	return testDB
}

// TestTemporalAggregation_CombinedTemporalAndSemantic tests temporal filtering + semantic weighting
// Query: ax activity_duration_s over 1.0s in "CA1 pyramidal" since last 1h
//
// Processing order:
//   1. Temporal filter: Only activity with start_time >= (now - 1h)
//   2. Semantic expansion: "CA1 pyramidal" → {CA1_PYRAMIDAL: 1.0, CA1_INTERNEURON: 0.7, CA3_PYRAMIDAL: 0.5}
//   3. Weighted aggregation: SUM(duration × semantic_weight) for recent activities only
//
// Expected weighted sums (recent activity only):
//   CA1_PYRAMIDAL_N042:   0.5s × 1.0 = 0.5s (old 0.6s filtered out)
//   CA1_INTERNEURON_N010: 0.4s × 0.7 = 0.28s
//   CA3_PYRAMIDAL_N001:   0.4s × 0.5 = 0.2s (old 0.8s filtered out)
//   Total: 0.98s (BELOW 1.0s threshold)
//
// Expected: NO results (weighted sum < threshold)
func TestTemporalAggregation_CombinedTemporalAndSemantic(t *testing.T) {
	t.Skip("Phase 2: Combined filtering - temporal + semantic weighted aggregation")

	db := setupCombinedFilteringTestDB(t)
	queryStore := NewSQLQueryStore(db, &mockNeuralQueryExpander{})

	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	// Query: ax activity_duration_s over 1.0s in "CA1 pyramidal" since last 1h
	filter := types.AxFilter{
		OverComparison: &types.OverFilter{
			Value:    1.0,
			Unit:     "s",
			Operator: "over",
		},
		TimeStart: &oneHourAgo,
		// TODO: Add SemanticWeights field
		// SemanticWeights: map[string]float64{
		//     "CA1_PYRAMIDAL_N042": 1.0,
		//     "CA1_INTERNEURON_N010": 0.7,
		//     "CA3_PYRAMIDAL_N001": 0.5,
		// },
	}

	results, err := queryStore.ExecuteAxQuery(context.Background(), filter)
	require.NoError(t, err)

	// Should return NO results because weighted sum of recent activity (0.98s) < threshold (1.0s)
	assert.Equal(t, 0, len(results), "Expected no results - weighted recent activity below threshold")
}

// ==============================================================================
// PHASE 3: Overlap Detection (Post-Demo)
// ==============================================================================

// setupOverlapDetectionTestDB creates test database with overlapping activity periods
func setupOverlapDetectionTestDB(t *testing.T) *sql.DB {
	testDB := qntxtest.CreateTestDB(t)
	store := NewSQLStore(testDB, nil)

	now := time.Now()
	attestationTime := now.Add(-30 * time.Minute)

	// Neuron N042: Has overlapping activity periods that should be merged
	// Activity 1: T+0ms to T+1000ms (1.0s)
	baseTime := now.Add(-45 * time.Minute)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N042",
		activityID:   "n042_overlap1",
		startTime:    baseTime,
		endTime:      baseTime.Add(1000 * time.Millisecond),
		durationS:    1.0,
		activityType: "theta_oscillation",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	// Activity 2: T+500ms to T+1500ms (1.0s, overlaps 500ms with Activity 1)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N042",
		activityID:   "n042_overlap2",
		startTime:    baseTime.Add(500 * time.Millisecond),
		endTime:      baseTime.Add(1500 * time.Millisecond),
		durationS:    1.0,
		activityType: "theta_oscillation",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	// Activity 3: T+2000ms to T+2800ms (0.8s, no overlap with previous activities)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N042",
		activityID:   "n042_separate",
		startTime:    baseTime.Add(2000 * time.Millisecond),
		endTime:      baseTime.Add(2800 * time.Millisecond),
		durationS:    0.8,
		activityType: "gamma_synchrony",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	// Without overlap detection: 1.0 + 1.0 + 0.8 = 2.8s
	// With overlap detection:
	//   Merge Activity 1 & 2: T+0ms to T+1500ms = 1.5s
	//   Add Activity 3: 0.8s
	//   Total: 1.5 + 0.8 = 2.3s

	// Neuron N043: Non-overlapping activities for comparison
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N043",
		activityID:   "n043_activity1",
		startTime:    now.Add(-50 * time.Minute),
		endTime:      now.Add(-50*time.Minute + 600*time.Millisecond),
		durationS:    0.6,
		activityType: "spike_burst",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N043",
		activityID:   "n043_activity2",
		startTime:    now.Add(-40 * time.Minute),
		endTime:      now.Add(-40*time.Minute + 700*time.Millisecond),
		durationS:    0.7,
		activityType: "theta_oscillation",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})
	// N043 total: 0.6 + 0.7 = 1.3s (no overlaps, sum is accurate)

	return testDB
}

// TestTemporalAggregation_OverlapDetection tests merging of overlapping activity periods
// Query: ax activity_duration_s over 2.0s
//
// Overlap detection algorithm:
//   1. Sort activity periods by start_time
//   2. Merge overlapping periods (if period.start <= merged.last.end)
//   3. Sum durations of merged periods
//
// Neuron N042 (overlapping activities):
//   Period 1: T+0ms to T+1000ms (1.0s)
//   Period 2: T+500ms to T+1500ms (1.0s, overlaps 500ms)
//   Period 3: T+2000ms to T+2800ms (0.8s, separate)
//
//   Merged:
//     Periods 1 & 2 → T+0ms to T+1500ms = 1.5s
//     Period 3 → 0.8s
//   Total: 2.3s (WITH overlap detection) vs 2.8s (WITHOUT)
//
// Neuron N043 (no overlaps):
//   Period 1: 0.6s
//   Period 2: 0.7s
//   Total: 1.3s (same with or without overlap detection)
//
// Expected: Only N042 matches (2.3s >= 2.0s threshold)
//           N043 does NOT match (1.3s < 2.0s threshold)
func TestTemporalAggregation_OverlapDetection(t *testing.T) {
	t.Skip("Phase 3: Overlap detection - implement period merging algorithm")

	db := setupOverlapDetectionTestDB(t)
	queryStore := NewSQLQueryStore(db, &mockNeuralQueryExpander{})

	// Query: ax activity_duration_s over 2.0s
	filter := types.AxFilter{
		OverComparison: &types.OverFilter{
			Value:    2.0,
			Unit:     "s",
			Operator: "over",
		},
	}

	results, err := queryStore.ExecuteAxQuery(context.Background(), filter)
	require.NoError(t, err)

	// Should return only N042's 3 attestations (merged duration 2.3s >= 2.0s)
	// N043 excluded (1.3s < 2.0s)
	assert.Equal(t, 3, len(results), "Expected 3 activity attestations for N042")

	// Verify all results are for N042
	for _, result := range results {
		assert.Equal(t, []string{"CA1_PYRAMIDAL_N042"}, result.Subjects)
	}
}

// ==============================================================================
// PHASE 3: Ongoing Activity (missing end_date)
// ==============================================================================

// setupOngoingActivityTestDB creates test database with ongoing activities (no end_date)
func setupOngoingActivityTestDB(t *testing.T) *sql.DB {
	testDB := qntxtest.CreateTestDB(t)
	store := NewSQLStore(testDB, nil)

	now := time.Now()
	attestationTime := now.Add(-10 * time.Minute) // Recorded 10 minutes ago

	// Neuron N042: Ongoing activity (no end_date)
	// Started 1.5s before attestation, still active when recorded
	// Duration should be calculated as: attestation_time - start_time = 1.5s
	ongoingStart := attestationTime.Add(-1500 * time.Millisecond)

	// Create attestation with empty end_date to simulate ongoing activity
	ongoingMeta := map[string]string{
		"start_time":    ongoingStart.Format(time.RFC3339),
		"end_time":      "", // Empty = ongoing
		"duration_s":    "1.5",
		"activity_type": "sustained_theta",
		"brain_region":  "hippocampus_ca1",
	}
	ongoingMetaJSON, _ := json.Marshal(ongoingMeta)

	err := store.CreateAttestation(&types.As{
		ID:         "n042_ongoing",
		Subjects:   []string{"CA1_PYRAMIDAL_N042"},
		Predicates: []string{"activity_duration_s"},
		Contexts:   []string{"1.5"},
		Actors:     []string{"openbci:recording"},
		Timestamp:  attestationTime,
		Source:     "neural_recording",
		Attributes: string(ongoingMetaJSON),
		CreatedAt:  attestationTime,
	})
	require.NoError(t, err)
	// N042 calculated duration: 1.5s (from start_time to attestation timestamp)

	// Neuron N043: Completed activity with explicit end_date
	completedStart := attestationTime.Add(-2 * time.Second)
	completedEnd := attestationTime.Add(-1200 * time.Millisecond)

	completedMeta := map[string]string{
		"start_time":    completedStart.Format(time.RFC3339),
		"end_time":      completedEnd.Format(time.RFC3339),
		"duration_s":    "0.8",
		"activity_type": "spike_burst",
		"brain_region":  "hippocampus_ca1",
	}
	completedMetaJSON, _ := json.Marshal(completedMeta)

	err = store.CreateAttestation(&types.As{
		ID:         "n043_completed",
		Subjects:   []string{"CA1_PYRAMIDAL_N043"},
		Predicates: []string{"activity_duration_s"},
		Contexts:   []string{"0.8"},
		Actors:     []string{"openbci:recording"},
		Timestamp:  attestationTime,
		Source:     "neural_recording",
		Attributes: string(completedMetaJSON),
		CreatedAt:  attestationTime,
	})
	require.NoError(t, err)
	// N043 explicit duration: 0.8s

	// Neuron N001: Another ongoing activity with longer duration
	longOngoingStart := attestationTime.Add(-3 * time.Second)

	longOngoingMeta := map[string]string{
		"start_time":    longOngoingStart.Format(time.RFC3339),
		"end_time":      "", // Empty = ongoing
		"duration_s":    "3.0",
		"activity_type": "persistent_firing",
		"brain_region":  "hippocampus_ca3",
	}
	longOngoingMetaJSON, _ := json.Marshal(longOngoingMeta)

	err = store.CreateAttestation(&types.As{
		ID:         "n001_ongoing",
		Subjects:   []string{"CA3_PYRAMIDAL_N001"},
		Predicates: []string{"activity_duration_s"},
		Contexts:   []string{"3.0"},
		Actors:     []string{"openbci:recording"},
		Timestamp:  attestationTime,
		Source:     "neural_recording",
		Attributes: string(longOngoingMetaJSON),
		CreatedAt:  attestationTime,
	})
	require.NoError(t, err)
	// N001 calculated duration: 3.0s

	return testDB
}

// TestTemporalAggregation_OngoingActivity tests duration calculation for activities without end_date
// Query: ax activity_duration_s over 1.0s
//
// Duration calculation logic:
//   - If end_time is present: use explicit duration_s from metadata
//   - If end_time is empty/missing: duration = attestation_timestamp - start_time
//
// Test data:
//   N042 (ongoing):   start_time = T-1.5s, end_time = "", attestation_time = T
//                     Calculated duration: 1.5s (from start to attestation)
//   N043 (completed): start_time = T-2.0s, end_time = T-1.2s
//                     Explicit duration: 0.8s
//   N001 (ongoing):   start_time = T-3.0s, end_time = "", attestation_time = T
//                     Calculated duration: 3.0s
//
// Expected: N042 (1.5s) and N001 (3.0s) match threshold >= 1.0s
//           N043 (0.8s) does NOT match
func TestTemporalAggregation_OngoingActivity(t *testing.T) {
	t.Skip("Phase 3: Ongoing activity - calculate duration from start_time to attestation timestamp when end_time missing")

	db := setupOngoingActivityTestDB(t)
	queryStore := NewSQLQueryStore(db, &mockNeuralQueryExpander{})

	// Query: ax activity_duration_s over 1.0s
	filter := types.AxFilter{
		OverComparison: &types.OverFilter{
			Value:    1.0,
			Unit:     "s",
			Operator: "over",
		},
	}

	results, err := queryStore.ExecuteAxQuery(context.Background(), filter)
	require.NoError(t, err)

	// Should return N042 (1.5s) and N001 (3.0s)
	// N043 excluded (0.8s < 1.0s)
	assert.Equal(t, 2, len(results), "Expected 2 attestations (N042 and N001)")

	// Verify results
	neuronIDs := make(map[string]bool)
	for _, result := range results {
		neuronIDs[result.Subjects[0]] = true
	}

	assert.True(t, neuronIDs["CA1_PYRAMIDAL_N042"], "N042 should match (ongoing 1.5s)")
	assert.True(t, neuronIDs["CA3_PYRAMIDAL_N001"], "N001 should match (ongoing 3.0s)")
	assert.False(t, neuronIDs["CA1_PYRAMIDAL_N043"], "N043 should NOT match (completed 0.8s)")
}

// ==============================================================================
// PHASE 2: Multiple Predicates with AND Logic
// ==============================================================================

// setupMultiplePredicatesTestDB creates test database with neurons having both activity duration and neurotransmitter properties
func setupMultiplePredicatesTestDB(t *testing.T) *sql.DB {
	testDB := qntxtest.CreateTestDB(t)
	store := NewSQLStore(testDB, nil)

	now := time.Now()
	attestationTime := now.Add(-30 * time.Minute)

	// Neuron N042: Has sufficient activity (1.2s) AND uses glutamate
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_PYRAMIDAL_N042",
		activityID:   "n042_activity",
		startTime:    now.Add(-45 * time.Minute),
		endTime:      now.Add(-45*time.Minute + 1200*time.Millisecond),
		durationS:    1.2,
		activityType: "theta_oscillation",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	// Add neurotransmitter attestation for N042
	err := store.CreateAttestation(&types.As{
		ID:         "n042_neurotransmitter",
		Subjects:   []string{"CA1_PYRAMIDAL_N042"},
		Predicates: []string{"uses_neurotransmitter"},
		Contexts:   []string{"glutamate"},
		Actors:     []string{"neurochemistry:analysis"},
		Timestamp:  attestationTime,
		Source:     "neural_recording",
		CreatedAt:  attestationTime,
	})
	require.NoError(t, err)
	// N042: 1.2s activity + glutamate ✓ (matches both conditions)

	// Neuron N043: Has sufficient activity (1.5s) but uses GABA (not glutamate)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA1_INTERNEURON_N043",
		activityID:   "n043_activity",
		startTime:    now.Add(-40 * time.Minute),
		endTime:      now.Add(-40*time.Minute + 1500*time.Millisecond),
		durationS:    1.5,
		activityType: "gamma_synchrony",
		region:       "hippocampus_ca1",
		attestedAt:   attestationTime,
	})

	err = store.CreateAttestation(&types.As{
		ID:         "n043_neurotransmitter",
		Subjects:   []string{"CA1_INTERNEURON_N043"},
		Predicates: []string{"uses_neurotransmitter"},
		Contexts:   []string{"GABA"},
		Actors:     []string{"neurochemistry:analysis"},
		Timestamp:  attestationTime,
		Source:     "neural_recording",
		CreatedAt:  attestationTime,
	})
	require.NoError(t, err)
	// N043: 1.5s activity + GABA ✗ (fails neurotransmitter condition)

	// Neuron N001: Uses glutamate but insufficient activity (0.7s)
	createActivityAttestation(t, store, &neuralActivity{
		neuronID:     "CA3_PYRAMIDAL_N001",
		activityID:   "n001_activity",
		startTime:    now.Add(-50 * time.Minute),
		endTime:      now.Add(-50*time.Minute + 700*time.Millisecond),
		durationS:    0.7,
		activityType: "spike_burst",
		region:       "hippocampus_ca3",
		attestedAt:   attestationTime,
	})

	err = store.CreateAttestation(&types.As{
		ID:         "n001_neurotransmitter",
		Subjects:   []string{"CA3_PYRAMIDAL_N001"},
		Predicates: []string{"uses_neurotransmitter"},
		Contexts:   []string{"glutamate"},
		Actors:     []string{"neurochemistry:analysis"},
		Timestamp:  attestationTime,
		Source:     "neural_recording",
		CreatedAt:  attestationTime,
	})
	require.NoError(t, err)
	// N001: 0.7s activity + glutamate ✗ (fails activity duration threshold)

	return testDB
}

// TestTemporalAggregation_MultiplePredicatesAND tests combining duration aggregation with other predicate filters
// Query: ax activity_duration_s over 1.0s AND uses_neurotransmitter glutamate
//
// Logic:
//   1. Find subjects with activity_duration_s >= 1.0s (via aggregation)
//   2. Find subjects with uses_neurotransmitter = glutamate
//   3. Return intersection (subjects matching BOTH conditions)
//
// Test data:
//   N042: activity 1.2s + glutamate → MATCH (both conditions)
//   N043: activity 1.5s + GABA      → NO MATCH (wrong neurotransmitter)
//   N001: activity 0.7s + glutamate → NO MATCH (insufficient activity)
//
// Expected: Only N042 matches both conditions
func TestTemporalAggregation_MultiplePredicatesAND(t *testing.T) {
	t.Skip("Phase 2: Multiple predicates - implement AND logic combining duration aggregation with other filters")

	db := setupMultiplePredicatesTestDB(t)
	queryStore := NewSQLQueryStore(db, &mockNeuralQueryExpander{})

	// Query: ax activity_duration_s over 1.0s AND uses_neurotransmitter glutamate
	// Note: AND logic would need to be implemented in parser/executor
	// For now, this test documents the expected behavior
	filter := types.AxFilter{
		OverComparison: &types.OverFilter{
			Value:    1.0,
			Unit:     "s",
			Operator: "over",
		},
		// TODO: Add AND clause support
		// PredicateFilters: []PredicateFilter{
		//     {Predicate: "uses_neurotransmitter", Context: "glutamate"},
		// },
	}

	results, err := queryStore.ExecuteAxQuery(context.Background(), filter)
	require.NoError(t, err)

	// Should only return N042's attestations (both activity and neurotransmitter)
	// Expected: 2 attestations (1 activity + 1 neurotransmitter)
	assert.Equal(t, 2, len(results), "Expected 2 attestations for N042")

	// Verify all results are for N042
	for _, result := range results {
		assert.Equal(t, []string{"CA1_PYRAMIDAL_N042"}, result.Subjects)
	}
}
