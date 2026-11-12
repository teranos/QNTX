package roomcomments

import (
	"context"
	"database/sql"
	"time"

	errors "github.com/teranos/sacred-error"
)

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] "review comments need to wake only when no comment was placed 30min since the」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] last comment"」

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Due is the wakes waiting for a quiet room, kept in the node's file so that a」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] node that restarts in the meantime still wakes the agent.」
type Due struct{ DB *sql.DB }

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Wake is one agent waiting to be told one room.」
type Wake struct {
	Target                Target
	Agent, Speaker, Since string
	DueAt                 string
}

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] dueTime sorts as time does, to the nanosecond.」
const dueTime = "2006-01-02T15:04:05.000000000Z"

// 「[Opus 5.5] Defer says the agent is to be told the room at due. A comment that arrives」
// 「[Opus 5.5] while one is waiting moves it later and keeps the earliest since: what the」
// 「[Opus 5.5] agent is told begins at the first comment of the stretch. A wake is one」
// 「[Opus 5.5] provider's: two providers that call a room alike wait apart.」
func (d Due) Defer(ctx context.Context, target Target, agent, speaker, since string, due time.Time) error {
	_, err := d.DB.ExecContext(ctx, `INSERT INTO room_agent_wake_due(room,agent,provider,speaker,since,due_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(provider,room,agent) DO UPDATE SET due_at=excluded.due_at, speaker=excluded.speaker`,
		target.Room, agent, target.Provider, speaker, since, due.UTC().Format(dueTime))
	return errors.Wrap(err, "the wake waiting for a quiet room was not kept")
}

// 「[Opus 5.5] Ready is every wake whose time has come, oldest first.」
func (d Due) Ready(ctx context.Context, now time.Time) (_ []Wake, err error) {
	rows, err := d.DB.QueryContext(ctx, `SELECT provider,room,agent,speaker,since,due_at FROM room_agent_wake_due WHERE due_at<=? ORDER BY due_at,room,agent`, now.UTC().Format(dueTime))
	if err != nil {
		return nil, errors.Wrap(err, "the wakes waiting for a quiet room were not read")
	}
	defer func() {
		if closed := rows.Close(); err == nil {
			err = errors.Wrap(closed, "the wakes waiting for a quiet room did not close")
		}
	}()
	var ready []Wake
	for rows.Next() {
		var w Wake
		if err := rows.Scan(&w.Target.Provider, &w.Target.Room, &w.Agent, &w.Speaker, &w.Since, &w.DueAt); err != nil {
			return nil, errors.Wrap(err, "a wake waiting for a quiet room did not read")
		}
		ready = append(ready, w)
	}
	return ready, errors.Wrap(rows.Err(), "the wakes waiting for a quiet room were not all read")
}

// 「[Opus 5.5] Claim holds a wake whose time has come until a later time, and says whether」
// 「[Opus 5.5] it was this call that did: the wake as it is held, to be done or to come」
// 「[Opus 5.5] again. The row stays, so a node that stops before the agent was told tells」
// 「[Opus 5.5] it after the hold. A comment that moved it later in the meantime leaves it,」
// 「[Opus 5.5] and it is not claimed.」
func (d Due) Claim(ctx context.Context, w Wake, until time.Time) (Wake, bool, error) {
	held := until.UTC().Format(dueTime)
	claimed, err := d.DB.ExecContext(ctx, `UPDATE room_agent_wake_due SET due_at=? WHERE provider=? AND room=? AND agent=? AND due_at=?`,
		held, w.Target.Provider, w.Target.Room, w.Agent, w.DueAt)
	if err != nil {
		return w, false, errors.Wrap(err, "the wake waiting for a quiet room was not claimed")
	}
	n, err := claimed.RowsAffected()
	if err != nil {
		return w, false, errors.Wrap(err, "the wake waiting for a quiet room was not counted")
	}
	w.DueAt = held
	return w, n == 1, nil
}

// 「[Opus 5.5] Done removes a claimed wake once the agent was told. A comment that came」
// 「[Opus 5.5] while it was being told moved the wake later, and that one stays: it is the」
// 「[Opus 5.5] next stretch's.」
func (d Due) Done(ctx context.Context, w Wake) error {
	_, err := d.DB.ExecContext(ctx, `DELETE FROM room_agent_wake_due WHERE provider=? AND room=? AND agent=? AND due_at=?`,
		w.Target.Provider, w.Target.Room, w.Agent, w.DueAt)
	return errors.Wrap(err, "the wake the agent was told was not removed")
}
