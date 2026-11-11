package server

import (
	"context"
	"testing"
	"time"

	"github.com/teranos/QNTX/internal/logger"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// 「[Opus 5.5] A row is an issue's day, and the newer counts replace the older.」
func TestIssuesAreWrittenToIssuesSeen(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 11, 0, 25, 3, 0, time.UTC)

	issue := logger.Issue{
		Day: "2026-11-11", Key: "k", Level: "error", Logger: "server.forward",
		Message:   "A delivery did not reach the node it is forwarded to",
		LastError: "dial tcp 10.88.0.2:8770: i/o timeout",
		FirstSeen: at, LastSeen: at, Occurrences: 1, Raised: 1,
	}
	if err := writeIssues(ctx, db, []logger.Issue{issue}); err != nil {
		t.Fatalf("writeIssues returned %v", err)
	}
	issue.Occurrences, issue.Raised, issue.LastSeen = 3103, 6, at.Add(19*time.Hour)
	if err := writeIssues(ctx, db, []logger.Issue{issue}); err != nil {
		t.Fatalf("writeIssues returned %v", err)
	}

	var rows, occurrences, raised int
	var firstSeen, lastSeen string
	if err := db.QueryRow(`SELECT COUNT(*), MAX(occurrences), MAX(raised), MAX(first_seen), MAX(last_seen) FROM issues_seen`).
		Scan(&rows, &occurrences, &raised, &firstSeen, &lastSeen); err != nil {
		t.Fatalf("reading issues_seen: %v", err)
	}
	if rows != 1 || occurrences != 3103 || raised != 6 {
		t.Errorf("issues_seen holds rows=%d occurrences=%d raised=%d, want 1, 3103, 6", rows, occurrences, raised)
	}
	if firstSeen != "2026-11-11T00:25:03Z" || lastSeen != "2026-11-11T19:25:03Z" {
		t.Errorf("first_seen=%s last_seen=%s", firstSeen, lastSeen)
	}
}
