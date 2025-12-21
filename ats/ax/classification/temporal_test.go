package classification

import (
	"testing"
	"time"
)

// The Attestation Chronicles: Temporal analysis testing through Matrix timeline events.
// These tests explore how the system handles time-based patterns in attestations,
// from the moment of awakening through the evolution of the resistance network.
// Time itself becomes a crucial factor in distinguishing legitimate identity evolution
// from suspicious activity or coordinated attacks.

func TestTemporalAnalyzer_AnalyzeTemporalPattern(t *testing.T) {
	config := DefaultTemporalConfig()
	ta := NewTemporalAnalyzer(config)

	now := time.Now()

	// Test cases reflect key temporal patterns in the resistance network
	tests := []struct {
		name     string
		timings  []ClaimTiming
		expected TemporalPattern
	}{
		{
			name: "Multiple witnesses verify Neo's awakening",
			timings: []ClaimTiming{
				{Actor: "morpheus@nebuchadnezzar", Timestamp: now, Predicate: "awakened"},
				{Actor: "trinity@nebuchadnezzar", Timestamp: now.Add(10 * time.Second), Predicate: "awakened"},
				{Actor: "tank@nebuchadnezzar", Timestamp: now.Add(30 * time.Second), Predicate: "awakened"},
			},
			expected: TemporalSimultaneous,
		},
		{
			name: "Neo's progression from programmer to The One",
			timings: []ClaimTiming{
				{Actor: "matrix-system", Timestamp: now.Add(-2 * time.Hour), Predicate: "programmer"},
				{Actor: "morpheus@nebuchadnezzar", Timestamp: now, Predicate: "the_one"},
			},
			expected: TemporalSequential,
		},
		{
			name: "Multiple actors establishing classifications over time",
			timings: []ClaimTiming{
				{Actor: "command-center", Timestamp: now.Add(-30 * time.Minute), Predicate: "specialist"},
				{Actor: "registry-system@acme", Timestamp: now.Add(-20 * time.Minute), Predicate: "analyst"},
				{Actor: "profile-api", Timestamp: now.Add(-10 * time.Minute), Predicate: "contractor"},
			},
			expected: TemporalSequential, // There are gaps > verification window, so it's sequential
		},
		{
			name: "Long-term resistance network development",
			timings: []ClaimTiming{
				{Actor: "oracle@sanctuary", Timestamp: now.Add(-25 * time.Hour), Predicate: "potential"},
				{Actor: "morpheus@nebuchadnezzar", Timestamp: now.Add(-12 * time.Hour), Predicate: "trainee"},
				{Actor: "zion-command", Timestamp: now, Predicate: "operative"},
			},
			expected: TemporalSequential, // Has gaps so classified as sequential
		},
		{
			name:     "Trinity confirms single identity",
			timings:  []ClaimTiming{{Actor: "trinity@nebuchadnezzar", Timestamp: now, Predicate: "pilot"}},
			expected: TemporalSimultaneous,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ta.AnalyzeTemporalPattern(test.timings)
			if result != test.expected {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestTemporalAnalyzer_IsSimultaneous(t *testing.T) {
	config := DefaultTemporalConfig()
	ta := NewTemporalAnalyzer(config)

	now := time.Now()

	// Testing temporal proximity for verification scenarios
	tests := []struct {
		name     string
		t1       time.Time
		t2       time.Time
		expected bool
	}{
		{
			name:     "Morpheus and Trinity confirm Neo simultaneously",
			t1:       now,
			t2:       now,
			expected: true,
		},
		{
			name:     "Ship crew verifies within operational window",
			t1:       now,
			t2:       now.Add(30 * time.Second),
			expected: true,
		},
		{
			name:     "Last-second verification before window closes",
			t1:       now,
			t2:       now.Add(59 * time.Second),
			expected: true,
		},
		{
			name:     "Verification missed by seconds - suspicious timing",
			t1:       now,
			t2:       now.Add(61 * time.Second),
			expected: false,
		},
		{
			name:     "Different Matrix entry/exit times - not simultaneous",
			t1:       now,
			t2:       now.Add(10 * time.Minute),
			expected: false,
		},
		{
			name:     "Tank verifies before Link (reverse chronology but valid)",
			t1:       now.Add(30 * time.Second),
			t2:       now,
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ta.IsSimultaneous(test.t1, test.t2)
			if result != test.expected {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestTemporalAnalyzer_IsEvolutionTimespan(t *testing.T) {
	config := DefaultTemporalConfig()
	ta := NewTemporalAnalyzer(config)

	now := time.Now()

	// Testing realistic timeframes for identity evolution in the resistance
	tests := []struct {
		name     string
		t1       time.Time
		t2       time.Time
		expected bool
	}{
		{
			name:     "Instant status change - likely false identity",
			t1:       now,
			t2:       now.Add(30 * time.Second),
			expected: false,
		},
		{
			name:     "Neo learns combat skills - realistic training period",
			t1:       now,
			t2:       now.Add(2 * time.Hour),
			expected: true,
		},
		{
			name:     "Resistance member gains ship assignment",
			t1:       now,
			t2:       now.Add(12 * time.Hour),
			expected: true,
		},
		{
			name:     "Suspicious status change - too fast for real growth",
			t1:       now,
			t2:       now.Add(25 * time.Hour),
			expected: false,
		},
		{
			name:     "Unrealistic progression timeline",
			t1:       now,
			t2:       now.Add(48 * time.Hour),
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ta.IsEvolutionTimespan(test.t1, test.t2)
			if result != test.expected {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestTemporalAnalyzer_IsObsolete(t *testing.T) {
	config := DefaultTemporalConfig()
	ta := NewTemporalAnalyzer(config)

	now := time.Now()

	// Testing attestation freshness in the resistance network
	tests := []struct {
		name      string
		timestamp time.Time
		expected  bool
	}{
		{
			name:      "Fresh intel from recent mission",
			timestamp: now.Add(-1 * time.Hour),
			expected:  false,
		},
		{
			name:      "Still valid ship assignment",
			timestamp: now.Add(-36 * time.Hour),
			expected:  false,
		},
		{
			name:      "Pre-war attestation - outdated but archived",
			timestamp: now.Add(-366 * 24 * time.Hour), // Just over 1 year
			expected:  true,
		},
		{
			name:      "Ancient Matrix-era data - completely obsolete",
			timestamp: now.Add(-400 * 24 * time.Hour), // Over 1 year
			expected:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ta.IsObsolete(test.timestamp)
			if result != test.expected {
				t.Errorf("Expected %v, got %v", test.expected, result)
			}
		})
	}
}

func TestTemporalAnalyzer_CalculateRecencyScore(t *testing.T) {
	config := DefaultTemporalConfig()
	ta := NewTemporalAnalyzer(config)

	now := time.Now()

	// Testing how recency affects trust scores in the resistance
	tests := []struct {
		name      string
		timestamp time.Time
		minScore  float64
		maxScore  float64
	}{
		{
			name:      "Real-time mission status",
			timestamp: now.Add(-10 * time.Second),
			minScore:  1.0,
			maxScore:  1.0,
		},
		{
			name:      "Fresh crew verification",
			timestamp: now.Add(-30 * time.Second),
			minScore:  1.0,
			maxScore:  1.0,
		},
		{
			name:      "Recent training completion",
			timestamp: now.Add(-6 * time.Hour),
			minScore:  0.5,
			maxScore:  1.0,
		},
		{
			name:      "Previous operation data",
			timestamp: now.Add(-48 * time.Hour),
			minScore:  0.1,
			maxScore:  0.5,
		},
		{
			name:      "Pre-awakening Matrix records",
			timestamp: now.Add(-400 * 24 * time.Hour), // Over 1 year
			minScore:  0.1,
			maxScore:  0.1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			score := ta.CalculateRecencyScore(test.timestamp)
			if score < test.minScore || score > test.maxScore {
				t.Errorf("Expected score between %f and %f, got %f", test.minScore, test.maxScore, score)
			}
		})
	}
}

func TestTemporalAnalyzer_GroupByTimeWindow(t *testing.T) {
	config := DefaultTemporalConfig()
	ta := NewTemporalAnalyzer(config)

	now := time.Now()
	// Simulate resistance verification waves during ship operations
	timings := []ClaimTiming{
		{Actor: "morpheus@nebuchadnezzar", Timestamp: now, Predicate: "pilot"},
		{Actor: "trinity@nebuchadnezzar", Timestamp: now.Add(10 * time.Second), Predicate: "pilot"},      // Same group (within 1 minute)
		{Actor: "zion-command", Timestamp: now.Add(2 * time.Minute), Predicate: "operative"},             // New group (>1 minute gap)
		{Actor: "niobe@logos", Timestamp: now.Add(3 * time.Minute), Predicate: "captain"},                // Same group (within 1 minute of zion-command)
		{Actor: "link@logos", Timestamp: now.Add(3*time.Minute + 30*time.Second), Predicate: "operator"}, // Same group (within 1 minute of niobe)
	}

	groups := ta.GroupByTimeWindow(timings)

	if len(groups) != 2 {
		t.Errorf("Expected 2 groups, got %d", len(groups))
	}

	// First group should have 2 items (morpheus, trinity)
	if len(groups[0]) != 2 {
		t.Errorf("Expected first group to have 2 items, got %d", len(groups[0]))
	}

	// Second group should have 3 items (zion-command, niobe, link)
	if len(groups[1]) != 3 {
		t.Errorf("Expected second group to have 3 items, got %d", len(groups[1]))
	}
}

func TestTemporalAnalyzer_GetMostRecentClaim(t *testing.T) {
	config := DefaultTemporalConfig()
	ta := NewTemporalAnalyzer(config)

	now := time.Now()
	// Test finding most recent attestation across resistance network
	timings := []ClaimTiming{
		{Actor: "tank@nebuchadnezzar", Timestamp: now.Add(-1 * time.Hour), Predicate: "gunner"},
		{Actor: "zee@haven-dock", Timestamp: now, Predicate: "engineer"}, // Most recent
		{Actor: "dozer@haven-dock", Timestamp: now.Add(-30 * time.Minute), Predicate: "mechanic"},
	}

	mostRecent := ta.GetMostRecentClaim(timings)

	if mostRecent.Actor != "zee@haven-dock" {
		t.Errorf("Expected zee@haven-dock to be most recent, got %s", mostRecent.Actor)
	}

	if !mostRecent.Timestamp.Equal(now) {
		t.Error("Expected most recent timestamp to match")
	}
}

func TestTemporalAnalyzer_CalculateTemporalConfidence(t *testing.T) {
	config := DefaultTemporalConfig()
	ta := NewTemporalAnalyzer(config)

	now := time.Now()

	// Confidence calculations based on Matrix resistance scenarios
	tests := []struct {
		name     string
		timings  []ClaimTiming
		expected float64
	}{
		{
			name:     "Single trusted verification",
			timings:  []ClaimTiming{{Actor: "oracle@sanctuary", Timestamp: now, Predicate: "potential"}},
			expected: 1.0,
		},
		{
			name: "Crew consensus on new member",
			timings: []ClaimTiming{
				{Actor: "morpheus@nebuchadnezzar", Timestamp: now, Predicate: "ready"},
				{Actor: "trinity@nebuchadnezzar", Timestamp: now.Add(10 * time.Second), Predicate: "ready"},
			},
			expected: 0.9,
		},
		{
			name: "Natural progression through resistance ranks",
			timings: []ClaimTiming{
				{Actor: "zion-training", Timestamp: now.Add(-2 * time.Hour), Predicate: "recruit"},
				{Actor: "ship-commander", Timestamp: now, Predicate: "operative"},
			},
			expected: 0.8,
		},
		{
			name: "Staggered mission assignments",
			timings: []ClaimTiming{
				{Actor: "logistics@haven", Timestamp: now.Add(-30 * time.Minute), Predicate: "engineer"},
				{Actor: "operations@haven", Timestamp: now.Add(-20 * time.Minute), Predicate: "analyst"},
			},
			expected: 0.8, // Sequential pattern gives 0.8 confidence
		},
		{
			name: "Long-term network development",
			timings: []ClaimTiming{
				{Actor: "oracle@sanctuary", Timestamp: now.Add(-25 * time.Hour), Predicate: "prophecy"},
				{Actor: "zion-council", Timestamp: now, Predicate: "fulfillment"},
			},
			expected: 0.8, // Actually classified as sequential with gaps
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			confidence := ta.CalculateTemporalConfidence(test.timings)
			if confidence != test.expected {
				t.Errorf("Expected confidence %f, got %f", test.expected, confidence)
			}
		})
	}
}
