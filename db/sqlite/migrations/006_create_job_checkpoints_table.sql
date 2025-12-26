-- GRACE Phase 3: Job checkpoint/resume support
-- Stores fine-grained progress for long-running jobs to enable resume after cancellation
--
-- Design decisions (from docs/development/graceful-shutdown-redesign.md):
-- - Fine-grained checkpointing: After each major stage (read_jd, extract, score, attestations)
-- - Low re-work tolerance: Prioritize preserving work over minimizing DB writes
-- - Stage-based intervals: Checkpoint every 1-2 seconds during long operations
--
-- Checkpoint stages for JD extraction:
--   read_jd → extract → generate_attestations → persist_data → score_candidates
--
-- Progress JSON schema (stage-specific):
-- {
--   "candidates_scored": 15,
--   "total_candidates": 47,
--   "last_candidate_id": "CAND12345",
--   "extracted_data": {...}  // Serialized intermediate results
-- }

CREATE TABLE IF NOT EXISTS job_checkpoints (
    job_id TEXT PRIMARY KEY,
    stage TEXT NOT NULL,                    -- Current stage: 'read_jd', 'extract', 'score_candidates', etc.
    progress TEXT,                          -- JSON blob with stage-specific progress data
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (job_id) REFERENCES async_ix_jobs(id) ON DELETE CASCADE
);

-- Index for efficient checkpoint lookups during worker startup
CREATE INDEX IF NOT EXISTS idx_job_checkpoints_job_id ON job_checkpoints(job_id);

-- Index for debugging/monitoring: find all checkpoints at a given stage
CREATE INDEX IF NOT EXISTS idx_job_checkpoints_stage ON job_checkpoints(stage);
