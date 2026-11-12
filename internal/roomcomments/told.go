package roomcomments

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	errors "github.com/teranos/sacred-error"
)

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] "everything that was in the room since the agent was told gets told all at」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] once as one turn"」

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Said is one message of a room, as a turn is told it.」
type Said struct{ Sender, Body, At string }

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Told is, for each agent in each room, when the last message it was told was」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] said. A provider's messages carry no ID to keep, and read by time.」
type Told struct{ DB *sql.DB }

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Mark is when the last message the agent was told in the room was said, empty」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] when it was never told.」
func (t Told) Mark(ctx context.Context, room, agent string) (string, error) {
	var at string
	err := t.DB.QueryRowContext(ctx, `SELECT told_at FROM room_agent_told WHERE room=? AND agent=?`, room, agent).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return at, errors.Wrap(err, "what the agent was last told was not read")
}

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Keep says the agent was told up to the message said at this time.」
func (t Told) Keep(ctx context.Context, room, agent, at string) error {
	_, err := t.DB.ExecContext(ctx, `INSERT INTO room_agent_told(room,agent,told_at) VALUES(?,?,?)
		ON CONFLICT(room,agent) DO UPDATE SET told_at=excluded.told_at`, room, agent, at)
	return errors.Wrap(err, "what the agent was told was not kept")
}

// 「[Opus 5.5] Back puts the mark where it was before a telling that did not reach the」
// 「[Opus 5.5] room: at was, or none when the agent had never been told.」
func (t Told) Back(ctx context.Context, room, agent, was string) error {
	if was != "" {
		return t.Keep(ctx, room, agent, was)
	}
	_, err := t.DB.ExecContext(ctx, `DELETE FROM room_agent_told WHERE room=? AND agent=?`, room, agent)
	return errors.Wrap(err, "what the agent was told was not put back")
}

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] roomTime is a time as a room says it: RFC3339 to the microsecond, in UTC.」
const roomTime = "2006-01-02T15:04:05.000000Z"

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] ReadFrom is the time to read a room from, for an agent last told the message」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] said at told. A room reads at or after a time, so this is the next」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] millisecond, the finest a room says. It holds the message said at told out,」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] and a message said in that same millisecond but after it with it.」
func ReadFrom(told string) (string, error) {
	at, err := time.Parse(time.RFC3339Nano, told)
	if err != nil {
		return "", errors.Wrapf(err, "%q is no time", told)
	}
	return at.UTC().Add(time.Millisecond).Format(roomTime), nil
}

// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] Digest is what a turn is told of what the room said: each message by who」
// 「[Opus 5.5, Sonnet 5.5, Fable 5.2] said it, oldest first.」
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
