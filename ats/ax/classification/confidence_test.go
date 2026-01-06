package classification

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/internal/util"
)

func TestConfidenceCalculator_CalculateConfidence(t *testing.T) {
	cm := NewCredibilityManager()
	ta := NewTemporalAnalyzer(DefaultTemporalConfig())
	cc := NewConfidenceCalculator(cm, ta)

	now := time.Now()

	tests := []struct {
		name     string
		claims   []ClaimWithTiming
		minScore float64
		maxScore float64
	}{
		{
			name:     "Empty claims",
			claims:   []ClaimWithTiming{},
			minScore: 0.0,
			maxScore: 0.0,
		},
		{
			name: "Single high credibility claim",
			claims: []ClaimWithTiming{
				{
					Actor:     "manager@company.com",
					Timestamp: now,
					Predicate: "senior_developer",
					Subject:   "john.doe@company.com",
					Context:   "engineering",
				},
			},
			minScore: 0.8,
			maxScore: 1.0,
		},
		{
			name: "Single low credibility claim",
			claims: []ClaimWithTiming{
				{
					Actor:     "unknown-scraper",
					Timestamp: now.Add(-48 * time.Hour),
					Predicate: "developer",
					Subject:   "john.doe@company.com",
					Context:   "engineering",
				},
			},
			minScore: 0.0,
			maxScore: 0.6, // External actor (0.5) with some recency
		},
		{
			name: "Multiple independent high credibility sources",
			claims: []ClaimWithTiming{
				{
					Actor:     "hr@company.com",
					Timestamp: now,
					Predicate: "senior_developer",
					Subject:   "john.doe@company.com",
					Context:   "engineering",
				},
				{
					Actor:     "manager@company.com",
					Timestamp: now.Add(10 * time.Second),
					Predicate: "senior_developer",
					Subject:   "john.doe@company.com",
					Context:   "engineering",
				},
			},
			minScore: 0.8,
			maxScore: 1.0,
		},
		{
			name: "Mixed credibility sources",
			claims: []ClaimWithTiming{
				{
					Actor:     "ats+system",
					Timestamp: now.Add(-1 * time.Hour),
					Predicate: "developer",
					Subject:   "john.doe@company.com",
					Context:   "engineering",
				},
				{
					Actor:     "claude-llm",
					Timestamp: now.Add(-30 * time.Minute),
					Predicate: "senior_developer",
					Subject:   "john.doe@company.com",
					Context:   "engineering",
				},
				{
					Actor:     "manager@company.com",
					Timestamp: now,
					Predicate: "tech_lead",
					Subject:   "john.doe@company.com",
					Context:   "engineering",
				},
			},
			minScore: 0.6,
			maxScore: 1.0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			confidence := cc.CalculateConfidence(test.claims)
			if confidence < test.minScore || confidence > test.maxScore {
				t.Errorf("Expected confidence between %f and %f, got %f", test.minScore, test.maxScore, confidence)
			}
		})
	}
}

func TestConfidenceCalculator_CalculateSourceDiversityBonus(t *testing.T) {
	cm := NewCredibilityManager()
	ta := NewTemporalAnalyzer(DefaultTemporalConfig())
	cc := NewConfidenceCalculator(cm, ta)

	now := time.Now()

	tests := []struct {
		name     string
		claims   []ClaimWithTiming
		expected float64
	}{
		{
			name: "Single source",
			claims: []ClaimWithTiming{
				{Actor: "actor1", Timestamp: now, Predicate: "role1"},
			},
			expected: 0.0,
		},
		{
			name: "Two independent sources",
			claims: []ClaimWithTiming{
				{Actor: "actor1", Timestamp: now, Predicate: "role1"},
				{Actor: "actor2", Timestamp: now, Predicate: "role1"},
			},
			expected: 0.1,
		},
		{
			name: "Three independent sources",
			claims: []ClaimWithTiming{
				{Actor: "actor1", Timestamp: now, Predicate: "role1"},
				{Actor: "actor2", Timestamp: now, Predicate: "role1"},
				{Actor: "actor3", Timestamp: now, Predicate: "role1"},
			},
			expected: 0.2,
		},
		{
			name: "Four independent sources (should cap at 0.3)",
			claims: []ClaimWithTiming{
				{Actor: "actor1", Timestamp: now, Predicate: "role1"},
				{Actor: "actor2", Timestamp: now, Predicate: "role1"},
				{Actor: "actor3", Timestamp: now, Predicate: "role1"},
				{Actor: "actor4", Timestamp: now, Predicate: "role1"},
			},
			expected: 0.3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bonus := cc.calculateSourceDiversityBonus(test.claims)
			if bonus != test.expected {
				t.Errorf("Expected bonus %f, got %f", test.expected, bonus)
			}
		})
	}
}

func TestConfidenceCalculator_CalculateCredibilityBonus(t *testing.T) {
	cm := NewCredibilityManager()
	ta := NewTemporalAnalyzer(DefaultTemporalConfig())
	cc := NewConfidenceCalculator(cm, ta)

	now := time.Now()

	tests := []struct {
		name     string
		claims   []ClaimWithTiming
		expected float64
	}{
		{
			name: "High credibility human actor",
			claims: []ClaimWithTiming{
				{Actor: "manager@company.com", Timestamp: now, Predicate: "role1"},
			},
			expected: 0.18, // 0.9 * 0.2
		},
		{
			name: "Medium credibility LLM actor",
			claims: []ClaimWithTiming{
				{Actor: "claude-llm", Timestamp: now, Predicate: "role1"},
			},
			expected: 0.12, // 0.6 * 0.2
		},
		{
			name: "Low credibility system actor",
			claims: []ClaimWithTiming{
				{Actor: "ats+system", Timestamp: now, Predicate: "role1"},
			},
			expected: 0.08, // 0.4 * 0.2
		},
		{
			name: "Mixed actors (should use highest)",
			claims: []ClaimWithTiming{
				{Actor: "ats+system", Timestamp: now, Predicate: "role1"},
				{Actor: "manager@company.com", Timestamp: now, Predicate: "role1"},
			},
			expected: 0.18, // Should use human's 0.9 * 0.2
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bonus := cc.calculateCredibilityBonus(test.claims)
			if util.AbsFloat64(bonus-test.expected) > 0.001 { // Allow small floating point differences
				t.Errorf("Expected bonus %f, got %f", test.expected, bonus)
			}
		})
	}
}

func TestConfidenceCalculator_CalculateConsistencyBonus(t *testing.T) {
	cm := NewCredibilityManager()
	ta := NewTemporalAnalyzer(DefaultTemporalConfig())
	cc := NewConfidenceCalculator(cm, ta)

	now := time.Now()

	tests := []struct {
		name     string
		claims   []ClaimWithTiming
		expected float64
	}{
		{
			name: "Same predicate",
			claims: []ClaimWithTiming{
				{Actor: "actor1", Timestamp: now, Predicate: "senior_developer"},
				{Actor: "actor2", Timestamp: now, Predicate: "senior_developer"},
			},
			expected: 0.1,
		},
		{
			name: "Related predicates",
			claims: []ClaimWithTiming{
				{Actor: "actor1", Timestamp: now, Predicate: "junior_developer"},
				{Actor: "actor2", Timestamp: now, Predicate: "developer"},
			},
			expected: 0.05,
		},
		{
			name: "Unrelated predicates",
			claims: []ClaimWithTiming{
				{Actor: "actor1", Timestamp: now, Predicate: "developer"},
				{Actor: "actor2", Timestamp: now, Predicate: "manager"},
			},
			expected: 0.0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bonus := cc.calculateConsistencyBonus(test.claims)
			if bonus != test.expected {
				t.Errorf("Expected bonus %f, got %f", test.expected, bonus)
			}
		})
	}
}

func TestConfidenceCalculator_RequiresHumanReview(t *testing.T) {
	cm := NewCredibilityManager()
	ta := NewTemporalAnalyzer(DefaultTemporalConfig())
	cc := NewConfidenceCalculator(cm, ta)

	tests := []struct {
		confidence     float64
		expectedReview bool
		description    string
	}{
		{0.8, false, "High confidence should not require review"},
		{0.5, false, "Medium confidence should not require review"},
		{0.3, false, "Exactly at threshold should not require review"},
		{0.29, true, "Below threshold should require review"},
		{0.1, true, "Low confidence should require review"},
		{0.0, true, "Zero confidence should require review"},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			result := cc.RequiresHumanReview(test.confidence)
			if result != test.expectedReview {
				t.Errorf("Confidence %f: expected review=%v, got %v",
					test.confidence, test.expectedReview, result)
			}
		})
	}
}

func TestConfidenceCalculator_GetConfidenceLevel(t *testing.T) {
	cm := NewCredibilityManager()
	ta := NewTemporalAnalyzer(DefaultTemporalConfig())
	cc := NewConfidenceCalculator(cm, ta)

	tests := []struct {
		confidence float64
		expected   string
	}{
		{0.9, "high"},
		{0.8, "high"},
		{0.7, "medium"},
		{0.6, "medium"},
		{0.5, "low"},
		{0.4, "low"},
		{0.3, "very_low"},
		{0.1, "very_low"},
		{0.0, "very_low"},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			level := cc.GetConfidenceLevel(test.confidence)
			if level != test.expected {
				t.Errorf("Confidence %f: expected %s, got %s",
					test.confidence, test.expected, level)
			}
		})
	}
}

func TestConfidenceCalculator_CalculateActorAgreement(t *testing.T) {
	cm := NewCredibilityManager()
	ta := NewTemporalAnalyzer(DefaultTemporalConfig())
	cc := NewConfidenceCalculator(cm, ta)

	now := time.Now()

	tests := []struct {
		name     string
		claims   []ClaimWithTiming
		expected float64
	}{
		{
			name:     "Single claim",
			claims:   []ClaimWithTiming{{Actor: "actor1", Timestamp: now, Predicate: "role1"}},
			expected: 1.0,
		},
		{
			name: "Full agreement",
			claims: []ClaimWithTiming{
				{Actor: "actor1", Timestamp: now, Predicate: "senior_developer"},
				{Actor: "actor2", Timestamp: now, Predicate: "senior_developer"},
				{Actor: "actor3", Timestamp: now, Predicate: "senior_developer"},
			},
			expected: 1.0,
		},
		{
			name: "Partial agreement",
			claims: []ClaimWithTiming{
				{Actor: "actor1", Timestamp: now, Predicate: "senior_developer"},
				{Actor: "actor2", Timestamp: now, Predicate: "senior_developer"},
				{Actor: "actor3", Timestamp: now, Predicate: "manager"},
			},
			expected: 0.6666666666666666, // 2/3
		},
		{
			name: "No agreement",
			claims: []ClaimWithTiming{
				{Actor: "actor1", Timestamp: now, Predicate: "developer"},
				{Actor: "actor2", Timestamp: now, Predicate: "manager"},
				{Actor: "actor3", Timestamp: now, Predicate: "analyst"},
			},
			expected: 0.3333333333333333, // 1/3
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agreement := cc.CalculateActorAgreement(test.claims)
			if util.AbsFloat64(agreement-test.expected) > 0.001 {
				t.Errorf("Expected agreement %f, got %f", test.expected, agreement)
			}
		})
	}
}

func TestConfidenceCalculator_SetReviewThreshold(t *testing.T) {
	cm := NewCredibilityManager()
	ta := NewTemporalAnalyzer(DefaultTemporalConfig())
	cc := NewConfidenceCalculator(cm, ta)

	// Test default threshold
	if cc.RequiresHumanReview(0.29) != true {
		t.Error("Expected default threshold 0.3 to require review for 0.29")
	}

	// Change threshold
	cc.SetReviewThreshold(0.5)

	if cc.RequiresHumanReview(0.49) != true {
		t.Error("Expected new threshold 0.5 to require review for 0.49")
	}

	if cc.RequiresHumanReview(0.51) != false {
		t.Error("Expected new threshold 0.5 to not require review for 0.51")
	}
}

