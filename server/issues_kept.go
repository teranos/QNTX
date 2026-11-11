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

// keepIssues takes in what is kept of today, then writes the ledger every
// issuesKeptEvery. The last write is flushIssues', at the end of Stop, after
// the last thing the node logs as it stops.
func (s *QNTXServer) keepIssues() {
	if s.nodeDB == nil {
		return
	}
	log := s.logger.Named("issues")
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	kept, err := readIssues(ctx, s.nodeDB, time.Now().UTC().Format(time.DateOnly))
	cancel()
	if err != nil {
		log.Warnw("What issues_seen holds of today was not read; today's counts start again", "error", err)
	}
	logger.SeedIssues(kept)
	s.wg.Go("issues.kept", func() {
		ticker := time.NewTicker(issuesKeptEvery)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
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

// issuesFlushTries is how many times the last write is tried as the node stops.
const issuesFlushTries = 3

// flushIssues is the last write, as the node stops: tried issuesFlushTries
// times, what failed given back between tries.
func (s *QNTXServer) flushIssues() {
	if s.nodeDB == nil {
		return
	}
	var err error
	for try := 1; try <= issuesFlushTries; try++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		taken := logger.TakeIssues()
		err = writeIssues(ctx, s.nodeDB, taken)
		cancel()
		if err == nil {
			return
		}
		logger.ReturnIssues(taken)
	}
	s.logger.Named("issues").Warnw("The issues the node counted were not written as it stopped", "tries", issuesFlushTries, "error", err)
}

// readIssues is what issues_seen holds of day.
func readIssues(ctx context.Context, db *sql.DB, day string) (_ []logger.Issue, err error) {
	rows, err := db.QueryContext(ctx, `SELECT day, key, level, logger, caller, message, plugin, last_error, first_seen, last_seen, occurrences, raised FROM issues_seen WHERE day = ?`, day)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the issues of %s", day)
	}
	defer func() {
		if closed := rows.Close(); err == nil && closed != nil {
			err = errors.Wrap(closed, "failed to close the issues read")
		}
	}()
	var kept []logger.Issue
	for rows.Next() {
		var issue logger.Issue
		var first, last string
		if err := rows.Scan(&issue.Day, &issue.Key, &issue.Level, &issue.Logger, &issue.Caller, &issue.Message, &issue.Plugin,
			&issue.LastError, &first, &last, &issue.Occurrences, &issue.Raised); err != nil {
			return nil, errors.Wrapf(err, "failed to read an issue of %s", day)
		}
		if issue.FirstSeen, err = time.Parse(time.RFC3339Nano, first); err != nil {
			return nil, errors.Wrapf(err, "issue %s first_seen %q does not read", issue.Key, first)
		}
		if issue.LastSeen, err = time.Parse(time.RFC3339Nano, last); err != nil {
			return nil, errors.Wrapf(err, "issue %s last_seen %q does not read", issue.Key, last)
		}
		kept = append(kept, issue)
	}
	return kept, errors.Wrapf(rows.Err(), "failed to read every issue of %s", day)
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
