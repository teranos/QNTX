package plugin

import (
	"context"
	"fmt"
	"sync"
)

// Base provides default implementations for common plugin boilerplate.
// Embed this in your plugin struct to get Metadata, Pause, Resume, IsPaused,
// Health, Shutdown, and RegisterWebSocket for free.
//
// Usage:
//
//	type Plugin struct {
//	    plugin.Base
//	    // plugin-specific fields
//	}
//
//	func NewPlugin() *Plugin {
//	    return &Plugin{
//	        Base: plugin.NewBase(plugin.Metadata{Name: "myplugin", ...}),
//	    }
//	}
//
//	func (p *Plugin) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
//	    p.Init(services)
//	    // plugin-specific initialization
//	}
type Base struct {
	meta     Metadata
	mu       sync.RWMutex
	paused   bool
	services ServiceRegistry
}

// NewBase creates a Base with the given metadata.
func NewBase(meta Metadata) Base {
	return Base{meta: meta}
}

// Init stores the ServiceRegistry. Call this from your plugin's Initialize().
func (b *Base) Init(services ServiceRegistry) {
	b.services = services
}

// Metadata returns the plugin metadata.
func (b *Base) Metadata() Metadata {
	return b.meta
}

// Services returns the ServiceRegistry provided during initialization.
func (b *Base) Services() ServiceRegistry {
	return b.services
}

// Pause temporarily suspends the plugin.
func (b *Base) Pause(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.paused {
		return fmt.Errorf("%s plugin is already paused", b.meta.Name)
	}
	b.paused = true
	b.services.Logger(b.meta.Name).Infof("%s plugin paused", b.meta.Name)
	return nil
}

// Resume restores the plugin to active operation.
func (b *Base) Resume(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.paused {
		return fmt.Errorf("%s plugin is not paused", b.meta.Name)
	}
	b.paused = false
	b.services.Logger(b.meta.Name).Infof("%s plugin resumed", b.meta.Name)
	return nil
}

// IsPaused returns whether the plugin is currently paused.
func (b *Base) IsPaused() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.paused
}

// Health returns a basic health status with pause state.
// Override this in your plugin to add domain-specific details.
func (b *Base) Health(ctx context.Context) HealthStatus {
	b.mu.RLock()
	paused := b.paused
	b.mu.RUnlock()

	message := b.meta.Name + " plugin operational"
	if paused {
		message = b.meta.Name + " plugin paused"
	}

	return HealthStatus{
		Healthy: true,
		Paused:  paused,
		Message: message,
	}
}

// Shutdown is a no-op default. Override if your plugin needs cleanup.
func (b *Base) Shutdown(ctx context.Context) error {
	b.services.Logger(b.meta.Name).Infof("%s plugin shutting down", b.meta.Name)
	return nil
}

// RegisterWebSocket returns nil. Override if your plugin uses WebSockets.
func (b *Base) RegisterWebSocket() (map[string]WebSocketHandler, error) {
	return nil, nil
}
