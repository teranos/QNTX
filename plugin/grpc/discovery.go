package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// PluginConfig represents configuration for a plugin.
type PluginConfig struct {
	// Name is the plugin identifier
	Name string `toml:"name"`

	// Enabled controls whether the plugin is loaded
	Enabled bool `toml:"enabled"`

	// Address is the gRPC address (host:port) if the plugin is already running
	// If empty, QNTX will launch the plugin binary
	Address string `toml:"address"`

	// Binary is the path to the plugin binary
	// If relative, it's resolved relative to ~/.qntx/plugins/
	Binary string `toml:"binary"`

	// Args are additional arguments passed to the plugin binary
	Args []string `toml:"args"`

	// Env are environment variables for the plugin process
	Env map[string]string `toml:"env"`

	// AutoStart controls whether to automatically start the plugin
	AutoStart bool `toml:"auto_start"`

	// Config contains plugin-specific runtime configuration
	// These key-value pairs are passed to the plugin via InitializeRequest
	Config map[string]string `toml:"config"`
}

// PluginManager manages plugin processes and connections.
type PluginManager struct {
	mu       sync.RWMutex
	plugins  map[string]*managedPlugin
	logger   *zap.SugaredLogger
	basePort int
}

// managedPlugin tracks a running plugin.
type managedPlugin struct {
	config  PluginConfig
	client  *ExternalDomainProxy
	process *os.Process
	port    int
}

const (
	// DefaultPluginBasePort is the starting port for plugin allocation
	// Uses 38700 to avoid conflicts with common development tools
	DefaultPluginBasePort = 38700
)

// NewPluginManager creates a new plugin manager.
func NewPluginManager(logger *zap.SugaredLogger) *PluginManager {
	return &PluginManager{
		plugins:  make(map[string]*managedPlugin),
		logger:   logger,
		basePort: DefaultPluginBasePort,
	}
}

// Global plugin manager instance (similar to plugin.Registry pattern)
var (
	defaultPluginManager *PluginManager
	pluginManagerMu      sync.RWMutex
)

// SetDefaultPluginManager sets the global plugin manager instance
func SetDefaultPluginManager(manager *PluginManager) {
	pluginManagerMu.Lock()
	defer pluginManagerMu.Unlock()
	defaultPluginManager = manager
}

// GetDefaultPluginManager returns the global plugin manager instance
func GetDefaultPluginManager() *PluginManager {
	pluginManagerMu.RLock()
	defer pluginManagerMu.RUnlock()
	return defaultPluginManager
}

// LoadPlugins loads and connects to plugins from configuration.
// If a plugin fails to load, it logs the error and continues with remaining plugins.
func (m *PluginManager) LoadPlugins(ctx context.Context, configs []PluginConfig) error {
	var failedPlugins []string

	for _, config := range configs {
		if !config.Enabled {
			m.logger.Infow("Skipping disabled plugin", "name", config.Name)
			continue
		}

		if err := m.loadPlugin(ctx, config); err != nil {
			m.logger.Errorf("Failed to load plugin '%s' (binary=%s, address=%s): %v",
				config.Name, config.Binary, config.Address, err)
			failedPlugins = append(failedPlugins, config.Name)
			continue
		}
	}

	if len(failedPlugins) > 0 {
		m.logger.Warnf("Some plugins failed to load: %v", failedPlugins)
		// Don't return error - plugin system is resilient, continue with loaded plugins
	}

	return nil
}

// loadPlugin loads a single plugin.
func (m *PluginManager) loadPlugin(ctx context.Context, config PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already loaded
	if _, exists := m.plugins[config.Name]; exists {
		err := errors.Newf("plugin already loaded: %s", config.Name)
		return errors.WithHint(err, "check for duplicate plugin entries in am.plugins.toml or ~/.qntx/plugins/")
	}

	var addr string
	var process *os.Process
	var port int

	if config.Address != "" {
		// Plugin is already running at the specified address
		addr = config.Address
		m.logger.Infow("Connecting to existing plugin", "name", config.Name, "address", addr)
	} else if config.Binary != "" && config.AutoStart {
		// Launch the plugin binary
		port = m.allocatePort()
		// Use explicit IPv4 127.0.0.1 instead of "localhost" to avoid IPv6 [::1] resolution
		addr = fmt.Sprintf("127.0.0.1:%d", port)

		var err error
		var actualPort int
		process, actualPort, err = m.launchPlugin(ctx, config, port)
		if err != nil {
			return errors.Wrapf(err, "failed to launch plugin %s (binary=%s, port=%d)",
				config.Name, config.Binary, port)
		}

		// Use the actual port if plugin reported a different one (due to auto-increment)
		if actualPort != 0 && actualPort != port {
			port = actualPort
			addr = fmt.Sprintf("127.0.0.1:%d", port)
			m.logger.Infow("Plugin bound to different port", "name", config.Name, "actual_port", actualPort)
		}

		m.logger.Infof("Started '%s' plugin process (pid=%d, port=%d, addr=%s)",
			config.Name, process.Pid, port, addr)

		// Wait for plugin to be ready (5 second timeout for faster failure detection)
		if err := m.waitForPlugin(ctx, addr, 5*time.Second); err != nil {
			process.Kill()
			return errors.Wrapf(err, "plugin %s failed to start (binary=%s, addr=%s, pid=%d)",
				config.Name, config.Binary, addr, process.Pid)
		}
	} else if config.Binary != "" {
		// Binary specified but auto_start is false
		m.logger.Warnw("Plugin binary specified but auto_start is false",
			"name", config.Name,
			"binary", config.Binary,
		)
		return nil
	} else {
		err := errors.Newf("plugin %s: either address or binary must be specified", config.Name)
		return errors.WithHint(err, "set 'address' for remote plugins or 'binary' with 'auto_start=true' in plugin config")
	}

	// Connect to the plugin
	client, err := NewExternalDomainProxy(addr, m.logger)
	if err != nil {
		if process != nil {
			process.Kill()
		}
		return errors.Wrapf(err, "failed to connect to plugin %s at %s", config.Name, addr)
	}

	// Validate plugin metadata matches config
	actualName := client.Metadata().Name
	if actualName != config.Name {
		if process != nil {
			process.Kill()
		}
		err := errors.Newf("plugin metadata mismatch: binary at %s reports name='%s' but config expects '%s'",
			config.Binary, actualName, config.Name)
		return errors.WithHint(err, "verify the correct plugin binary is installed or update the plugin name in config")
	}

	m.plugins[config.Name] = &managedPlugin{
		config:  config,
		client:  client,
		process: process,
		port:    port,
	}

	m.logger.Infof("Plugin '%s' v%s loaded and ready - %s",
		config.Name, client.Metadata().Version, client.Metadata().Description)

	return nil
}

// allocatePort finds the next available port for a plugin.
// Returns the next port after the highest currently allocated port.
func (m *PluginManager) allocatePort() int {
	port := m.basePort

	// Find the highest allocated port
	// Note: Map iteration order is non-deterministic, but we're only finding max value
	maxPort := m.basePort - 1
	for _, p := range m.plugins {
		if p.port > maxPort {
			maxPort = p.port
		}
	}

	// Next port is one after the current maximum
	if maxPort >= m.basePort {
		port = maxPort + 1
	}

	return port
}

// launchPlugin starts a plugin binary and returns the process and actual port.
// If the plugin outputs QNTX_PLUGIN_PORT=XXXX, that port is returned instead of the requested port.
func (m *PluginManager) launchPlugin(ctx context.Context, config PluginConfig, port int) (*os.Process, int, error) {
	binary := config.Binary

	// Resolve relative paths
	if !filepath.IsAbs(binary) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, 0, errors.Wrapf(err, "failed to get home directory for plugin %s", config.Name)
		}
		binary = filepath.Join(home, ".qntx", "plugins", binary)
	}

	// Check if binary exists
	if _, err := os.Stat(binary); os.IsNotExist(err) {
		err := errors.Newf("plugin binary not found for %s: %s", config.Name, binary)
		return nil, 0, errors.WithHint(err, "install the plugin binary to ~/.qntx/plugins/ or specify the full path in config")
	}

	// Build command arguments
	args := append([]string{"--port", strconv.Itoa(port)}, config.Args...)

	// NOTE: Using exec.Command instead of exec.CommandContext intentionally.
	// This prevents plugins from being killed when parent context is cancelled (e.g., during
	// graceful shutdown), allowing proper plugin shutdown via gRPC Shutdown() call.
	//
	// TRADEOFF: If QNTX crashes or is killed (SIGKILL), plugin processes become orphans.
	// TODO: Consider implementing process group management or pidfile tracking for cleanup
	// of orphaned plugins on next QNTX startup.
	cmd := exec.Command(binary, args...)

	// Set environment
	cmd.Env = os.Environ()
	for key, value := range config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	// Create a channel to receive the actual port from plugin output
	portChan := make(chan int, 1)

	// Capture output for debugging and port discovery
	cmd.Stdout = &pluginLogger{
		logger:   m.logger,
		name:     config.Name,
		level:    "info",
		portChan: portChan,
	}
	cmd.Stderr = &pluginLogger{
		logger: m.logger,
		name:   config.Name,
		level:  "error",
		// Don't pass portChan to stderr - port announcement should be on stdout
	}

	if err := cmd.Start(); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to start plugin %s (binary=%s, args=%v)",
			config.Name, binary, args)
	}

	// Wait for the port announcement with a short timeout (2 seconds)
	// The plugin should announce its port almost immediately after binding
	actualPort := port // Default to requested port
	select {
	case discoveredPort := <-portChan:
		actualPort = discoveredPort
		m.logger.Infow("Discovered plugin port from stdout",
			"name", config.Name,
			"requested_port", port,
			"actual_port", actualPort)
	case <-time.After(2 * time.Second):
		// No port announcement - plugin is using the requested port
		// This is normal for older plugins that don't support auto-increment
		m.logger.Debugw("No port announcement from plugin, assuming requested port",
			"name", config.Name,
			"port", port)
	}

	return cmd.Process, actualPort, nil
}

// waitForPlugin waits for a plugin's gRPC server to become ready.
// This polls the gRPC metadata endpoint rather than just checking TCP connectivity
// to ensure the plugin is actually ready to handle requests.
func (m *PluginManager) waitForPlugin(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Try gRPC connection with short timeout
		connCtx, cancel := context.WithTimeout(ctx, time.Second)
		conn, err := grpc.DialContext(connCtx, addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		cancel()

		if err == nil {
			// Connection succeeded, verify gRPC service is ready by calling metadata
			client := protocol.NewDomainPluginServiceClient(conn)
			metaCtx, metaCancel := context.WithTimeout(ctx, time.Second)
			_, metaErr := client.Metadata(metaCtx, &protocol.Empty{})
			metaCancel()
			conn.Close()

			if metaErr == nil {
				// gRPC service is ready
				return nil
			}
			// gRPC service not ready yet, continue waiting
		}

		time.Sleep(100 * time.Millisecond)
	}

	err := errors.Newf("timeout waiting for plugin gRPC service at %s", addr)
	return errors.WithHint(err, "check plugin logs for startup errors, increase timeout, or verify the plugin binary is compatible")
}

// GetPlugin returns a connected plugin as a DomainPlugin.
// The returned plugin can be registered with the Registry like any other plugin.
func (m *PluginManager) GetPlugin(name string) (plugin.DomainPlugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if p, ok := m.plugins[name]; ok {
		return p.client, true
	}
	return nil, false
}

// GetAllPlugins returns all connected plugins as DomainPlugin instances.
// These can be registered with the Registry.
func (m *PluginManager) GetAllPlugins() []plugin.DomainPlugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugins := make([]plugin.DomainPlugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		plugins = append(plugins, p.client)
	}
	return plugins
}

// ConfigureWebSocket sets WebSocket configuration on all loaded plugins.
// This should be called after LoadPlugins to configure keepalive and origin validation.
func (m *PluginManager) ConfigureWebSocket(keepalive KeepaliveConfig, wsConfig WebSocketConfig) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, p := range m.plugins {
		p.client.SetWebSocketConfig(keepalive, wsConfig)
	}
	m.logger.Infow("WebSocket configuration applied to plugins",
		"keepalive_enabled", keepalive.Enabled,
		"ping_interval", keepalive.PingInterval,
		"allowed_origins_count", len(wsConfig.AllowedOrigins),
	)
}

// ReinitializePlugin reinitializes a plugin with updated configuration.
// This is called after plugin config is updated via the UI.
// The plugin must already be loaded and running.
func (m *PluginManager) ReinitializePlugin(ctx context.Context, pluginName string, services plugin.ServiceRegistry) error {
	m.mu.RLock()
	p, exists := m.plugins[pluginName]
	m.mu.RUnlock()

	if !exists {
		err := errors.Newf("plugin not loaded: %s", pluginName)
		return errors.WithHintf(err, "ensure plugin '%s' is enabled and running before reinitializing", pluginName)
	}

	// Call Initialize again with updated config from ServiceRegistry
	if err := p.client.Initialize(ctx, services); err != nil {
		wrappedErr := errors.Wrapf(err, "failed to reinitialize plugin %s", pluginName)
		return errors.WithHintf(wrappedErr, "check plugin logs and verify configuration is valid")
	}

	m.logger.Infof("Successfully reinitialized plugin '%s' with updated configuration", pluginName)
	return nil
}

// Shutdown stops all managed plugins.
func (m *PluginManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	for name, p := range m.plugins {
		// Shutdown the plugin via gRPC
		if err := p.client.Shutdown(ctx); err != nil {
			m.logger.Warnw("Plugin shutdown error", "name", name, "error", err)
			errs = append(errs, err)
		}

		// Kill the process if we launched it
		if p.process != nil {
			if err := p.process.Signal(os.Interrupt); err != nil {
				m.logger.Warnw("Failed to signal plugin process", "name", name, "error", err)
				// Try harder
				p.process.Kill()
			}
		}
	}

	m.plugins = make(map[string]*managedPlugin)

	if len(errs) > 0 {
		return errors.Newf("shutdown errors: %v", errs)
	}
	return nil
}

// pluginLogger logs plugin output and captures port announcements.
type pluginLogger struct {
	logger   *zap.SugaredLogger
	name     string
	level    string
	buf      strings.Builder
	portChan chan int // Optional channel to send discovered port
}

func (l *pluginLogger) Write(p []byte) (n int, err error) {
	l.buf.Write(p)
	for {
		line, rest, found := strings.Cut(l.buf.String(), "\n")
		if !found {
			break
		}
		l.buf.Reset()
		l.buf.WriteString(rest)

		if line = strings.TrimSpace(line); line != "" {
			// Check for port announcement (QNTX_PLUGIN_PORT=9001)
			if l.portChan != nil && strings.HasPrefix(line, "QNTX_PLUGIN_PORT=") {
				portStr := strings.TrimPrefix(line, "QNTX_PLUGIN_PORT=")
				if port, err := strconv.Atoi(portStr); err == nil {
					select {
					case l.portChan <- port:
						// Port sent successfully
					default:
						// Channel full or closed, ignore
					}
				}
				// Don't log the raw QNTX_PLUGIN_PORT line - it's internal protocol
				continue
			}

			// Try to parse JSON log entry and extract actual level
			var logEntry struct {
				Level string `json:"level"`
			}
			actualLevel := l.level // Default to configured level (stdout=info, stderr=error)
			if err := json.Unmarshal([]byte(line), &logEntry); err == nil && logEntry.Level != "" {
				actualLevel = logEntry.Level
			}

			// Log at the actual level from JSON, or fallback to configured level for non-JSON
			switch actualLevel {
			case "debug":
				l.logger.Debugf("[%s] %s", l.name, line)
			case "info":
				l.logger.Infof("[%s] %s", l.name, line)
			case "warn":
				l.logger.Warnf("[%s] %s", l.name, line)
			case "error":
				l.logger.Errorf("[%s stderr] %s", l.name, line)
			default:
				// Unknown level or non-JSON
				l.logger.Warnf("[%s UNKNOWN] %s", l.name, line)
			}
		}
	}
	return len(p), nil
}

// DiscoverPlugins scans for plugin configuration files.
func DiscoverPlugins(configDir string) ([]PluginConfig, error) {
	var configs []PluginConfig

	// Look for am.plugins.toml or individual am.<name>.plugin.toml files
	pluginsFile := filepath.Join(configDir, "am.plugins.toml")
	if _, err := os.Stat(pluginsFile); err == nil {
		// Parse plugins configuration file
		data, err := os.ReadFile(pluginsFile)
		if err == nil {
			// The file should contain a map of plugin configs
			var pluginsConfig struct {
				Plugins map[string]PluginConfig `toml:"plugins"`
			}
			if err := toml.Unmarshal(data, &pluginsConfig); err == nil {
				for name, config := range pluginsConfig.Plugins {
					// Ensure the name is set
					if config.Name == "" {
						config.Name = name
					}
					// Resolve relative binary paths
					if config.Binary != "" && !filepath.IsAbs(config.Binary) {
						config.Binary = filepath.Join(configDir, "plugins", config.Binary)
					}
					configs = append(configs, config)
				}
			}
		}
	}

	// Scan plugins directory for binaries
	pluginsDir := filepath.Join(configDir, "plugins")
	if entries, err := os.ReadDir(pluginsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			// Skip non-executables
			if strings.HasSuffix(name, ".toml") || strings.HasSuffix(name, ".md") {
				continue
			}

			// Check if there's a corresponding config file
			configFile := filepath.Join(pluginsDir, name+".toml")
			if _, err := os.Stat(configFile); err == nil {
				// Parse plugin-specific config
				data, err := os.ReadFile(configFile)
				if err == nil {
					var config PluginConfig
					if err := toml.Unmarshal(data, &config); err == nil {
						// Ensure name is set
						if config.Name == "" {
							config.Name = name
						}
						// Ensure binary is set
						if config.Binary == "" {
							config.Binary = filepath.Join(pluginsDir, name)
						} else if !filepath.IsAbs(config.Binary) {
							config.Binary = filepath.Join(pluginsDir, config.Binary)
						}
						configs = append(configs, config)
						continue
					}
				}
				// Fall through to defaults if parsing failed
			}

			// Binary without config or failed to parse - add with defaults
			configs = append(configs, PluginConfig{
				Name:      name,
				Enabled:   true,
				Binary:    filepath.Join(pluginsDir, name),
				AutoStart: true,
			})
		}
	}

	return configs, nil
}
