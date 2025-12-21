package classification

import (
	"sort"
	"time"
)

// TemporalAnalyzer analyzes temporal patterns in claims
type TemporalAnalyzer struct {
	config TemporalConfig
}

// NewTemporalAnalyzer creates a new temporal analyzer with configuration
func NewTemporalAnalyzer(config TemporalConfig) *TemporalAnalyzer {
	return &TemporalAnalyzer{
		config: config,
	}
}

// TemporalPattern represents different temporal relationships between claims
type TemporalPattern string

const (
	TemporalSimultaneous TemporalPattern = "simultaneous" // Within verification window
	TemporalSequential   TemporalPattern = "sequential"   // Clear time ordering
	TemporalOverlapping  TemporalPattern = "overlapping"  // Some temporal overlap
	TemporalDistributed  TemporalPattern = "distributed"  // Spread over long period
)

// ClaimTiming represents timing information for a claim
type ClaimTiming struct {
	Actor     string
	Timestamp time.Time
	Predicate string
}

// AnalyzeTemporalPattern determines the temporal pattern of claims
func (ta *TemporalAnalyzer) AnalyzeTemporalPattern(timings []ClaimTiming) TemporalPattern {
	if len(timings) <= 1 {
		return TemporalSimultaneous
	}

	// Sort by timestamp
	sort.Slice(timings, func(i, j int) bool {
		return timings[i].Timestamp.Before(timings[j].Timestamp)
	})

	earliest := timings[0].Timestamp
	latest := timings[len(timings)-1].Timestamp
	totalSpan := latest.Sub(earliest)

	// Check if all claims are within verification window
	if totalSpan <= ta.config.VerificationWindow {
		return TemporalSimultaneous
	}

	// Check if claims are clearly sequential with gaps
	if ta.hasSequentialGaps(timings) {
		return TemporalSequential
	}

	// Check if spread over long period
	if totalSpan > ta.config.EvolutionWindow {
		return TemporalDistributed
	}

	return TemporalOverlapping
}

// hasSequentialGaps checks if there are clear gaps between claim groups
func (ta *TemporalAnalyzer) hasSequentialGaps(timings []ClaimTiming) bool {
	if len(timings) < 2 {
		return false
	}

	// Look for gaps larger than verification window
	for i := 1; i < len(timings); i++ {
		gap := timings[i].Timestamp.Sub(timings[i-1].Timestamp)
		if gap > ta.config.VerificationWindow {
			return true
		}
	}

	return false
}

// IsSimultaneous checks if two timestamps are within verification window
func (ta *TemporalAnalyzer) IsSimultaneous(t1, t2 time.Time) bool {
	diff := t1.Sub(t2)
	if diff < 0 {
		diff = -diff
	}
	return diff <= ta.config.VerificationWindow
}

// IsEvolutionTimespan checks if timespan suggests natural evolution
func (ta *TemporalAnalyzer) IsEvolutionTimespan(t1, t2 time.Time) bool {
	diff := t1.Sub(t2)
	if diff < 0 {
		diff = -diff
	}
	return diff >= ta.config.VerificationWindow && diff <= ta.config.EvolutionWindow
}

// IsObsolete checks if a timestamp is too old to be relevant
func (ta *TemporalAnalyzer) IsObsolete(timestamp time.Time) bool {
	age := time.Since(timestamp)
	return age > ta.config.ObsolescenceWindow
}

// CalculateRecencyScore returns a score (0.0-1.0) based on how recent a timestamp is
func (ta *TemporalAnalyzer) CalculateRecencyScore(timestamp time.Time) float64 {
	age := time.Since(timestamp)

	// Recent claims (within verification window) get full score
	if age <= ta.config.VerificationWindow {
		return 1.0
	}

	// Claims within evolution window get declining score
	if age <= ta.config.EvolutionWindow {
		ratio := float64(age) / float64(ta.config.EvolutionWindow)
		return 1.0 - (ratio * 0.5) // Decline from 1.0 to 0.5
	}

	// Very old claims get low score but not zero
	if age <= ta.config.ObsolescenceWindow {
		ratio := float64(age) / float64(ta.config.ObsolescenceWindow)
		return 0.5 - (ratio * 0.4) // Decline from 0.5 to 0.1
	}

	// Obsolete claims get minimal score
	return 0.1
}

// GroupByTimeWindow groups claims into temporal windows
func (ta *TemporalAnalyzer) GroupByTimeWindow(timings []ClaimTiming) [][]ClaimTiming {
	if len(timings) == 0 {
		return nil
	}

	// Sort by timestamp
	sort.Slice(timings, func(i, j int) bool {
		return timings[i].Timestamp.Before(timings[j].Timestamp)
	})

	var groups [][]ClaimTiming
	currentGroup := []ClaimTiming{timings[0]}

	for i := 1; i < len(timings); i++ {
		// If within verification window of last item in current group, add to group
		lastInGroup := currentGroup[len(currentGroup)-1]
		if ta.IsSimultaneous(timings[i].Timestamp, lastInGroup.Timestamp) {
			currentGroup = append(currentGroup, timings[i])
		} else {
			// Start new group
			groups = append(groups, currentGroup)
			currentGroup = []ClaimTiming{timings[i]}
		}
	}

	// Add final group
	groups = append(groups, currentGroup)
	return groups
}

// GetMostRecentClaim returns the claim with the most recent timestamp
func (ta *TemporalAnalyzer) GetMostRecentClaim(timings []ClaimTiming) ClaimTiming {
	if len(timings) == 0 {
		return ClaimTiming{}
	}

	mostRecent := timings[0]
	for _, timing := range timings[1:] {
		if timing.Timestamp.After(mostRecent.Timestamp) {
			mostRecent = timing
		}
	}

	return mostRecent
}

// CalculateTemporalConfidence returns confidence based on temporal patterns
func (ta *TemporalAnalyzer) CalculateTemporalConfidence(timings []ClaimTiming) float64 {
	if len(timings) <= 1 {
		return 1.0
	}

	pattern := ta.AnalyzeTemporalPattern(timings)

	switch pattern {
	case TemporalSimultaneous:
		return 0.9 // High confidence for simultaneous verification
	case TemporalSequential:
		return 0.8 // Good confidence for clear evolution
	case TemporalOverlapping:
		return 0.6 // Medium confidence for overlapping claims
	case TemporalDistributed:
		return 0.4 // Lower confidence for distributed claims
	default:
		return 0.5
	}
}
