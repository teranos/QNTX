package roomcomments

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	errors "github.com/teranos/sacred-error"
)

// "everything that was in the room since the agent was told gets told all at
// once as one turn"

// Said is one message of a room, as a turn is told it.
type Said struct{ ID, Sender, Body, At string }

// Told is, for each agent in each room, the last message it was told.
type Told struct{ DB *sql.DB }

// Mark is the time and ID of the last message the agent was told in the room,
// empty when it was never told.
func (t Told) Mark(ctx context.Context, room, agent string) (at, id string, err error) {
	err = t.DB.QueryRowContext(ctx, `SELECT told_at, told_id FROM room_agent_told WHERE room=? AND agent=?`, room, agent).Scan(&at, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return at, id, errors.Wrap(err, "what the agent was last told was not read")
}

// Keep says the agent was told up to this message.
func (t Told) Keep(ctx context.Context, room, agent, at, id string) error {
	_, err := t.DB.ExecContext(ctx, `INSERT INTO room_agent_told(room,agent,told_at,told_id) VALUES(?,?,?,?)
		ON CONFLICT(room,agent) DO UPDATE SET told_at=excluded.told_at, told_id=excluded.told_id`, room, agent, at, id)
	return errors.Wrap(err, "what the agent was told was not kept")
}

// After is what was said after the message with ID last. A room reads "at or
// after" a time, so the message the mark names comes back with the rest.
func After(read []Said, last string) []Said {
	if last == "" {
		return read
	}
	for i, said := range read {
		if said.ID == last {
			return read[i+1:]
		}
	}
	return read
}

// Digest is what a turn is told of what the room said: each message by who
// said it, oldest first.
func Digest(said []Said, truncated bool) string {
	var out strings.Builder
	out.WriteString("What was said in the room since you were last told:\n")
	if truncated {
		out.WriteString("(Older than this was not read.)\n")
	}
	for _, s := range said {
		fmt.Fprintf(&out, "\n[%s] %s:\n%s\n", s.At, s.Sender, strings.TrimSpace(s.Body))
	}
	return out.String()
}
