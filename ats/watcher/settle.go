package watcher

import (
	"strings"

	"github.com/teranos/QNTX/ats/storage"
	errors "github.com/teranos/sacred-error"
)

// axElementPrefix is the id the canvas gives an AX element's watcher.
const axElementPrefix = "ax-element-"

// glyphPrefixes are the ids the canvas gave an element's watcher before a
// glyph was an element, and what it names them now. Migration 066 left the
// ids a node minted as they were.
var glyphPrefixes = [][2]string{
	{"ax-glyph-", axElementPrefix},
	{"se-glyph-", "se-element-"},
}

// settleStored brings what a store holds up to what this build writes, before
// the watchers are loaded. It goes through storage.Watchers rather than a SQL
// migration because on a parquet node the watchers are not in SQLite.
//
// An AX element's watcher was written as a Python watcher with no code. It
// exists to tell the page what matched, which it does at match time; what the
// Python action added was a row in the execution queue for every match, and
// on a node without the python plugin that row waited forever.
func (e *Engine) settleStored() error {
	if err := e.settleGlyphIDs(); err != nil {
		return err
	}
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

// settleGlyphIDs moves a watcher held under a glyph id to the id the canvas
// names. Under the old id the canvas could not find it, show what it queued,
// or switch it off, and opening the element wrote a second watcher beside it;
// where it did, the one the canvas wrote is kept and the old one dropped.
func (e *Engine) settleGlyphIDs() error {
	held, err := e.store.List(e.ctx, false)
	if err != nil {
		return errors.Wrap(err, "failed to list watchers to settle glyph ids")
	}
	for _, w := range held {
		oldID := w.ID
		newID := ""
		for _, p := range glyphPrefixes {
			if strings.HasPrefix(oldID, p[0]) {
				newID = p[1] + strings.TrimPrefix(oldID, p[0])
				break
			}
		}
		if newID == "" {
			continue
		}

		_, err := e.store.Get(e.ctx, newID)
		switch {
		case err == nil:
			// The canvas already wrote it under the new id.
		case errors.Is(err, errors.ErrNotFound):
			w.ID = newID
			if err := e.store.Create(e.ctx, w); err != nil {
				return errors.Wrapf(err, "failed to move watcher %s to %s", oldID, newID)
			}
		default:
			return errors.Wrapf(err, "failed to ask whether %s is held before moving %s", newID, oldID)
		}

		// What it queued under the old id belongs to a watcher that is going.
		purged, err := e.queueStore.PurgeForWatcher(oldID)
		if err != nil {
			return errors.Wrapf(err, "failed to empty the queue of %s before dropping it", oldID)
		}
		if err := e.store.Delete(e.ctx, oldID); err != nil {
			return errors.Wrapf(err, "failed to drop %s once it was held as %s", oldID, newID)
		}
		e.logger.Infow("A watcher under a glyph id is held under the id the canvas names",
			"from", oldID, "to", newID, "purged", purged)
	}
	return nil
}
