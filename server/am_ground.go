package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

// "THE STARS, QNTX, IT CAN ONLY REALLY TELL YOU ABOUT SOME MINOR THINGS, LIKE HOW MANY TIMES IT WROTE DEFERRED NEW BACK OR ITS OWN UPTIME"

// "this part is supposed to show things QNTX does for ground specifically"

// What the node does in Ground's place is ci.watch: it waits on a run where the
// socket is, and leaves what it concluded on the status line the laptop polls
// (ci_pulse.go). am ground is that, asked for whole.

// groundWatches is every standing watcher that reaches ci.watch: what Ground
// attests that sets the node waiting.
func groundWatches() ([]map[string]any, error) {
	watches := []map[string]any{}
	for _, w := range watcher.Standing() {
		if w.ActionType != storage.ActionTypeBuiltinExecute {
			continue
		}
		var action watcher.BuiltinExecuteAction
		if err := json.Unmarshal([]byte(w.ActionData), &action); err != nil {
			return nil, errors.Wrapf(err, "the standing watcher %s names a built-in in action data that does not read: %q", w.ID, w.ActionData)
		}
		if action.HandlerName != watcher.CIWatchHandlerName {
			continue
		}
		watches = append(watches, map[string]any{"id": w.ID, "name": w.Name, "predicates": w.Filter.Predicates})
	}
	return watches, nil
}

// amGround answers am ground for whoever asks. News is filed under the person
// (newsKey), so a node that knows nobody has left nothing for anybody.
func (s *QNTXServer) amGround(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	watches, err := groundWatches()
	if err != nil {
		if s.logger != nil {
			s.logger.Errorw("am ground could not read what the node watches for Ground", "error", err)
		}
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "what the node watches for Ground did not read: " + err.Error()}
	}

	var held []News
	var left int64
	if admitted, ok := auth.AdmissionFrom(ctx); ok {
		held, left = s.news.leftFor(newsKey(admitted))
	}
	now := time.Now().UnixMilli()
	news := make([]map[string]any, 0, len(held))
	for _, n := range held {
		said := map[string]any{}
		// What the item carries in full, the way a click on it answers (newsDetail).
		for k, v := range n.Detail {
			said[k] = v
		}
		said["id"] = n.ID
		said["name"] = n.Item.Name
		said["note"] = n.Item.Note
		said["symbol"] = n.Item.Symbol
		said["waiting"] = n.Quiet
		said["at"] = time.UnixMilli(n.AtMs).UTC().Format(time.RFC3339)
		said["until"] = time.UnixMilli(n.UntilMs).UTC().Format(time.RFC3339)
		said["on_row"] = n.UntilMs > now
		news = append(news, said)
	}

	failed := []map[string]any{}
	for _, f := range s.handlerFailures.since(handlerFailureWindow) {
		if f.Handler != watcher.CIWatchHandlerName {
			continue
		}
		failed = append(failed, map[string]any{
			"at":           time.UnixMilli(f.AtMs).UTC().Format(time.RFC3339),
			"error":        f.Error,
			"execution_id": f.ExecutionID,
		})
	}

	started := ""
	if !s.startedAt.IsZero() {
		started = s.startedAt.UTC().Format(time.RFC3339)
	}
	return map[string]any{"started": started, "left": left, "watches": watches, "news": news, "failed": failed}, nil
}
