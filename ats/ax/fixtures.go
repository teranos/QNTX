package ax

import (
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/util"
)

// TestFixtures provides predictable test data for Ask System testing
type TestFixtures struct {
	Attestations []types.As
	Predicates   []string
}

func NewTestFixtures() *TestFixtures {
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	lastWeek := now.AddDate(0, 0, -7)

	return &TestFixtures{
		Attestations: []types.As{
			// A100: Simple existence attestation
			{
				ID:         "A100",
				Subjects:   []string{"NEO"},
				Predicates: []string{"_"},
				Contexts:   []string{"_"},
				Actors:     []string{"system-admin@platform"},
				Timestamp:  lastWeek,
				Source:     "admin-system",
			},
			// A200: Multiple entities with organization relationship (cartesian expansion test)
			{
				ID:         "A200",
				Subjects:   []string{"NEO", "TRINITY", "MORPHEUS"},
				Predicates: []string{"member"},
				Contexts:   []string{"ACME"},
				Actors:     []string{"registry-system"},
				Timestamp:  yesterday,
				Source:     "registry-system",
			},
			// A300: Entities participating in projects (fuzzy matching test)
			{
				ID:         "A300",
				Subjects:   []string{"APOC", "SWITCH"},
				Predicates: []string{"participant", "contributor"},
				Contexts:   []string{"HAVEN_DEFENSE", "MATRIX_BREACH"},
				Actors:     []string{"project-tracker"},
				Timestamp:  yesterday,
				Source:     "project-tracker",
			},
			// A400: Ghost's classification (fuzzy matching variant)
			{
				ID:         "A400",
				Subjects:   []string{"GHOST"},
				Predicates: []string{"active participant"},
				Contexts:   []string{"RESEARCH_LAB"},
				Actors:     []string{"profile-system"},
				Timestamp:  now,
				Source:     "profile-system",
			},
			// A500: Dozer's coordinator role (conflict setup)
			{
				ID:         "A500",
				Subjects:   []string{"DOZER"},
				Predicates: []string{"coordinator"},
				Contexts:   []string{"RESEARCH_LAB"},
				Actors:     []string{"registry-system"},
				Timestamp:  lastWeek,
				Source:     "registry-system",
			},
			// A600: Dozer as participant (real conflict - different actor, different time)
			{
				ID:         "A600",
				Subjects:   []string{"DOZER"},
				Predicates: []string{"participant"},
				Contexts:   []string{"RESEARCH_LAB"},
				Actors:     []string{"profile-system"},
				Timestamp:  yesterday,
				Source:     "profile-system",
			},
			// A700: Dozer's role update - same actor, different time (should NOT conflict)
			{
				ID:         "A700",
				Subjects:   []string{"DOZER"},
				Predicates: []string{"senior coordinator"},
				Contexts:   []string{"RESEARCH_LAB"},
				Actors:     []string{"registry-system"},
				Timestamp:  now,
				Source:     "registry-system",
			},
		},
		Predicates: []string{
			"participant",
			"active participant",
			"senior participant",
			"principal participant",
			"participation coordinator",
			"contributor",
			"senior contributor",
			"coordinator",
			"activity coordinator",
			"group coordinator",
			"specialist",
			"analyst",
		},
	}
}

// GetConflictTestCase returns specific attestations for conflict testing
func (tf *TestFixtures) GetConflictTestCase() []types.As {
	var conflictAttestations []types.As
	for _, as := range tf.Attestations {
		if len(as.Subjects) > 0 && as.Subjects[0] == "DOZER" {
			conflictAttestations = append(conflictAttestations, as)
		}
	}
	return conflictAttestations
}

// GetFuzzyMatchingTestCase returns attestations for fuzzy predicate testing
func (tf *TestFixtures) GetFuzzyMatchingTestCase() []types.As {
	var roleAttestations []types.As
	for _, as := range tf.Attestations {
		for _, pred := range as.Predicates {
			if util.ContainsString([]string{"participant", "active participant", "contributor"}, pred) {
				roleAttestations = append(roleAttestations, as)
				break
			}
		}
	}
	return roleAttestations
}

// GetCartesianTestCase returns attestations for cartesian expansion testing
func (tf *TestFixtures) GetCartesianTestCase() []types.As {
	var cartesianAttestations []types.As
	for _, as := range tf.Attestations {
		// Multi-dimensional attestations (multiple subjects, predicates, or contexts)
		if len(as.Subjects) > 1 || len(as.Predicates) > 1 || len(as.Contexts) > 1 {
			cartesianAttestations = append(cartesianAttestations, as)
		}
	}
	return cartesianAttestations
}
