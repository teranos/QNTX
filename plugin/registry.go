package plugin

import (
	"context"
	"sort"
	"sync"

	"github.com/Masterminds/semver/v3"
	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

// Registry manages all domain plugins
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]DomainPlugin
	states  map[string]PluginState // Track state of each plugin
	version string                 // QNTX version
	logger  *zap.SugaredLogger
}

// NewRegistry creates a new plugin registry
func NewRegistry(qntxVersion string, logger *zap.SugaredLogger) *Registry {
	return &Registry{
		plugins: make(map[string]DomainPlugin),
		states:  make(map[string]PluginState),
		version: qntxVersion,
		logger:  logger,
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
		err := errors.Newf("domain plugin already registered: %s", metadata.Name)
		return errors.WithHint(err, "each plugin name must be unique - check for duplicate registrations")
	}

	// Validate version compatibility
	if err := r.validateVersion(metadata); err != nil {
		return errors.Wrapf(err, "version incompatible for %s", metadata.Name)
	}

	r.plugins[metadata.Name] = plugin
	r.states[metadata.Name] = StateStopped // Initially stopped until Initialize is called
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

// ListEnabled returns all enabled plugin names (including pre-registered ones) in sorted order
// This includes plugins that are still loading, not just fully loaded ones
func (r *Registry) ListEnabled() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.states))
	for name := range r.states {
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

// snapshotPlugins creates a thread-safe snapshot of all plugins.
// This allows operations on plugins without holding the registry lock.
func (r *Registry) snapshotPlugins() map[string]DomainPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := make(map[string]DomainPlugin, len(r.plugins))
	for name, plugin := range r.plugins {
		snapshot[name] = plugin
	}
	return snapshot
}

// InitializeAll initializes all registered plugins
func (r *Registry) InitializeAll(ctx context.Context, services ServiceRegistry) error {
	plugins := r.snapshotPlugins()

	// Initialize plugins in sorted order for determinism
	names := make([]string, 0, len(plugins))
	for name := range plugins {
		names = append(names, name)
	}
	sort.Strings(names)

	var failedPlugins []string
	for _, name := range names {
		if err := plugins[name].Initialize(ctx, services); err != nil {
			r.logger.Errorf("Failed to initialize plugin '%s': %v", name, err)
			failedPlugins = append(failedPlugins, name)
			// Mark as failed but continue with other plugins
			r.mu.Lock()
			r.states[name] = StateFailed
			r.mu.Unlock()
			continue
		}
		// Set state to running after successful initialization
		r.mu.Lock()
		r.states[name] = StateRunning
		r.mu.Unlock()
	}

	if len(failedPlugins) > 0 {
		r.logger.Warnf("Some plugins failed to initialize: %v", failedPlugins)
	}

	return nil
}

// ShutdownAll shuts down all registered plugins
func (r *Registry) ShutdownAll(ctx context.Context) error {
	plugins := r.snapshotPlugins()

	// Shutdown in reverse order
	names := make([]string, 0, len(plugins))
	for name := range plugins {
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	var errs []error
	for _, name := range names {
		if err := plugins[name].Shutdown(ctx); err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to shutdown domain plugin %s", name))
		}
	}

	if len(errs) > 0 {
		return errors.Newf("shutdown errors: %v", errs)
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

// GetState returns the current state of a plugin
func (r *Registry) GetState(name string) (PluginState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state, ok := r.states[name]
	return state, ok
}

// GetAllStates returns the states of all plugins
func (r *Registry) GetAllStates() map[string]PluginState {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]PluginState, len(r.states))
	for name, state := range r.states {
		result[name] = state
	}
	return result
}

// IsPausable checks if a plugin implements the PausablePlugin interface
func (r *Registry) IsPausable(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugin, ok := r.plugins[name]
	if !ok {
		return false
	}
	_, isPausable := plugin.(PausablePlugin)
	return isPausable
}

// Pause pauses a plugin if it implements PausablePlugin
func (r *Registry) Pause(ctx context.Context, name string) error {
	r.mu.Lock()
	plugin, ok := r.plugins[name]
	if !ok {
		r.mu.Unlock()
		err := errors.Newf("plugin not found: %s", name)
		return errors.WithHint(err, "check available plugins with 'qntx plugin list'")
	}

	state := r.states[name]
	if state != StateRunning {
		r.mu.Unlock()
		err := errors.Newf("plugin %s is not running (current state: %s)", name, state)
		return errors.WithHint(err, "plugin must be in 'running' state to pause")
	}

	pausable, ok := plugin.(PausablePlugin)
	if !ok {
		r.mu.Unlock()
		return errors.Newf("plugin %s does not support pause/resume", name)
	}
	r.mu.Unlock()

	// Call pause without holding lock
	if err := pausable.Pause(ctx); err != nil {
		return errors.Wrapf(err, "failed to pause plugin %s", name)
	}

	// Update state
	r.mu.Lock()
	r.states[name] = StatePaused
	r.mu.Unlock()

	return nil
}

// Resume resumes a paused plugin
func (r *Registry) Resume(ctx context.Context, name string) error {
	r.mu.Lock()
	plugin, ok := r.plugins[name]
	if !ok {
		r.mu.Unlock()
		err := errors.Newf("plugin not found: %s", name)
		return errors.WithHint(err, "check available plugins with 'qntx plugin list'")
	}

	state := r.states[name]
	if state != StatePaused {
		r.mu.Unlock()
		err := errors.Newf("plugin %s is not paused (current state: %s)", name, state)
		return errors.WithHint(err, "plugin must be in 'paused' state to resume")
	}

	pausable, ok := plugin.(PausablePlugin)
	if !ok {
		r.mu.Unlock()
		return errors.Newf("plugin %s does not support pause/resume", name)
	}
	r.mu.Unlock()

	// Call resume without holding lock
	if err := pausable.Resume(ctx); err != nil {
		return errors.Wrapf(err, "failed to resume plugin %s", name)
	}

	// Update state
	r.mu.Lock()
	r.states[name] = StateRunning
	r.mu.Unlock()

	return nil
}

// IsReady returns whether a plugin is ready to handle requests
func (r *Registry) IsReady(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	state, exists := r.states[name]
	return exists && state == StateRunning
}

// PreRegister reserves a plugin slot in loading state before async initialization
// This allows routes to be registered immediately while plugins load in background
func (r *Registry) PreRegister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.states[name] = StateLoading
	r.logger.Debugf("Pre-registered plugin '%s' in loading state", name)
}

// MarkReady marks a plugin as ready (StateRunning) after successful loading
// Used by async plugin loading to indicate plugin is ready to handle requests
func (r *Registry) MarkReady(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.states[name] = StateRunning
	r.logger.Debugf("Marked plugin '%s' as ready", name)
}

// validateVersion checks if plugin version is compatible with QNTX version
func (r *Registry) validateVersion(metadata Metadata) error {
	if metadata.QNTXVersion == "" {
		// No version constraint specified
		return nil
	}

	// Allow "dev" version without validation (development builds)
	if r.version == "dev" {
		r.logger.Debugf("Skipping version validation for development build (QNTX version: dev)")
		return nil
	}

	// Parse QNTX version
	qntxVer, err := semver.NewVersion(r.version)
	if err != nil {
		return errors.Wrapf(err, "invalid QNTX version %s", r.version)
	}

	// Parse version constraint
	constraint, err := semver.NewConstraint(metadata.QNTXVersion)
	if err != nil {
		wrappedErr := errors.Wrapf(err, "invalid version constraint %s", metadata.QNTXVersion)
		return errors.WithHint(wrappedErr, "plugin specifies invalid version constraint - contact plugin author")
	}

	// Check compatibility
	if !constraint.Check(qntxVer) {
		err := errors.Newf("plugin requires QNTX %s, but running %s", metadata.QNTXVersion, r.version)
		return errors.WithHint(err, "update QNTX or use a compatible plugin version")
	}

	return nil
}

// Global registry instance (Issue #4: Thread-safe initialization)
var (
	defaultRegistry *Registry
	registryMu      sync.RWMutex
)

// SetDefaultRegistry sets the global registry (Issue #4: Thread-safe)
// Panics if called more than once. The mutex ensures thread-safe check-and-set.
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
		return errors.New("default registry not initialized")
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
