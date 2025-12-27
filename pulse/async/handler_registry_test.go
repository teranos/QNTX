package async

import (
	"context"
	"fmt"
	"testing"
)

// ============================================================================
// Phone Book Test Universe
// ============================================================================
//
// Characters:
//   - Phone Company: Maintains the registry of who handles what calls
//
// Theme: HandlerRegistry is like a phone book that maps department names to
// the people who handle calls for that department. Want to reach "tech-support"?
// The phone book tells you who answers those calls.
// ============================================================================

// phoneBookTestHandler is a test handler that just records it was called
type phoneBookTestHandler struct {
	name        string
	wasCalled   bool
	lastJobID   string
	shouldError bool
}

func (h *phoneBookTestHandler) Name() string {
	return h.name
}

func (h *phoneBookTestHandler) Execute(ctx context.Context, job *Job) error {
	h.wasCalled = true
	h.lastJobID = job.ID
	if h.shouldError {
		return fmt.Errorf("mock handler error")
	}
	return nil
}

func TestHandlerRegistry_PhoneBook(t *testing.T) {
	t.Run("phone company creates empty phone book", func(t *testing.T) {
		// Phone company sets up a new phone book
		phoneBook := NewHandlerRegistry()

		if phoneBook == nil {
			t.Fatal("Expected phone book to be created")
		}

		// Empty phone book has no entries
		if len(phoneBook.Names()) != 0 {
			t.Errorf("Expected empty phone book, got %d entries", len(phoneBook.Names()))
		}
	})

	t.Run("phone company adds departments to phone book", func(t *testing.T) {
		phoneBook := NewHandlerRegistry()

		// Register handlers (like adding phone book entries)
		techSupport := &phoneBookTestHandler{name: "tech-support"}
		billing := &phoneBookTestHandler{name: "billing"}
		sales := &phoneBookTestHandler{name: "sales"}

		phoneBook.Register(techSupport)
		phoneBook.Register(billing)
		phoneBook.Register(sales)

		// Check all departments are in the phone book
		names := phoneBook.Names()
		if len(names) != 3 {
			t.Errorf("Expected 3 departments in phone book, got %d", len(names))
		}
	})

	t.Run("phone company looks up department in phone book", func(t *testing.T) {
		phoneBook := NewHandlerRegistry()

		// Add tech support to phone book
		techSupport := &phoneBookTestHandler{name: "tech-support"}
		phoneBook.Register(techSupport)

		// Look up tech support in phone book
		handler := phoneBook.Get("tech-support")
		if handler == nil {
			t.Fatal("Expected to find tech-support in phone book")
		}
		if handler.Name() != "tech-support" {
			t.Errorf("Expected tech-support, got %s", handler.Name())
		}
	})

	t.Run("phone company checks if department exists", func(t *testing.T) {
		phoneBook := NewHandlerRegistry()

		// Add tech support
		techSupport := &phoneBookTestHandler{name: "tech-support"}
		phoneBook.Register(techSupport)

		// Check existing department
		if !phoneBook.Has("tech-support") {
			t.Error("Expected phone book to have tech-support")
		}

		// Check non-existent department
		if phoneBook.Has("legal") {
			t.Error("Expected phone book to NOT have legal department")
		}
	})

	t.Run("phone company routes calls using phone book", func(t *testing.T) {
		phoneBook := NewHandlerRegistry()

		// Add tech support to phone book
		techSupport := &phoneBookTestHandler{name: "tech-support"}
		phoneBook.Register(techSupport)

		// Create executor that uses phone book to route calls
		executor := NewRegistryExecutor(phoneBook, nil)

		// Incoming call for tech support
		job := &Job{
			ID:          "call-001",
			HandlerName: "tech-support",
			Source:      "customer-hotline",
		}

		// Route the call
		err := executor.Execute(context.Background(), job)
		if err != nil {
			t.Fatalf("Failed to route call: %v", err)
		}

		// Verify tech support handled the call
		if !techSupport.wasCalled {
			t.Error("Expected tech-support to handle the call")
		}
		if techSupport.lastJobID != "call-001" {
			t.Errorf("Expected job call-001, got %s", techSupport.lastJobID)
		}
	})

	t.Run("phone company rejects duplicate entries", func(t *testing.T) {
		phoneBook := NewHandlerRegistry()

		// Add tech support
		techSupport1 := &phoneBookTestHandler{name: "tech-support"}
		phoneBook.Register(techSupport1)

		// Try to add another tech support entry (should panic)
		techSupport2 := &phoneBookTestHandler{name: "tech-support"}

		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic when registering duplicate handler name")
			}
		}()

		phoneBook.Register(techSupport2) // Should panic
	})

	t.Run("phone company handles unknown departments", func(t *testing.T) {
		phoneBook := NewHandlerRegistry()

		// Phone book is empty
		handler := phoneBook.Get("tech-support")
		if handler != nil {
			t.Error("Expected nil for unknown department")
		}
	})

	t.Run("phone company uses fallback for unknown numbers", func(t *testing.T) {
		phoneBook := NewHandlerRegistry()

		// Create fallback handler
		fallback := &phoneBookTestHandler{name: "operator"}

		// Create executor with fallback
		executor := NewRegistryExecutor(phoneBook, fallback)

		// Call unknown department
		job := &Job{
			ID:          "call-002",
			HandlerName: "unknown-department",
			Source:      "customer",
		}

		// Should route to fallback
		err := executor.Execute(context.Background(), job)
		if err != nil {
			t.Fatalf("Failed to route to fallback: %v", err)
		}

		// Verify fallback handled it
		if !fallback.wasCalled {
			t.Error("Expected fallback operator to handle unknown call")
		}
	})

	t.Run("phone company lists all departments", func(t *testing.T) {
		phoneBook := NewHandlerRegistry()

		// Add multiple departments
		phoneBook.Register(&phoneBookTestHandler{name: "tech-support"})
		phoneBook.Register(&phoneBookTestHandler{name: "billing"})
		phoneBook.Register(&phoneBookTestHandler{name: "sales"})

		// Get directory listing
		names := phoneBook.Names()

		expectedNames := map[string]bool{
			"tech-support": false,
			"billing":      false,
			"sales":        false,
		}

		for _, name := range names {
			if _, exists := expectedNames[name]; exists {
				expectedNames[name] = true
			}
		}

		// Verify all departments appear in listing
		for name, found := range expectedNames {
			if !found {
				t.Errorf("Expected %s in directory listing", name)
			}
		}
	})
}
