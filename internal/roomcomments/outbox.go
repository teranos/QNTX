package roomcomments

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	errors "github.com/teranos/sacred-error"
)

type Target struct{ Provider, Room string }
type Outbox struct{ DB *sql.DB }

func (o Outbox) Keep(ctx context.Context, c *Comment) error {
	payload, err := json.Marshal(c)
	if err != nil {
		return errors.Wrap(err, "the room comment did not encode")
	}
	_, err = o.DB.ExecContext(ctx, `INSERT INTO github_room_comments(key,payload,received_at) VALUES(?,?,?) ON CONFLICT(key) DO NOTHING`, c.Key, payload, time.Now().UTC().Format(time.RFC3339Nano))
	return errors.Wrap(err, "the room comment was not kept")
}

// One node owns the outbox. Its single sweep retries failures and pins the
// destination before posting. Receipts remain; delivered bodies leave the DB.
func (o Outbox) Flush(ctx context.Context, locate func(context.Context) (Target, error), post func(context.Context, Target, Comment) (string, error)) error {
	for range 25 {
		var key string
		var payload []byte
		var target Target
		err := o.DB.QueryRowContext(ctx, `SELECT key,payload,provider,room FROM github_room_comments WHERE delivered_at IS NULL ORDER BY received_at,key LIMIT 1`).Scan(&key, &payload, &target.Provider, &target.Room)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "the waiting room comments were not read")
		}
		var c Comment
		if err = json.Unmarshal(payload, &c); err != nil {
			return errors.Wrap(err, "the waiting room comment did not decode")
		}
		if target.Room == "" {
			target, err = locate(ctx)
			if err == nil {
				_, err = o.DB.ExecContext(ctx, `UPDATE github_room_comments SET provider=?,room=? WHERE key=?`, target.Provider, target.Room, key)
			}
		}
		var id string
		if err == nil {
			id, err = post(ctx, target, c)
		}
		if err == nil && id == "" {
			err = errors.New("the room returned no message ID")
		}
		if err != nil {
			if _, kept := o.DB.ExecContext(ctx, `UPDATE github_room_comments SET last_error=? WHERE key=?`, err.Error(), key); kept != nil {
				return errors.Wrapf(kept, "the room comment failed (%v), and its failure was not kept", err)
			}
			return errors.Wrapf(err, "%s was not said into the Node Room", c.URL)
		}
		// A provider post and this receipt cannot be one transaction. A crash
		// between them can repeat the message: Rooms has no idempotency key.
		_, err = o.DB.ExecContext(ctx, `UPDATE github_room_comments SET payload=NULL,message_id=?,delivered_at=?,last_error='' WHERE key=?`, id, time.Now().UTC().Format(time.RFC3339Nano), key)
		if err != nil {
			return errors.Wrap(err, "the room comment arrived but its receipt was not kept")
		}
	}
	return nil
}
