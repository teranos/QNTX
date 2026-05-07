package embeddings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

func TestEmitPulseDeferredNews_DedupSameFailurePicture(t *testing.T) {
	atsStore, db := qntxtest.CreateTestStore(t)
	logger := zap.NewNop().Sugar()

	// Insert some pulse execution data: 2 completed, 1 failed
	_, err := db.Exec(`INSERT INTO scheduled_pulse_jobs (id, ats_code, interval_seconds, handler_name, created_at)
		VALUES ('job1', 'recluster', 300, 'embeddings.recluster', datetime('now'))`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO pulse_executions (id, scheduled_job_id, status, started_at, completed_at, duration_ms, created_at, updated_at)
		VALUES ('ex1', 'job1', 'completed', datetime('now'), datetime('now'), 100, datetime('now'), datetime('now'))`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO pulse_executions (id, scheduled_job_id, status, started_at, completed_at, duration_ms, created_at, updated_at)
		VALUES ('ex2', 'job1', 'completed', datetime('now'), datetime('now'), 200, datetime('now'), datetime('now'))`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO pulse_executions (id, scheduled_job_id, status, started_at, completed_at, duration_ms, created_at, updated_at)
		VALUES ('ex3', 'job1', 'failed', datetime('now'), datetime('now'), 50, datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	projectCtx := "project:test/dedup"

	// First call — should emit
	EmitPulseDeferredNews(db, atsStore, projectCtx, "", nil, logger)

	// Second call — same failure picture, should NOT emit again
	EmitPulseDeferredNews(db, atsStore, projectCtx, "", nil, logger)

	// Count pulse-summary attestations
	results, err := atsStore.GetAttestations(ats.AttestationFilter{
		Predicates: []string{"deferred:pulse-summary"},
		Contexts:   []string{projectCtx},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, len(results), "same failure picture should emit only once")
}

func TestEmitPulseDeferredNews_EmitsOnNewFailure(t *testing.T) {
	atsStore, db := qntxtest.CreateTestStore(t)
	logger := zap.NewNop().Sugar()

	// Insert initial data: all completed
	_, err := db.Exec(`INSERT INTO scheduled_pulse_jobs (id, ats_code, interval_seconds, handler_name, created_at)
		VALUES ('job1', 'recluster', 300, 'embeddings.recluster', datetime('now'))`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO pulse_executions (id, scheduled_job_id, status, started_at, completed_at, duration_ms, created_at, updated_at)
		VALUES ('ex1', 'job1', 'completed', datetime('now'), datetime('now'), 100, datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	projectCtx := "project:test/dedup"

	// First call — no failures, emits success summary
	EmitPulseDeferredNews(db, atsStore, projectCtx, "", nil, logger)

	// Add a failure
	_, err = db.Exec(`INSERT INTO pulse_executions (id, scheduled_job_id, status, started_at, completed_at, duration_ms, created_at, updated_at)
		VALUES ('ex2', 'job1', 'failed', datetime('now'), datetime('now'), 50, datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	// Second call — failure picture changed, should emit again
	EmitPulseDeferredNews(db, atsStore, projectCtx, "", nil, logger)

	results, err := atsStore.GetAttestations(ats.AttestationFilter{
		Predicates: []string{"deferred:pulse-summary"},
		Contexts:   []string{projectCtx},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, len(results), "different failure picture should emit a new attestation")
}
