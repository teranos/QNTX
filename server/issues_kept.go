package server

import (
	"context"
	"database/sql"
	"time"

	"github.com/teranos/QNTX/internal/logger"
	errors "github.com/teranos/sacred-error"
)

// "「[Opus 5.5] WE NEED OUR OWN ABSTRACTION AND TRACKING OF THIS IN CASE WE DO HIT SENTRY LIMIT, SO WE NEVER REALLY LOST OBS」"
//
// 「[Opus 5.5] The ledger in internal/logger counts every error the node logs. This writes」
// 「[Opus 5.5] what changed in it to issues_seen. See docs/sentry.md.」

const issuesKeptEvery = time.Minute

// 「[Opus 5.5] seedIssues reads what issues_seen holds of today into the ledger. Until it」
// 「[Opus 5.5] has, nothing is written: a write would put this process's smaller totals」
// 「[Opus 5.5] over the kept ones.」
func (s *QNTXServer) seedIssues() {
	if s.nodeDB == nil || s.issuesSeeded.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	kept, err := readIssues(ctx, s.nodeDB, time.Now().UTC().Format(time.DateOnly))
	if err != nil {
		s.logger.Named("issues").Warnw("What issues_seen holds of today was not read; nothing is written until it is", "error", err)
		return
	}
	logger.SeedIssues(kept)
	s.issuesSeeded.Store(true)
}

// 「[Opus 5.5] keepIssues writes the ledger every issuesKeptEvery, once today is read into」
// 「[Opus 5.5] it. The last write is flushIssues', at the end of Stop, after the last thing」
// 「[Opus 5.5] the node logs as it stops.」
func (s *QNTXServer) keepIssues() {
	if s.nodeDB == nil {
		return
	}
	s.wg.Go("issues.kept", func() {
		ticker := time.NewTicker(issuesKeptEvery)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.seedIssues()
				s.writeIssuesOnce(s.ctx)
			}
		}
	})
}

// 「[Opus 5.5] writeIssuesOnce takes what changed and writes it, giving it back when the」
// 「[Opus 5.5] write fails. One runs at a time, so the last write never misses a batch a」
// 「[Opus 5.5] periodic one still holds.」
func (s *QNTXServer) writeIssuesOnce(ctx context.Context) error {
	if !s.issuesSeeded.Load() {
		return errors.New("today's issues_seen is not read yet")
	}
	s.issuesWrite.Lock()
	defer s.issuesWrite.Unlock()
	taken := logger.TakeIssues()
	if err := writeIssues(ctx, s.nodeDB, taken); err != nil {
		logger.ReturnIssues(taken)
		// 「[Opus 5.5] Warn, not Error: an Error here is an issue that this write would」
		// 「[Opus 5.5] count, about the write that failed to count it.」
		s.logger.Named("issues").Warnw("The issues the node counted were not written to issues_seen", "issues", len(taken), "error", err)
		return err
	}
	return nil
}

// 「[Opus 5.5] issuesFlushTries is how many times the last write is tried as the node stops.」
const issuesFlushTries = 3

// 「[Opus 5.5] flushIssues is the last write, as the node stops, tried issuesFlushTries times.」
func (s *QNTXServer) flushIssues() {
	if s.nodeDB == nil {
		return
	}
	s.seedIssues()
	for range issuesFlushTries {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := s.writeIssuesOnce(ctx)
		cancel()
		if err == nil {
			return
		}
	}
}

// 「[Opus 5.5] readIssues is what issues_seen holds of day.」
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

// 「[Opus 5.5] writeIssues upserts each issue's day. A row's counts are the ledger's own」
// 「[Opus 5.5] running totals for that day, so the newer row replaces the older.」
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
