package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

// insertTestAttestation inserts an attestation with RFC3339-formatted timestamp
// matching production format. The sqlTestStore uses Go's time.Time serialization
// which doesn't sort correctly against RFC3339 in SQLite string comparisons.
func insertTestAttestation(t *testing.T, db *sql.DB, id string, subjects, predicates []string, actor, ctx string, ts time.Time, source string, attrs map[string]interface{}) {
	t.Helper()
	subJSON, _ := json.Marshal(subjects)
	predJSON, _ := json.Marshal(predicates)
	actJSON, _ := json.Marshal([]string{actor})
	ctxJSON, _ := json.Marshal([]string{ctx})
	attrJSON := []byte("{}")
	if attrs != nil {
		attrJSON, _ = json.Marshal(attrs)
	}
	_, err := db.Exec(
		`INSERT INTO attestations (id, subjects, predicates, actors, contexts, timestamp, source, attributes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, string(subJSON), string(predJSON), string(actJSON), string(ctxJSON),
		ts.UTC().Format(time.RFC3339), source, string(attrJSON),
	)
	require.NoError(t, err, "Failed to insert test attestation %s", id)
}

func TestDistillHandler_BasicCycle(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	logger := zap.NewNop().Sugar()

	h := &distillHandler{
		db:        db,
		atsStore:  store,
		maxAge:    1 * time.Hour,
		batchSize: 500,
		logger:    logger,
	}

	oldTime := time.Now().UTC().Add(-2 * time.Hour)
	for i := 0; i < 10; i++ {
		insertTestAttestation(t, db,
			fmt.Sprintf("OLD_%d", i),
			[]string{fmt.Sprintf("subject_%d", i)},
			[]string{"crawl-stage-changed"},
			"levi", "reticulum",
			oldTime.Add(time.Duration(i)*time.Minute),
			"test",
			map[string]interface{}{"stage": "connecting", "elapsed_ms": float64(i * 100)},
		)
	}

	recentTime := time.Now().UTC().Add(1 * time.Hour)
	for i := 0; i < 3; i++ {
		insertTestAttestation(t, db,
			fmt.Sprintf("RECENT_%d", i),
			[]string{"recent_subject"},
			[]string{"crawl-stage-changed"},
			"levi", "reticulum",
			recentTime.Add(time.Duration(i)*time.Minute),
			"test", nil,
		)
	}

	err := h.Execute(context.Background(), &async.Job{})
	require.NoError(t, err)

	// Original old attestations should be deleted
	for i := 0; i < 10; i++ {
		assert.False(t, store.AttestationExists(fmt.Sprintf("OLD_%d", i)),
			"Old attestation %d should be deleted", i)
	}

	// Recent attestations should survive
	for i := 0; i < 3; i++ {
		assert.True(t, store.AttestationExists(fmt.Sprintf("RECENT_%d", i)),
			"Recent attestation %d should survive", i)
	}

	// One distill attestation should exist
	var distillCount int
	db.QueryRow("SELECT COUNT(*) FROM attestations WHERE source = 'distill'").Scan(&distillCount)
	assert.Equal(t, 1, distillCount, "Should create exactly 1 distill attestation for 1 predicate group")

	// Verify distill attestation metadata
	var attrsJSON string
	db.QueryRow("SELECT attributes FROM attestations WHERE source = 'distill'").Scan(&attrsJSON)

	var attrs map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(attrsJSON), &attrs))

	assert.Equal(t, true, attrs["_distill"])
	assert.Equal(t, float64(10), attrs["_count"])
	assert.Equal(t, float64(10), attrs["_total"])  // 10 raw attestations, no prior distills
	assert.Equal(t, float64(10), attrs["_subjects_count"])
	assert.NotEmpty(t, attrs["_first_seen"])
	assert.NotEmpty(t, attrs["_last_seen"])
	assert.NotEmpty(t, attrs["_version"])
	assert.NotEmpty(t, attrs["_rust_version"])

	// Verify attribute merging: elapsed_ms should be aggregated
	elapsed, ok := attrs["elapsed_ms"].(map[string]interface{})
	require.True(t, ok, "elapsed_ms should be a numeric aggregate")
	assert.Equal(t, float64(0), elapsed["min"])
	assert.Equal(t, float64(900), elapsed["max"])
	assert.Equal(t, float64(10), elapsed["count"])
}

func TestDistillHandler_MultiplePredicateGroups(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	logger := zap.NewNop().Sugar()

	h := &distillHandler{
		db:        db,
		atsStore:  store,
		maxAge:    1 * time.Hour,
		batchSize: 500,
		logger:    logger,
	}

	oldTime := time.Now().UTC().Add(-2 * time.Hour)

	for i := 0; i < 5; i++ {
		insertTestAttestation(t, db,
			fmt.Sprintf("ANN_%d", i),
			[]string{fmt.Sprintf("node_%d", i)},
			[]string{"announced"},
			"levi", "reticulum",
			oldTime.Add(time.Duration(i)*time.Minute),
			"test", nil,
		)
	}

	for i := 0; i < 3; i++ {
		insertTestAttestation(t, db,
			fmt.Sprintf("PATH_%d", i),
			[]string{fmt.Sprintf("path_%d", i)},
			[]string{"path-found"},
			"levi", "reticulum",
			oldTime.Add(time.Duration(i)*time.Minute),
			"test", nil,
		)
	}

	err := h.Execute(context.Background(), &async.Job{})
	require.NoError(t, err)

	var distillCount int
	db.QueryRow("SELECT COUNT(*) FROM attestations WHERE source = 'distill'").Scan(&distillCount)
	assert.Equal(t, 2, distillCount, "Should create 2 distill attestations (one per predicate)")

	var remaining int
	db.QueryRow("SELECT COUNT(*) FROM attestations WHERE source = 'test'").Scan(&remaining)
	assert.Equal(t, 0, remaining, "All originals should be deleted")
}

func TestDistillHandler_MetaDistillation(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	logger := zap.NewNop().Sugar()

	h := &distillHandler{
		db:        db,
		atsStore:  store,
		maxAge:    1 * time.Hour,
		batchSize: 500,
		logger:    logger,
	}

	oldTime := time.Now().UTC().Add(-2 * time.Hour)

	// Insert an old distill attestation (from a previous distill cycle)
	insertTestAttestation(t, db,
		"AS-distill-old-1",
		[]string{"distill:announced"},
		[]string{"distill:announced"},
		"levi", "reticulum",
		oldTime, "distill",
		map[string]interface{}{
			"_distill":        true,
			"_count":          float64(5),
			"_first_seen":     oldTime.Add(-1 * time.Hour).UTC().Format(time.RFC3339),
			"_last_seen":      oldTime.UTC().Format(time.RFC3339),
			"_subjects_count": float64(5),
		},
	)

	// Insert some regular old attestations with same predicate
	for i := 0; i < 3; i++ {
		insertTestAttestation(t, db,
			fmt.Sprintf("REG_%d", i),
			[]string{fmt.Sprintf("node_%d", i)},
			[]string{"distill:announced"},
			"levi", "reticulum",
			oldTime.Add(time.Duration(i)*time.Minute),
			"test", nil,
		)
	}

	err := h.Execute(context.Background(), &async.Job{})
	require.NoError(t, err)

	// Old distill attestation should be gone (meta-distilled)
	assert.False(t, store.AttestationExists("AS-distill-old-1"))

	// Regular attestations should be gone
	for i := 0; i < 3; i++ {
		assert.False(t, store.AttestationExists(fmt.Sprintf("REG_%d", i)))
	}

	// One new distill attestation should exist
	var attrsJSON string
	db.QueryRow("SELECT attributes FROM attestations WHERE source = 'distill'").Scan(&attrsJSON)

	var attrs map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(attrsJSON), &attrs))
	assert.Equal(t, float64(4), attrs["_count"])  // batch size: 1 old distill + 3 regular
	assert.Equal(t, float64(8), attrs["_total"])   // transitive: 5 (from old distill _count) + 3 regular

	// Predicate should not stack distill: prefix
	var predJSON string
	db.QueryRow("SELECT predicates FROM attestations WHERE source = 'distill'").Scan(&predJSON)
	assert.Contains(t, predJSON, "distill:announced")
	assert.NotContains(t, predJSON, "distill:distill:")
}

func TestDistillHandler_DryRun(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	logger := zap.NewNop().Sugar()

	h := &distillHandler{
		db:        db,
		atsStore:  store,
		maxAge:    1 * time.Hour,
		batchSize: 500,
		dryRun:    true,
		logger:    logger,
	}

	oldTime := time.Now().UTC().Add(-2 * time.Hour)
	for i := 0; i < 5; i++ {
		insertTestAttestation(t, db,
			fmt.Sprintf("DRY_%d", i),
			[]string{"sub"},
			[]string{"test-pred"},
			"actor", "ctx",
			oldTime.Add(time.Duration(i)*time.Minute),
			"test", nil,
		)
	}

	err := h.Execute(context.Background(), &async.Job{})
	require.NoError(t, err)

	var count int
	db.QueryRow("SELECT COUNT(*) FROM attestations").Scan(&count)
	assert.Equal(t, 5, count, "Dry run should not delete anything")

	var distillCount int
	db.QueryRow("SELECT COUNT(*) FROM attestations WHERE source = 'distill'").Scan(&distillCount)
	assert.Equal(t, 0, distillCount)
}

func TestDistillHandler_GhostRowCleanup(t *testing.T) {
	_, db := qntxtest.CreateTestStore(t)
	logger := zap.NewNop().Sugar()

	db.Exec("INSERT INTO attestations (id, subjects, predicates, actors, contexts, timestamp, source, attributes) VALUES (NULL, '[]', '[]', '[]', '[]', '0', 'ghost', '{}')")
	db.Exec("INSERT INTO attestations (id, subjects, predicates, actors, contexts, timestamp, source, attributes) VALUES ('', '[]', '[]', '[]', '[]', '0001-01-01', 'ghost', '{}')")

	var ghostCount int
	db.QueryRow("SELECT COUNT(*) FROM attestations WHERE id IS NULL OR id = '' OR timestamp < '0002'").Scan(&ghostCount)
	require.Equal(t, 2, ghostCount, "Ghost rows should exist before cleanup")

	h := &distillHandler{
		db:        db,
		atsStore:  nil,
		maxAge:    1 * time.Hour,
		batchSize: 500,
		logger:    logger,
	}

	err := h.Execute(context.Background(), &async.Job{})
	require.NoError(t, err)

	db.QueryRow("SELECT COUNT(*) FROM attestations WHERE id IS NULL OR id = '' OR timestamp < '0002'").Scan(&ghostCount)
	assert.Equal(t, 0, ghostCount, "Ghost rows should be cleaned up")
}
