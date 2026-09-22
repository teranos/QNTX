//go:build qntxwasm

package watcher_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// A built-in is reached by name with the attestation that matched, and nothing
// about a plugin's loading stands between them.
type mockBuiltinExecutor struct {
	mu      sync.Mutex
	calls   []string
	lastAs  *types.As
	refuse  string
}

func (m *mockBuiltinExecutor) ExecuteBuiltin(_ context.Context, handlerName string, as *types.As) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, handlerName)
	m.lastAs = as
	if handlerName == m.refuse {
		return errors.Newf("no built-in named %s", handlerName)
	}
	return nil
}

func TestEngine_ExecuteBuiltin(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://localhost:8770", logger)

	exec := &mockBuiltinExecutor{}
	engine.SetBuiltinExecutor(exec)

	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "builtin-test",
		Name:              "Builtin Test",
		ActionType:        storage.ActionTypeBuiltinExecute,
		ActionData:        `{"handler_name":"test.builtin"}`,
		MaxFiresPerSecond: 105,
		Enabled:           true,
		Filter:            types.AxFilter{Predicates: []string{"builtin-test"}},
	}
	if err := store.Create(context.Background(), w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	as := &types.As{
		ID:         "builtin-1",
		Subjects:   []string{"teranos/ground:main"},
		Predicates: []string{"builtin-test"},
		Attributes: map[string]interface{}{"repo": "teranos/ground", "branch": "main", "sha": "abc"},
	}
	engine.OnAttestationCreated(as)
	time.Sleep(100 * time.Millisecond)

	exec.mu.Lock()
	defer exec.mu.Unlock()
	if len(exec.calls) != 1 || exec.calls[0] != "test.builtin" {
		t.Fatalf("built-in was reached %v; wanted one call to test.builtin", exec.calls)
	}
	if exec.lastAs == nil || exec.lastAs.ID != "builtin-1" {
		t.Fatalf("built-in was handed %+v; wanted the attestation that matched", exec.lastAs)
	}
	if exec.lastAs.Attributes["repo"] != "teranos/ground" {
		t.Errorf("attributes did not travel: %v", exec.lastAs.Attributes)
	}
}

// The node is born watching for ground's push. No stored watcher, one
// ci-status attestation, and ci.watch is reached with it.
func TestStandingCIPushedReachesCIWatch(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://localhost:8770", logger)

	exec := &mockBuiltinExecutor{}
	engine.SetBuiltinExecutor(exec)
	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	engine.OnAttestationCreated(&types.As{
		ID:         "ci-status-1",
		Predicates: []string{watcher.CIPushedPredicate},
		Attributes: map[string]interface{}{"repo": "teranos/ground", "branch": "main", "sha": "abc"},
	})
	time.Sleep(100 * time.Millisecond)

	exec.mu.Lock()
	defer exec.mu.Unlock()
	if len(exec.calls) != 1 || exec.calls[0] != watcher.CIWatchHandlerName {
		t.Fatalf("standing row reached %v; wanted one call to %s", exec.calls, watcher.CIWatchHandlerName)
	}
	if exec.lastAs == nil || exec.lastAs.ID != "ci-status-1" {
		t.Fatalf("standing row handed over %+v", exec.lastAs)
	}
}

// A name nothing is registered under is the watcher's error, recorded on it,
// the way a refused webhook is.
func TestEngine_ExecuteBuiltin_UnknownNameIsRecorded(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://localhost:8770", logger)

	exec := &mockBuiltinExecutor{refuse: "nobody.home"}
	engine.SetBuiltinExecutor(exec)

	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "builtin-unknown",
		Name:              "Builtin Unknown",
		ActionType:        storage.ActionTypeBuiltinExecute,
		ActionData:        `{"handler_name":"nobody.home"}`,
		MaxFiresPerSecond: 105,
		Enabled:           true,
		Filter:            types.AxFilter{Predicates: []string{"builtin-unknown"}},
	}
	if err := store.Create(context.Background(), w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	engine.OnAttestationCreated(&types.As{ID: "unknown-1", Predicates: []string{"builtin-unknown"}})
	time.Sleep(100 * time.Millisecond)

	got, ok := engine.GetWatcher("builtin-unknown")
	if !ok {
		t.Fatal("watcher not loaded")
	}
	if got.ErrorCount == 0 || got.LastError == "" {
		t.Fatalf("the refusal was not recorded on the watcher: count=%d last=%q", got.ErrorCount, got.LastError)
	}
}
