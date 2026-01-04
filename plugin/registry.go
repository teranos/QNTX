package plugin

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/Masterminds/semver/v3"
)

// Registry manages all domain plugins
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]DomainPlugin
	version string // QNTX version
}

// NewRegistry creates a new plugin registry
func NewRegistry(qntxVersion string) *Registry {
	return &Registry{
		plugins: make(map[string]DomainPlugin),
		version: qntxVersion,
	}
}

// Register registers a domain plugin
// Returns error if plugin name conflicts or version incompatible
func (r *Registry) Register(plugin DomainPlugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	metadata := plugin.Metadata()

	// Check for name conflicts
	if _, exists := r.plugins[metadata.Name]; exists {
		return fmt.Errorf("domain plugin already registered: %s", metadata.Name)
	}

	// Validate version compatibility
	if err := r.validateVersion(metadata); err != nil {
		return fmt.Errorf("version incompatible for %s: %w", metadata.Name, err)
	}

	r.plugins[metadata.Name] = plugin
	return nil
}

// Get retrieves a domain plugin by name
func (r *Registry) Get(name string) (DomainPlugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	plugin, ok := r.plugins[name]
	return plugin, ok
}

// List returns all registered domain plugin names in sorted order
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetAll returns all registered plugins
func (r *Registry) GetAll() map[string]DomainPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]DomainPlugin, len(r.plugins))
	for name, plugin := range r.plugins {
		result[name] = plugin
	}
	return result
}

// InitializeAll initializes all registered plugins
func (r *Registry) InitializeAll(ctx context.Context, services ServiceRegistry) error {
	r.mu.RLock()
	plugins := make(map[string]DomainPlugin, len(r.plugins))
	for name, plugin := range r.plugins {
		plugins[name] = plugin
	}
	r.mu.RUnlock()

	// Initialize plugins in sorted order for determinism
	names := make([]string, 0, len(plugins))
	for name := range plugins {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := plugins[name].Initialize(ctx, services); err != nil {
			return fmt.Errorf("failed to initialize domain plugin %s: %w", name, err)
		}
	}

	return nil
}

// ShutdownAll shuts down all registered plugins
func (r *Registry) ShutdownAll(ctx context.Context) error {
	r.mu.RLock()
	plugins := make(map[string]DomainPlugin, len(r.plugins))
	for name, plugin := range r.plugins {
		plugins[name] = plugin
	}
	r.mu.RUnlock()

	// Shutdown in reverse order
	names := make([]string, 0, len(plugins))
	for name := range plugins {
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	var errs []error
	for _, name := range names {
		if err := plugins[name].Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to shutdown domain plugin %s: %w", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	return nil
}

// HealthCheckAll checks health of all plugins
func (r *Registry) HealthCheckAll(ctx context.Context) map[string]HealthStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	results := make(map[string]HealthStatus, len(r.plugins))
	for name, plugin := range r.plugins {
		results[name] = plugin.Health(ctx)
	}
	return results
}

// validateVersion checks if plugin version is compatible with QNTX version
func (r *Registry) validateVersion(metadata Metadata) error {
	if metadata.QNTXVersion == "" {
		// No version constraint specified
		return nil
	}

	// Parse QNTX version
	qntxVer, err := semver.NewVersion(r.version)
	if err != nil {
		return fmt.Errorf("invalid QNTX version %s: %w", r.version, err)
	}

	// Parse version constraint
	constraint, err := semver.NewConstraint(metadata.QNTXVersion)
	if err != nil {
		return fmt.Errorf("invalid version constraint %s: %w", metadata.QNTXVersion, err)
	}

	// Check compatibility
	if !constraint.Check(qntxVer) {
		return fmt.Errorf("plugin requires QNTX %s, but running %s", metadata.QNTXVersion, r.version)
	}

	return nil
}

// Global registry instance (Issue #4: Thread-safe initialization)
var (
	defaultRegistry *Registry
	registryOnce    sync.Once
	registryMu      sync.RWMutex
)

// SetDefaultRegistry sets the global registry (Issue #4: Thread-safe with sync.Once)
func SetDefaultRegistry(registry *Registry) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if defaultRegistry != nil {
		panic("default registry already initialized - call SetDefaultRegistry only once")
	}
	defaultRegistry = registry
}

// GetDefaultRegistry returns the global registry (Issue #4: Thread-safe read)
func GetDefaultRegistry() *Registry {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return defaultRegistry
}

// Register registers a plugin with the global registry (Issue #4: Thread-safe)
func Register(plugin DomainPlugin) error {
	registryMu.RLock()
	defer registryMu.RUnlock()

	if defaultRegistry == nil {
		return fmt.Errorf("default registry not initialized")
	}
	return defaultRegistry.Register(plugin)
}

// Get retrieves a plugin from the global registry (Issue #4: Thread-safe)
func Get(name string) (DomainPlugin, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	if defaultRegistry == nil {
		return nil, false
	}
	return defaultRegistry.Get(name)
}

// List returns all plugin names from the global registry (Issue #4: Thread-safe)
func List() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	if defaultRegistry == nil {
		return nil
	}
	return defaultRegistry.List()
}
