package server

import (
	"context"
	"database/sql"
	"time"

	"github.com/teranos/QNTX/internal/logger"
	errors "github.com/teranos/sacred-error"
)

// "WE NEED OUR OWN ABSTRACTION AND TRACKING OF THIS IN CASE WE DO HIT SENTRY LIMIT, SO WE NEVER REALLY LOST OBS"
//
// The ledger in internal/logger counts every error the node logs. This writes
// what changed in it to issues_seen. See docs/sentry.md.

const issuesKeptEvery = time.Minute

// keepIssues writes the ledger every issuesKeptEvery, and once more as the node stops.
func (s *QNTXServer) keepIssues() {
	if s.nodeDB == nil {
		return
	}
	log := s.logger.Named("issues")
	s.wg.Go("issues.kept", func() {
		ticker := time.NewTicker(issuesKeptEvery)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := writeIssues(ctx, s.nodeDB, logger.TakeIssues()); err != nil {
					log.Warnw("The issues the node counted were not written as it stopped", "error", err)
				}
				return
			case <-ticker.C:
				taken := logger.TakeIssues()
				if err := writeIssues(s.ctx, s.nodeDB, taken); err != nil {
					logger.ReturnIssues(taken)
					// Warn, not Error: an Error here is an issue that this write
					// would count, about the write that failed to count it.
					log.Warnw("The issues the node counted were not written to issues_seen",
						"issues", len(taken), "error", err)
				}
			}
		}
	})
}

// writeIssues upserts each issue's day. A row's counts are the ledger's own
// running totals for that day, so the newer row replaces the older.
func writeIssues(ctx context.Context, db *sql.DB, taken []logger.Issue) error {
	if len(taken) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrapf(err, "failed to begin writing %d issues to issues_seen", len(taken))
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO issues_seen (day, key, level, logger, caller, message, plugin, last_error, first_seen, last_seen, occurrences, raised)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (day, key) DO UPDATE SET
			last_error  = excluded.last_error,
			last_seen   = excluded.last_seen,
			occurrences = excluded.occurrences,
			raised      = excluded.raised`)
	if err != nil {
		return errors.Wrap(err, "failed to prepare the issues_seen upsert")
	}
	defer stmt.Close()

	for _, issue := range taken {
		if _, err := stmt.ExecContext(ctx,
			issue.Day, issue.Key, issue.Level, issue.Logger, issue.Caller, issue.Message, issue.Plugin, issue.LastError,
			issue.FirstSeen.UTC().Format(time.RFC3339Nano), issue.LastSeen.UTC().Format(time.RFC3339Nano),
			issue.Occurrences, issue.Raised,
		); err != nil {
			return errors.Wrapf(err, "failed to write issue %q of %s to issues_seen", issue.Message, issue.Day)
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.Wrapf(err, "failed to commit %d issues to issues_seen", len(taken))
	}
	return nil
}
