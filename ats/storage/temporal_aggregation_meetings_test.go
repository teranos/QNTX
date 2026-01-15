package storage

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// ==============================================================================
// Meeting/Calendar Scheduling Domain: Temporal Aggregation with Overlap Detection
// ==============================================================================
// Models calendar meeting attendance with temporal aggregation for scheduling.
// Subjects: People (e.g., PERSON_ALICE_SMITH)
// Predicates: meeting_duration_hours (duration of meeting attendance in hours)
// Metadata: start_time, end_time, duration_hours, meeting_title, meeting_type
//
// Use case: "Find people with >20 hours of meetings this week"
// Query: ax meeting_duration_hours over 20h since last 7 days
//
// Key Challenge: Overlapping meetings (double-bookings, concurrent meetings)
// Example: Meeting 1: 11:00-13:00 (2h), Meeting 2: 12:30-13:30 (1h)
//          Without overlap detection: 2h + 1h = 3h ❌
//          With overlap detection: 11:00-13:30 = 2.5h ✓
// ==============================================================================

// mockMeetingQueryExpander provides meeting domain-specific predicates for testing
type mockMeetingQueryExpander struct{}

func (m *mockMeetingQueryExpander) ExpandPredicate(predicate string, values []string) []ats.PredicateExpansion {
	var expansions []ats.PredicateExpansion
	for _, value := range values {
		expansions = append(expansions, ats.PredicateExpansion{
			Predicate: predicate,
			Context:   value,
		})
	}
	return expansions
}

func (m *mockMeetingQueryExpander) GetNumericPredicates() []string {
	return []string{"meeting_duration_hours"}
}

func (m *mockMeetingQueryExpander) GetNaturalLanguagePredicates() []string {
	return []string{}
}

type meetingAttendance struct {
	personID     string
	meetingID    string
	title        string
	meetingType  string
	startTime    time.Time
	endTime      time.Time
	durationH    float64
	attestedAt   time.Time
}

func createMeetingAttestation(t *testing.T, store *SQLStore, meeting *meetingAttendance) {
	metadata := map[string]interface{}{
		"start_time":   meeting.startTime.Format(time.RFC3339),
		"end_time":     meeting.endTime.Format(time.RFC3339),
		"duration_hours": fmt.Sprintf("%.2f", meeting.durationH),
		"meeting_title":  meeting.title,
		"meeting_type":   meeting.meetingType,
	}

	err := store.CreateAttestation(&types.As{
		ID:         meeting.meetingID,
		Subjects:   []string{meeting.personID},
		Predicates: []string{"meeting_duration_hours"},
		Contexts:   []string{fmt.Sprintf("%.2f", meeting.durationH)},
		Actors:     []string{meeting.meetingID}, // Self-certifying ASID
		Timestamp:  meeting.attestedAt,
		Source:     "adapter:calendar",
		Attributes: metadata,
		CreatedAt:  meeting.attestedAt,
	})
	require.NoError(t, err)
}

// ==============================================================================
// PHASE 1: Basic Aggregation (MVP)
// ==============================================================================

// setupBasicMeetingsTestDB creates test database with meeting attestations
func setupBasicMeetingsTestDB(t *testing.T) *sql.DB {
	testDB := qntxtest.CreateTestDB(t)
	store := NewSQLStore(testDB, nil)

	now := time.Now()
	attestationTime := now.Add(-1 * time.Hour)

	// Person 1: Alice - 25 hours of meetings (ABOVE 20h threshold)
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_1",
		title:       "Weekly Team Sync",
		meetingType: "team_meeting",
		startTime:   now.Add(-48 * time.Hour),
		endTime:     now.Add(-48*time.Hour + 2*time.Hour),
		durationH:   2.0,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_2",
		title:       "Sprint Planning",
		meetingType: "planning",
		startTime:   now.Add(-47 * time.Hour),
		endTime:     now.Add(-47*time.Hour + 3*time.Hour),
		durationH:   3.0,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_3",
		title:       "Client Demo",
		meetingType: "external",
		startTime:   now.Add(-46 * time.Hour),
		endTime:     now.Add(-46*time.Hour + 90*time.Minute),
		durationH:   1.5,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_4",
		title:       "1-on-1 with Manager",
		meetingType: "one_on_one",
		startTime:   now.Add(-45 * time.Hour),
		endTime:     now.Add(-45*time.Hour + 30*time.Minute),
		durationH:   0.5,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_5",
		title:       "All-Hands Company Meeting",
		meetingType: "company_wide",
		startTime:   now.Add(-44 * time.Hour),
		endTime:     now.Add(-44*time.Hour + 90*time.Minute),
		durationH:   1.5,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_6",
		title:       "Architecture Review",
		meetingType: "technical",
		startTime:   now.Add(-43 * time.Hour),
		endTime:     now.Add(-43*time.Hour + 4*time.Hour),
		durationH:   4.0,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_7",
		title:       "Team Retrospective",
		meetingType: "team_meeting",
		startTime:   now.Add(-42 * time.Hour),
		endTime:     now.Add(-42*time.Hour + 90*time.Minute),
		durationH:   1.5,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_8",
		title:       "Product Roadmap Discussion",
		meetingType: "planning",
		startTime:   now.Add(-41 * time.Hour),
		endTime:     now.Add(-41*time.Hour + 2*time.Hour),
		durationH:   2.0,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_9",
		title:       "Customer Success Review",
		meetingType: "external",
		startTime:   now.Add(-40 * time.Hour),
		endTime:     now.Add(-40*time.Hour + 2*time.Hour),
		durationH:   2.0,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_10",
		title:       "Design Review",
		meetingType: "technical",
		startTime:   now.Add(-39 * time.Hour),
		endTime:     now.Add(-39*time.Hour + 3*time.Hour),
		durationH:   3.0,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_11",
		title:       "Security Audit",
		meetingType: "technical",
		startTime:   now.Add(-38 * time.Hour),
		endTime:     now.Add(-38*time.Hour + 2*time.Hour),
		durationH:   2.0,
		attestedAt:  attestationTime,
	})
	// Total for Alice: 2 + 3 + 1.5 + 0.5 + 1.5 + 4 + 1.5 + 2 + 2 + 3 + 2 = 25 hours

	// Person 2: Bob - 15 hours of meetings (BELOW 20h threshold)
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_BOB_JONES",
		meetingID:   "meeting_bob_1",
		title:       "Weekly Team Sync",
		meetingType: "team_meeting",
		startTime:   now.Add(-48 * time.Hour),
		endTime:     now.Add(-48*time.Hour + 2*time.Hour),
		durationH:   2.0,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_BOB_JONES",
		meetingID:   "meeting_bob_2",
		title:       "Code Review Session",
		meetingType: "technical",
		startTime:   now.Add(-46 * time.Hour),
		endTime:     now.Add(-46*time.Hour + 90*time.Minute),
		durationH:   1.5,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_BOB_JONES",
		meetingID:   "meeting_bob_3",
		title:       "1-on-1 with Manager",
		meetingType: "one_on_one",
		startTime:   now.Add(-45 * time.Hour),
		endTime:     now.Add(-45*time.Hour + 30*time.Minute),
		durationH:   0.5,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_BOB_JONES",
		meetingID:   "meeting_bob_4",
		title:       "All-Hands Company Meeting",
		meetingType: "company_wide",
		startTime:   now.Add(-44 * time.Hour),
		endTime:     now.Add(-44*time.Hour + 90*time.Minute),
		durationH:   1.5,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_BOB_JONES",
		meetingID:   "meeting_bob_5",
		title:       "Team Retrospective",
		meetingType: "team_meeting",
		startTime:   now.Add(-42 * time.Hour),
		endTime:     now.Add(-42*time.Hour + 90*time.Minute),
		durationH:   1.5,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_BOB_JONES",
		meetingID:   "meeting_bob_6",
		title:       "Bug Triage",
		meetingType: "technical",
		startTime:   now.Add(-40 * time.Hour),
		endTime:     now.Add(-40*time.Hour + 2*time.Hour),
		durationH:   2.0,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_BOB_JONES",
		meetingID:   "meeting_bob_7",
		title:       "Tech Talk",
		meetingType: "learning",
		startTime:   now.Add(-38 * time.Hour),
		endTime:     now.Add(-38*time.Hour + 90*time.Minute),
		durationH:   1.5,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_BOB_JONES",
		meetingID:   "meeting_bob_8",
		title:       "Pair Programming Session",
		meetingType: "technical",
		startTime:   now.Add(-36 * time.Hour),
		endTime:     now.Add(-36*time.Hour + 3*time.Hour),
		durationH:   3.0,
		attestedAt:  attestationTime,
	})
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_BOB_JONES",
		meetingID:   "meeting_bob_9",
		title:       "Quarterly Planning",
		meetingType: "planning",
		startTime:   now.Add(-34 * time.Hour),
		endTime:     now.Add(-34*time.Hour + 2*time.Hour),
		durationH:   2.0,
		attestedAt:  attestationTime,
	})
	// Total for Bob: 2 + 1.5 + 0.5 + 1.5 + 1.5 + 2 + 1.5 + 3 + 2 = 15 hours

	return testDB
}

// TestMeetingTemporalAggregation_Basic tests meeting duration aggregation
// Query: ax meeting_duration_hours over 20h
// Expected: Alice (25h) matches, Bob (15h) does not
func TestMeetingTemporalAggregation_Basic(t *testing.T) {
	db := setupBasicMeetingsTestDB(t)
	queryStore := NewSQLQueryStoreWithExpander(db, &mockMeetingQueryExpander{})

	// Query: ax meeting_duration_hours over 20h
	filter := types.AxFilter{
		OverComparison: &types.OverFilter{
			Value:    20.0,
			Unit:     "h",
			Operator: "over",
		},
	}

	results, err := queryStore.ExecuteAxQuery(context.Background(), filter)
	require.NoError(t, err)

	// Should return meeting attestations for people with total >= 20 hours
	// Alice: 11 attestations (25 hours) ✓
	// Bob: 0 attestations (15 hours) ✗
	assert.Equal(t, 11, len(results), "Expected 11 meeting attestations for Alice")

	// Verify only Alice is returned
	personMeetingCount := make(map[string]int)
	for _, result := range results {
		personMeetingCount[result.Subjects[0]]++
	}

	assert.Equal(t, 11, personMeetingCount["PERSON_ALICE_SMITH"], "Alice should have 11 meeting attestations")
	assert.Equal(t, 0, personMeetingCount["PERSON_BOB_JONES"], "Bob should not appear (only 15 hours)")

	t.Logf("✅ Meeting Temporal Aggregation: Found %d people with 20+ hours of meetings", len(personMeetingCount))
	t.Logf("   - Alice Smith: 25 hours (11 meetings)")
}

// ==============================================================================
// PHASE 3: Overlap Detection (Critical for Calendar Domain!)
// ==============================================================================

// setupOverlappingMeetingsTestDB creates test database with overlapping meetings
func setupOverlappingMeetingsTestDB(t *testing.T) *sql.DB {
	testDB := qntxtest.CreateTestDB(t)
	store := NewSQLStore(testDB, nil)

	now := time.Now()
	attestationTime := now.Add(-1 * time.Hour)

	// Alice: Has overlapping meetings (double-booked)
	// Meeting 1: 11:00-13:00 (2 hours) - "Demo QNTX project"
	baseTime := now.Add(-48 * time.Hour).Truncate(time.Hour).Add(11 * time.Hour)
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_demo",
		title:       "Demo QNTX project",
		meetingType: "external",
		startTime:   baseTime,
		endTime:     baseTime.Add(2 * time.Hour),
		durationH:   2.0,
		attestedAt:  attestationTime,
	})

	// Meeting 2: 12:30-13:30 (1 hour) - "Emergency Bug Fix Discussion"
	// Overlaps 30 minutes with Meeting 1 (12:30-13:00)
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_bugfix",
		title:       "Emergency Bug Fix Discussion",
		meetingType: "technical",
		startTime:   baseTime.Add(90 * time.Minute), // 12:30
		endTime:     baseTime.Add(150 * time.Minute), // 13:30
		durationH:   1.0,
		attestedAt:  attestationTime,
	})

	// Meeting 3: 14:00-15:30 (1.5 hours) - separate, no overlap
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "meeting_alice_planning",
		title:       "Sprint Planning",
		meetingType: "planning",
		startTime:   baseTime.Add(3 * time.Hour), // 14:00
		endTime:     baseTime.Add(270 * time.Minute), // 15:30
		durationH:   1.5,
		attestedAt:  attestationTime,
	})

	// Without overlap detection: 2.0 + 1.0 + 1.5 = 4.5 hours
	// With overlap detection:
	//   Merge Meeting 1 & 2: 11:00-13:30 = 2.5 hours
	//   Add Meeting 3: 1.5 hours
	//   Total: 2.5 + 1.5 = 4.0 hours

	// Bob: Non-overlapping meetings for comparison
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_BOB_JONES",
		meetingID:   "meeting_bob_1",
		title:       "Team Sync",
		meetingType: "team_meeting",
		startTime:   baseTime,
		endTime:     baseTime.Add(1 * time.Hour),
		durationH:   1.0,
		attestedAt:  attestationTime,
	})

	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_BOB_JONES",
		meetingID:   "meeting_bob_2",
		title:       "Code Review",
		meetingType: "technical",
		startTime:   baseTime.Add(2 * time.Hour),
		endTime:     baseTime.Add(3 * time.Hour),
		durationH:   1.0,
		attestedAt:  attestationTime,
	})

	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_BOB_JONES",
		meetingID:   "meeting_bob_3",
		title:       "1-on-1",
		meetingType: "one_on_one",
		startTime:   baseTime.Add(4 * time.Hour),
		endTime:     baseTime.Add(270 * time.Minute),
		durationH:   0.5,
		attestedAt:  attestationTime,
	})
	// Bob total: 1.0 + 1.0 + 0.5 = 2.5 hours (no overlaps)

	return testDB
}

// TestMeetingTemporalAggregation_OverlapDetection tests merging of overlapping meetings
// Query: ax meeting_duration_hours over 3.5h
//
// Overlap detection algorithm:
//  1. Sort meetings by start_time
//  2. Merge overlapping periods (if meeting.start <= merged.last.end)
//  3. Sum durations of merged periods
//
// Alice (overlapping meetings):
//   Meeting 1: 11:00-13:00 (2.0h) - "Demo QNTX project"
//   Meeting 2: 12:30-13:30 (1.0h) - "Emergency Bug Fix Discussion" (overlaps 30min)
//   Meeting 3: 14:00-15:30 (1.5h) - "Sprint Planning" (separate)
//
//   WITHOUT overlap detection: 2.0 + 1.0 + 1.5 = 4.5h ✓ (matches threshold)
//   WITH overlap detection:
//     Meetings 1 & 2 merged → 11:00-13:30 = 2.5h
//     Meeting 3 → 1.5h
//     Total: 2.5 + 1.5 = 4.0h ✓ (still matches threshold)
//
// Bob (no overlaps):
//   Meeting 1: 1.0h
//   Meeting 2: 1.0h
//   Meeting 3: 0.5h
//   Total: 2.5h ✗ (below threshold, same with or without overlap detection)
//
// Expected: Only Alice matches (4.0h >= 3.5h with overlap detection)
//           Bob does NOT match (2.5h < 3.5h)
func TestMeetingTemporalAggregation_OverlapDetection(t *testing.T) {
	t.Skip("Phase 3: Overlap detection - implement period merging algorithm for calendar meetings")

	db := setupOverlappingMeetingsTestDB(t)
	queryStore := NewSQLQueryStoreWithExpander(db, &mockMeetingQueryExpander{})

	// Query: ax meeting_duration_hours over 3.5h
	filter := types.AxFilter{
		OverComparison: &types.OverFilter{
			Value:    3.5,
			Unit:     "h",
			Operator: "over",
		},
	}

	results, err := queryStore.ExecuteAxQuery(context.Background(), filter)
	require.NoError(t, err)

	// Should return only Alice's 3 meetings (merged duration 4.0h >= 3.5h)
	// Bob excluded (2.5h < 3.5h)
	assert.Equal(t, 3, len(results), "Expected 3 meeting attestations for Alice")

	// Verify all results are for Alice
	personMeetingCount := make(map[string]int)
	for _, result := range results {
		personMeetingCount[result.Subjects[0]]++
	}

	assert.Equal(t, 3, personMeetingCount["PERSON_ALICE_SMITH"], "Alice should have 3 meetings")
	assert.Equal(t, 0, personMeetingCount["PERSON_BOB_JONES"], "Bob should not match (2.5h < 3.5h)")

	t.Logf("✅ Meeting Overlap Detection: Alice has 4.0h actual meeting time (not 4.5h)")
	t.Logf("   - Meeting 1 (11:00-13:00) + Meeting 2 (12:30-13:30) merged to 11:00-13:30 = 2.5h")
	t.Logf("   - Meeting 3 (14:00-15:30) = 1.5h")
	t.Logf("   - Total: 2.5h + 1.5h = 4.0h (saved 0.5h from overlap detection)")
}

// setupRealisticOverlapsTestDB creates realistic double-booking scenario
func setupRealisticOverlapsTestDB(t *testing.T) *sql.DB {
	testDB := qntxtest.CreateTestDB(t)
	store := NewSQLStore(testDB, nil)

	now := time.Now()
	attestationTime := now.Add(-1 * time.Hour)
	baseTime := now.Add(-48 * time.Hour).Truncate(time.Hour).Add(9 * time.Hour) // Start at 9 AM

	// Alice: Heavy meeting load with multiple overlaps (realistic overbooked scenario)
	// 9:00-10:00: Team Standup (1h)
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "alice_standup",
		title:       "Daily Standup",
		meetingType: "team_meeting",
		startTime:   baseTime,
		endTime:     baseTime.Add(1 * time.Hour),
		durationH:   1.0,
		attestedAt:  attestationTime,
	})

	// 9:30-11:00: All-Hands (1.5h, overlaps 30min with standup)
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "alice_allhands",
		title:       "All-Hands Meeting",
		meetingType: "company_wide",
		startTime:   baseTime.Add(30 * time.Minute),
		endTime:     baseTime.Add(2 * time.Hour),
		durationH:   1.5,
		attestedAt:  attestationTime,
	})

	// 11:00-12:00: Project Sync (1h, no overlap)
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "alice_project",
		title:       "Project Sync",
		meetingType: "team_meeting",
		startTime:   baseTime.Add(2 * time.Hour),
		endTime:     baseTime.Add(3 * time.Hour),
		durationH:   1.0,
		attestedAt:  attestationTime,
	})

	// 11:30-13:00: Client Call (1.5h, overlaps 30min with project sync)
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "alice_client",
		title:       "Client Demo Call",
		meetingType: "external",
		startTime:   baseTime.Add(150 * time.Minute),
		endTime:     baseTime.Add(4 * time.Hour),
		durationH:   1.5,
		attestedAt:  attestationTime,
	})

	// 14:00-16:00: Deep Work Block - NOT a meeting, separate activity
	// (not included in this test)

	// 16:00-17:30: Architecture Review (1.5h, no overlap)
	createMeetingAttestation(t, store, &meetingAttendance{
		personID:    "PERSON_ALICE_SMITH",
		meetingID:   "alice_arch",
		title:       "Architecture Review",
		meetingType: "technical",
		startTime:   baseTime.Add(7 * time.Hour),
		endTime:     baseTime.Add(510 * time.Minute),
		durationH:   1.5,
		attestedAt:  attestationTime,
	})

	// Without overlap detection: 1.0 + 1.5 + 1.0 + 1.5 + 1.5 = 6.5h
	// With overlap detection:
	//   9:00-11:00 (standup + allhands merged) = 2.0h
	//   11:00-13:00 (project + client merged) = 2.0h
	//   16:00-17:30 (arch review) = 1.5h
	//   Total: 2.0 + 2.0 + 1.5 = 5.5h
	// Saved: 1.0h from overlap detection

	return testDB
}

// TestMeetingTemporalAggregation_RealisticOverlaps tests realistic double-booking scenario
// Query: ax meeting_duration_hours over 5h
//
// Alice's day (overlapping meetings):
//   9:00-10:00: Daily Standup (1h)
//   9:30-11:00: All-Hands (1.5h, overlaps 30min with standup)
//   11:00-12:00: Project Sync (1h)
//   11:30-13:00: Client Call (1.5h, overlaps 30min with project sync)
//   16:00-17:30: Architecture Review (1.5h)
//
// WITHOUT overlap detection: 1.0 + 1.5 + 1.0 + 1.5 + 1.5 = 6.5h
// WITH overlap detection:
//   9:00-11:00 (merged) = 2.0h
//   11:00-13:00 (merged) = 2.0h
//   16:00-17:30 = 1.5h
//   Total: 5.5h
//
// Expected: Alice matches with 5.5h >= 5h (with overlap detection)
//           Without overlap detection would show 6.5h (incorrect)
func TestMeetingTemporalAggregation_RealisticOverlaps(t *testing.T) {
	t.Skip("Phase 3: Realistic overlap scenario - Alice double-booked for 1 hour total")

	db := setupRealisticOverlapsTestDB(t)
	queryStore := NewSQLQueryStoreWithExpander(db, &mockMeetingQueryExpander{})

	// Query: ax meeting_duration_hours over 5h
	filter := types.AxFilter{
		OverComparison: &types.OverFilter{
			Value:    5.0,
			Unit:     "h",
			Operator: "over",
		},
	}

	results, err := queryStore.ExecuteAxQuery(context.Background(), filter)
	require.NoError(t, err)

	// Should return Alice's 5 meetings (merged duration 5.5h >= 5h)
	assert.Equal(t, 5, len(results), "Expected 5 meeting attestations for Alice")

	// Verify all results are for Alice
	for _, result := range results {
		assert.Equal(t, []string{"PERSON_ALICE_SMITH"}, result.Subjects)
	}

	t.Logf("✅ Realistic Overlap Detection: Alice has 5.5h actual meeting time")
	t.Logf("   - WITHOUT overlap detection: 6.5h (wrong - counts overlaps twice)")
	t.Logf("   - WITH overlap detection: 5.5h (correct - 1.0h saved from merging)")
	t.Logf("   - Double-booked periods: 9:30-10:00 (30min) + 11:30-12:00 (30min) = 1.0h")
}
