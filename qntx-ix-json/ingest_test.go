package qntxixjson

import (
	"context"
	"database/sql"
	"testing"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap"
)

// --- test mocks ---

type mockStore struct {
	lastCmd *types.AsCommand
	err     error
}

func (m *mockStore) GenerateAndCreateAttestation(_ context.Context, cmd *types.AsCommand) (*types.As, error) {
	m.lastCmd = cmd
	if m.err != nil {
		return nil, m.err
	}
	return &types.As{ID: "test-id"}, nil
}

func (m *mockStore) CreateAttestation(*types.As) error                          { return nil }
func (m *mockStore) CreateAttestationInbound(*types.As) error                   { return nil }
func (m *mockStore) AttestationExists(string) bool                              { return false }
func (m *mockStore) GetAttestations(ats.AttestationFilter) ([]*types.As, error) { return nil, nil }

type mockServices struct {
	store  ats.AttestationStore
	logger *zap.SugaredLogger
}

func (m *mockServices) Database() *sql.DB                { return nil }
func (m *mockServices) Logger(string) *zap.SugaredLogger { return m.logger }
func (m *mockServices) Config(string) plugin.Config      { return nil }
func (m *mockServices) ATSStore() ats.AttestationStore   { return m.store }
func (m *mockServices) Queue() plugin.QueueService       { return nil }
func (m *mockServices) Schedule() plugin.ScheduleService { return nil }
func (m *mockServices) FileService() plugin.FileService  { return nil }

func testPlugin(store ats.AttestationStore) *Plugin {
	p := NewPlugin()
	svc := &mockServices{
		store:  store,
		logger: zap.NewNop().Sugar(),
	}
	p.Init(svc)
	return p
}

// --- tests ---

func TestCreateAttestationFromJSON(t *testing.T) {
	store := &mockStore{}
	p := testPlugin(store)
	ctx := context.Background()

	mapping := &MappingConfig{
		SubjectPath:   "id",
		PredicatePath: "type",
		ContextPath:   "source",
		KeyRemapping:  map[string]string{},
	}

	data := map[string]any{
		"id":     "user-42",
		"type":   "created",
		"source": "api",
		"email":  "test@example.com",
		"score":  99.5,
	}

	if err := p.createAttestationFromJSON(ctx, store, data, mapping); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cmd := store.lastCmd
	if cmd == nil {
		t.Fatal("store was not called")
	}

	// SPC extracted correctly
	if cmd.Subjects[0] != "user-42" {
		t.Errorf("subject = %q, want %q", cmd.Subjects[0], "user-42")
	}
	if cmd.Predicates[0] != "created" {
		t.Errorf("predicate = %q, want %q", cmd.Predicates[0], "created")
	}
	if cmd.Contexts[0] != "api" {
		t.Errorf("context = %q, want %q", cmd.Contexts[0], "api")
	}

	// SPC fields excluded from attributes, remaining fields kept
	if _, ok := cmd.Attributes["id"]; ok {
		t.Error("subject field 'id' should not be in attributes")
	}
	if _, ok := cmd.Attributes["email"]; !ok {
		t.Error("remaining field 'email' should be in attributes")
	}
	if cmd.Source != "ix-json" {
		t.Errorf("source = %q, want %q", cmd.Source, "ix-json")
	}

	// Key remapping
	t.Run("key remapping", func(t *testing.T) {
		store := &mockStore{}
		p := testPlugin(store)

		mapping := &MappingConfig{
			SubjectPath:   "user_id",
			PredicatePath: "event_type",
			KeyRemapping: map[string]string{
				"id":   "user_id",
				"type": "event_type",
			},
		}

		data := map[string]any{
			"id":   "u-1",
			"type": "login",
			"ip":   "127.0.0.1",
		}

		if err := p.createAttestationFromJSON(ctx, store, data, mapping); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if store.lastCmd.Subjects[0] != "u-1" {
			t.Errorf("subject = %q after remap, want %q", store.lastCmd.Subjects[0], "u-1")
		}
	})

	// Missing subject/predicate
	t.Run("missing subject returns error", func(t *testing.T) {
		store := &mockStore{}
		p := testPlugin(store)

		mapping := &MappingConfig{
			SubjectPath:   "missing_field",
			PredicatePath: "type",
		}

		data := map[string]any{"type": "event"}

		err := p.createAttestationFromJSON(ctx, store, data, mapping)
		if err == nil {
			t.Fatal("expected error for missing subject")
		}
	})
}

func TestInferMapping(t *testing.T) {
	p := testPlugin(nil)

	// Known candidate fields
	t.Run("known fields", func(t *testing.T) {
		data := []byte(`{"id": "123", "type": "event", "source": "api", "value": 42}`)
		m := p.inferMapping(data)

		if m.SubjectPath != "id" {
			t.Errorf("SubjectPath = %q, want %q", m.SubjectPath, "id")
		}
		if m.PredicatePath != "type" {
			t.Errorf("PredicatePath = %q, want %q", m.PredicatePath, "type")
		}
		if m.ContextPath != "source" {
			t.Errorf("ContextPath = %q, want %q", m.ContextPath, "source")
		}
	})

	// Array input — should use first element
	t.Run("array input uses first element", func(t *testing.T) {
		data := []byte(`[{"name": "alice", "kind": "user", "origin": "signup"}, {"name": "bob"}]`)
		m := p.inferMapping(data)

		if m.SubjectPath != "name" {
			t.Errorf("SubjectPath = %q, want %q", m.SubjectPath, "name")
		}
		if m.PredicatePath != "kind" {
			t.Errorf("PredicatePath = %q, want %q", m.PredicatePath, "kind")
		}
		if m.ContextPath != "origin" {
			t.Errorf("ContextPath = %q, want %q", m.ContextPath, "origin")
		}
	})

	// No candidate fields — falls back to string fields
	t.Run("no candidates falls back to string fields", func(t *testing.T) {
		data := []byte(`{"foo": "bar", "baz": "qux", "num": 42}`)
		m := p.inferMapping(data)

		// All three should pick string fields (map iteration order varies, but all should be non-empty)
		if m.SubjectPath == "" {
			t.Error("SubjectPath should not be empty")
		}
		if m.PredicatePath == "" {
			t.Error("PredicatePath should not be empty")
		}
	})

	// Invalid JSON — returns empty mapping
	t.Run("invalid JSON returns empty mapping", func(t *testing.T) {
		m := p.inferMapping([]byte(`not json`))
		if m.SubjectPath != "" || m.PredicatePath != "" || m.ContextPath != "" {
			t.Error("invalid JSON should return empty mapping")
		}
	})
}
