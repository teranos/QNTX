package async

import (
	"context"
	"fmt"
	"sync"
)

// JobHandler defines the interface for executing a specific job type.
// Domain packages implement this interface to handle their job types,
// allowing the async infrastructure to remain decoupled from domain logic.
//
// Design: Dependency Inversion
// - async package defines this abstraction
// - domain packages provide implementations
// - worker pool executes jobs through handlers without knowing domain details
//
// ARCHITECTURE: Generic handler system
// - Handlers identify themselves by name (e.g., "data.batch-import", "ml.inference")
// - Handlers decode their own payload types from job.Payload
// - Infrastructure doesn't know about domain-specific data structures
type JobHandler interface {
	// Execute runs the job and returns any error encountered.
	// The handler should:
	// - Decode job.Payload into handler-specific struct
	// - Update job.Progress as work proceeds
	// - Set job.CostActual if tracking costs
	// - Return nil on success, error on failure
	//
	// Context cancellation: Handlers MUST check ctx.Done() periodically
	// and exit cleanly with checkpointed state when cancelled.
	Execute(ctx context.Context, job *Job) error

	// Name returns the handler name (e.g., "data.batch-import", "ml.inference").
	// Used for handler registration and job routing.
	Name() string
}

// HandlerRegistry manages job handlers by name.
// Thread-safe for concurrent handler registration and lookup.
//
// ARCHITECTURE: Generic handler registry
// - Handlers register by name (e.g., "data.batch-import", "ml.inference")
// - Infrastructure routes jobs by HandlerName, not domain-specific types
// - Domain packages own handler names and payload structures
type HandlerRegistry struct {
	handlers map[string]JobHandler // Handler name -> handler
	mu       sync.RWMutex
}

// NewHandlerRegistry creates an empty handler registry.
func NewHandlerRegistry() *HandlerRegistry {
	return &HandlerRegistry{
		handlers: make(map[string]JobHandler),
	}
}

// Register adds a handler using its name.
// Panics if a handler is already registered with that name.
func (r *HandlerRegistry) Register(handler JobHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	handlerName := handler.Name()
	if _, exists := r.handlers[handlerName]; exists {
		panic(fmt.Sprintf("handler already registered for name: %s", handlerName))
	}
	r.handlers[handlerName] = handler
}

// Get retrieves the handler for a handler name.
// Returns nil if no handler is registered.
func (r *HandlerRegistry) Get(handlerName string) JobHandler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.handlers[handlerName]
}

// Has checks if a handler is registered for a name.
func (r *HandlerRegistry) Has(handlerName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.handlers[handlerName]
	return exists
}

// Names returns all registered handler names.
func (r *HandlerRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	return names
}

// RegistryExecutor adapts a HandlerRegistry to the JobExecutor interface.
// This allows gradual migration: existing code uses JobExecutor,
// new handlers register with the registry.
type RegistryExecutor struct {
	registry *HandlerRegistry
	fallback JobExecutor // Optional: for unregistered job types during migration
}

// NewRegistryExecutor creates an executor backed by a handler registry.
func NewRegistryExecutor(registry *HandlerRegistry, fallback JobExecutor) *RegistryExecutor {
	return &RegistryExecutor{
		registry: registry,
		fallback: fallback,
	}
}

// Execute implements JobExecutor by dispatching to registered handlers.
func (e *RegistryExecutor) Execute(ctx context.Context, job *Job) error {
	if job.HandlerName == "" {
		return fmt.Errorf("job missing handler_name")
	}

	handler := e.registry.Get(job.HandlerName)
	if handler != nil {
		return handler.Execute(ctx, job)
	}

	// Try fallback executor for unregistered handler names
	if e.fallback != nil {
		return e.fallback.Execute(ctx, job)
	}

	return fmt.Errorf("no handler registered for handler name: %s", job.HandlerName)
}
