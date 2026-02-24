package plugin

import (
	"fmt"
	"sync"
)

// PauseController provides thread-safe pause/resume state management.
// Embed this in plugin structs to implement PausablePlugin's state tracking.
//
// Usage:
//
//	type Plugin struct {
//	    plugin.PauseController
//	    // ...
//	}
//
//	func (p *Plugin) Pause(ctx context.Context) error {
//	    if err := p.PauseController.Pause(); err != nil {
//	        return err
//	    }
//	    // plugin-specific pause logic
//	    return nil
//	}
type PauseController struct {
	mu     sync.RWMutex
	paused bool
	name   string // plugin name, used in error messages
}

// InitPauseController sets the plugin name used in error messages.
// Call this during plugin initialization.
func (pc *PauseController) InitPauseController(name string) {
	pc.name = name
}

// Pause marks the plugin as paused. Returns error if already paused.
func (pc *PauseController) Pause() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if pc.paused {
		return fmt.Errorf("%s plugin is already paused", pc.name)
	}
	pc.paused = true
	return nil
}

// Resume marks the plugin as active. Returns error if not paused.
func (pc *PauseController) Resume() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if !pc.paused {
		return fmt.Errorf("%s plugin is not paused", pc.name)
	}
	pc.paused = false
	return nil
}

// IsPaused returns whether the plugin is currently paused.
func (pc *PauseController) IsPaused() bool {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return pc.paused
}

// HealthMessage returns "operational" or "paused" message with the plugin name.
func (pc *PauseController) HealthMessage(label string) string {
	if pc.IsPaused() {
		return label + " paused"
	}
	return label + " operational"
}
