package roomcomments

import (
	"context"
	"database/sql"
	"time"

	errors "github.com/teranos/sacred-error"
)

// "review comments need to wake only when no comment was placed 30min since the
// last comment"

// Due is the wakes waiting for a quiet room, kept in the node's file so that a
// node that restarts in the meantime still wakes the agent.
type Due struct{ DB *sql.DB }

// Wake is one agent waiting to be told one room.
type Wake struct {
	Target                Target
	Agent, Speaker, Since string
	DueAt                 string
}

// dueTime sorts as time does, to the nanosecond.
const dueTime = "2006-01-02T15:04:05.000000000Z"

// Defer says the agent is to be told the room at due. A comment that arrives
// while one is waiting moves it later and keeps the earliest since: what the
// agent is told begins at the first comment of the stretch.
func (d Due) Defer(ctx context.Context, target Target, agent, speaker, since string, due time.Time) error {
	_, err := d.DB.ExecContext(ctx, `INSERT INTO room_agent_wake_due(room,agent,provider,speaker,since,due_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(room,agent) DO UPDATE SET due_at=excluded.due_at, speaker=excluded.speaker, provider=excluded.provider`,
		target.Room, agent, target.Provider, speaker, since, due.UTC().Format(dueTime))
	return errors.Wrap(err, "the wake waiting for a quiet room was not kept")
}

// Ready is every wake whose time has come, oldest first.
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

// Take removes the wake and says whether it was this call that did. A comment
// that moved it later in the meantime leaves it, and it is not taken.
func (d Due) Take(ctx context.Context, w Wake) (bool, error) {
	taken, err := d.DB.ExecContext(ctx, `DELETE FROM room_agent_wake_due WHERE room=? AND agent=? AND due_at=?`, w.Target.Room, w.Agent, w.DueAt)
	if err != nil {
		return false, errors.Wrap(err, "the wake waiting for a quiet room was not taken")
	}
	n, err := taken.RowsAffected()
	return n == 1, errors.Wrap(err, "the wake waiting for a quiet room was not counted")
}
