package watcher

import (
	"strings"

	"github.com/teranos/QNTX/ats/storage"
	errors "github.com/teranos/sacred-error"
)

// axElementPrefix is the id the canvas gives an AX element's watcher.
const axElementPrefix = "ax-element-"

// settleStored brings what a store holds up to what this build writes, before
// the watchers are loaded. It goes through storage.Watchers rather than a SQL
// migration because on a parquet node the watchers are not in SQLite.
//
// An AX element's watcher was written as a Python watcher with no code. It
// exists to tell the page what matched, which it does at match time; what the
// Python action added was a row in the execution queue for every match, and
// on a node without the python plugin that row waited forever.
func (e *Engine) settleStored() error {
	held, err := e.store.List(e.ctx, false)
	if err != nil {
		return errors.Wrap(err, "failed to list watchers to settle")
	}
	for _, w := range held {
		if !strings.HasPrefix(w.ID, axElementPrefix) ||
			w.ActionType != storage.ActionTypePython || w.ActionData != "" {
			continue
		}
		w.ActionType = storage.ActionTypeTell
		if err := e.store.Update(e.ctx, w); err != nil {
			return errors.Wrapf(err, "failed to make AX watcher %s a tell", w.ID)
		}
		purged, err := e.queueStore.PurgeForWatcher(w.ID)
		if err != nil {
			return errors.Wrapf(err, "AX watcher %s is a tell, but its queue was not emptied", w.ID)
		}
		e.logger.Infow("An AX watcher that ran no code is a tell, and what it queued is gone",
			"watcher_id", w.ID, "purged", purged)
	}
	return nil
}
