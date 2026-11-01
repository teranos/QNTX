package watcher

import (
	"encoding/json"
	"reflect"
	"sync"

	"github.com/teranos/QNTX/ats/storage"
	errors "github.com/teranos/sacred-error"
)

// BuiltinActionData is a built-in row's action data, naming the handler the
// row reaches: what engine_execute.go reads back as BuiltinExecuteAction.
func BuiltinActionData(handlerName string) string {
	// A struct of one string field marshals; there is no error to carry.
	data, _ := json.Marshal(BuiltinExecuteAction{HandlerName: handlerName})
	return string(data)
}

// A subsystem compiled in stands its rows here, beside QNTX's own, before the
// namespaces start (server/linked.go). The same rule holds for a stood row as
// for a born one: on every namespace, back on an empty database, and no route
// takes it away. The code it runs is compiled into this node, which is what
// makes it safe to be permanent.

var (
	stoodMu sync.RWMutex
	// stood is what the subsystems linked in stood; standing is what QNTX is
	// born with. Standing hands out both.
	stood []storage.Watcher
)

// Stand stands rows beside QNTX's own. A row under an id already standing is
// refused by that id, unless it is the same row: one build booting again in
// one process stands its rows once. The rows are copied on the way in.
func Stand(rows ...storage.Watcher) error {
	stoodMu.Lock()
	defer stoodMu.Unlock()
	for _, row := range rows {
		if row.ID == "" {
			return errors.New("a row with no id cannot stand: the engine reports by id")
		}
		if held, standing := standingRow(row.ID); standing {
			if reflect.DeepEqual(held, row) {
				continue
			}
			return errors.Newf("a row is standing as %s already, and this one differs", row.ID)
		}
		stood = append(stood, row)
	}
	return nil
}

// standingRow is the row of that id, born or stood. The caller holds stoodMu.
func standingRow(id string) (storage.Watcher, bool) {
	for _, w := range standing {
		if w.ID == id {
			return w, true
		}
	}
	for _, w := range stood {
		if w.ID == id {
			return w, true
		}
	}
	return storage.Watcher{}, false
}
