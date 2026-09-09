package server

import (
	"go.uber.org/zap"

	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/so/actions/prompt"
	"github.com/teranos/QNTX/ats/storage"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/server/namespaces"
)

// servingOne is a node running one namespace, made of the store and the
// operational database a test already has.
func servingOne(db *sql.DB, store ats.AttestationStore) *namespaces.Held {
	held, err := namespaces.Serving(namespaces.Made{
		Store:      store,
		Watchers:   storage.NewWatcherStore(db),
		Schedules:  schedule.NewStore(db),
		Canvas:     glyphstorage.NewCanvasStore(db),
		Embeddings: storage.NewEmbeddingStore(db, zap.NewNop()),
		Rich:       storage.NewBoundedStore(db, nil, zap.NewNop().Sugar()),
		Executions: schedule.NewExecutionStore(db),
		Prompts:    prompt.NewPromptStore(db, store),
		Aliases:    storage.NewAliasStore(db),
		Queries:    storage.NewSQLQueryStore(db),
	})
	if err != nil {
		panic(err)
	}
	return held
}

// oneNamespace is a namespace made the same way, for a test that adds one.
func oneNamespace(name string, store ats.AttestationStore) *namespaces.Universe {
	u, err := namespaces.NewUniverse(name, namespaces.Made{
		Store:      store,
		Watchers:   stubWatchers{},
		Schedules:  &schedule.Store{},
		Canvas:     &glyphstorage.CanvasStore{},
		Embeddings: &storage.EmbeddingStore{},
		Rich:       &storage.BoundedStore{},
		Executions: &schedule.ExecutionStore{},
		Prompts:    &prompt.PromptStore{},
		Aliases:    &storage.AliasStore{},
		Queries:    &storage.SQLQueryStore{},
	})
	if err != nil {
		panic(err)
	}
	return u
}

type stubWatchers struct{ storage.Watchers }

// stubStore stands in for the attestations when a test is about routing rather
// than about what lands.
type stubStore struct{ ats.AttestationStore }
