package server

import (
	"context"
	"sync"
	"testing"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"go.uber.org/zap"
)

type recordingBuiltins struct {
	mu    sync.Mutex
	calls []string
	last  *types.As
}

func (r *recordingBuiltins) ExecuteBuiltin(_ context.Context, name string, as *types.As) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
	r.last = as
	return nil
}

// "What every namespace watches before anybody makes a watcher." The ground
// token attests in the Ground namespace; the engine watches default. Five
// pushes went through and the standing row saw none of them. The standing
// observer is what a namespace registers when it starts, whichever it is.
func TestStandingObserverReachesTheBuiltInForAnyNamespace(t *testing.T) {
	rec := &recordingBuiltins{}
	o := &standingObserver{
		ctx:      context.Background(),
		builtins: func() watcher.BuiltinExecutor { return rec },
		logger:   zap.NewNop().Sugar(),
	}

	o.OnAttestationCreated(&types.As{
		ID:         "ci-status-in-ground",
		Predicates: []string{watcher.CIPushedPredicate},
		Contexts:   []string{"session:s"},
		Attributes: map[string]interface{}{"repo": "teranos/ground", "branch": "main", "sha": "abc"},
	})

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.calls) != 1 || rec.calls[0] != watcher.CIWatchHandlerName {
		t.Fatalf("standing row reached %v; wanted one call to %s", rec.calls, watcher.CIWatchHandlerName)
	}
	if rec.last == nil || rec.last.ID != "ci-status-in-ground" {
		t.Fatalf("handed over %+v", rec.last)
	}
}

// A row a standing filter does not name reaches nothing.
func TestStandingObserverIgnoresWhatNoStandingRowWatches(t *testing.T) {
	rec := &recordingBuiltins{}
	o := &standingObserver{ctx: context.Background(), builtins: func() watcher.BuiltinExecutor { return rec }, logger: zap.NewNop().Sugar()}
	o.OnAttestationCreated(&types.As{ID: "x", Predicates: []string{"something-else"}})
	if len(rec.calls) != 0 {
		t.Fatalf("reached %v for a row nothing standing watches", rec.calls)
	}
}

// Before the built-ins are wired, a row is refused out loud, not dropped: the
// executor is looked up at fire time so a namespace started early still fires
// once the executor exists.
func TestStandingObserverWithNoExecutorFiresNothing(t *testing.T) {
	o := &standingObserver{ctx: context.Background(), builtins: func() watcher.BuiltinExecutor { return nil }, logger: zap.NewNop().Sugar()}
	o.OnAttestationCreated(&types.As{ID: "x", Predicates: []string{watcher.CIPushedPredicate}})
}
