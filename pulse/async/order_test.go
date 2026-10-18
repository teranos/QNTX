package async

import (
	"strings"
	"testing"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// On q, Pulse asked for the newest completed jobs among 132078, and the node sorted every one of
// them to give fifty: 7s with a connection held, the store stalled, and the caller was turned away.
func TestTheNewestJobsInAStatusAreReadInOrderNotSorted(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT `+StandardJobSelectColumns()+` `+newestInStatus, JobStatusCompleted, 50)
	if err != nil {
		t.Fatalf("the plan for ListJobs could not be read: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatalf("a step of the plan for ListJobs could not be read: %v", err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("the plan for ListJobs stopped: %v", err)
	}
	for _, step := range plan {
		if strings.Contains(step, "TEMP B-TREE") {
			t.Fatalf("ListJobs sorts what it reads instead of reading it in order: %s", strings.Join(plan, "; "))
		}
	}
}
