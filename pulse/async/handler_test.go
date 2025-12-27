package async

import (
	"context"
	"testing"
)

// mockHandler implements JobHandler for testing
type mockHandler struct {
	name string
}

func (m *mockHandler) Execute(ctx context.Context, job *Job) error {
	return nil
}

func (m *mockHandler) Name() string {
	return m.name
}

func TestHandlerRegistry(t *testing.T) {
	t.Run("NewHandlerRegistry creates empty registry", func(t *testing.T) {
		r := NewHandlerRegistry()
		if r == nil {
			t.Fatal("Expected non-nil registry")
		}
		if len(r.Names()) != 0 {
			t.Errorf("Expected empty registry, got %d handlers", len(r.Names()))
		}
	})

	t.Run("Register and Get", func(t *testing.T) {
		r := NewHandlerRegistry()
		handler := &mockHandler{name: "test.jd-ingestion"}

		r.Register(handler)

		got := r.Get("test.jd-ingestion")
		if got != handler {
			t.Errorf("Expected registered handler, got %v", got)
		}
	})

	t.Run("Get returns nil for unregistered name", func(t *testing.T) {
		r := NewHandlerRegistry()

		got := r.Get("test.unknown")
		if got != nil {
			t.Errorf("Expected nil for unregistered name, got %v", got)
		}
	})

	t.Run("Has returns true for registered name", func(t *testing.T) {
		r := NewHandlerRegistry()
		r.Register(&mockHandler{name: "test.jd-ingestion"})

		if !r.Has("test.jd-ingestion") {
			t.Error("Expected Has() to return true for registered name")
		}
	})

	t.Run("Has returns false for unregistered name", func(t *testing.T) {
		r := NewHandlerRegistry()

		if r.Has("test.unknown") {
			t.Error("Expected Has() to return false for unregistered name")
		}
	})

	t.Run("Names returns all registered names", func(t *testing.T) {
		r := NewHandlerRegistry()
		r.Register(&mockHandler{name: "test.jd-ingestion"})
		r.Register(&mockHandler{name: "test.candidate-scoring"})

		names := r.Names()
		if len(names) != 2 {
			t.Errorf("Expected 2 names, got %d", len(names))
		}

		// Check both names are present (order not guaranteed)
		found := make(map[string]bool)
		for _, name := range names {
			found[name] = true
		}
		if !found["test.jd-ingestion"] || !found["test.candidate-scoring"] {
			t.Errorf("Expected both handler names, got %v", names)
		}
	})

	t.Run("Register panics on duplicate", func(t *testing.T) {
		r := NewHandlerRegistry()
		r.Register(&mockHandler{name: "test.jd-ingestion"})

		defer func() {
			if recover() == nil {
				t.Error("Expected panic on duplicate registration")
			}
		}()

		r.Register(&mockHandler{name: "test.jd-ingestion"})
	})
}

func TestRegistryExecutor(t *testing.T) {
	t.Run("Execute dispatches to registered handler by name", func(t *testing.T) {
		r := NewHandlerRegistry()
		r.Register(&mockHandler{name: "test.jd-ingestion"})

		exec := NewRegistryExecutor(r, nil)
		job := &Job{HandlerName: "test.jd-ingestion"}

		err := exec.Execute(context.Background(), job)
		if err != nil {
			t.Errorf("Expected nil error, got %v", err)
		}
	})

	t.Run("Execute returns error for unregistered handler", func(t *testing.T) {
		r := NewHandlerRegistry()
		exec := NewRegistryExecutor(r, nil)
		job := &Job{HandlerName: "test.unknown"}

		err := exec.Execute(context.Background(), job)
		if err == nil {
			t.Error("Expected error for unregistered handler")
		}
	})

	t.Run("Execute uses fallback for unregistered handler", func(t *testing.T) {
		r := NewHandlerRegistry()
		fallback := &mockHandler{name: "test.batch-rescore"}
		fallbackRegistry := NewHandlerRegistry()
		fallbackRegistry.Register(fallback)
		fallbackExec := NewRegistryExecutor(fallbackRegistry, nil)

		exec := NewRegistryExecutor(r, fallbackExec)
		job := &Job{HandlerName: "test.batch-rescore"}

		err := exec.Execute(context.Background(), job)
		if err != nil {
			t.Errorf("Expected fallback to handle, got error: %v", err)
		}
	})
}
