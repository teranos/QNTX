package grpc

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap"
)

// PluginConfig represents configuration for an external plugin.
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
}

// PluginManager manages external plugin processes and connections.
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

// NewPluginManager creates a new plugin manager.
func NewPluginManager(logger *zap.SugaredLogger) *PluginManager {
	return &PluginManager{
		plugins:  make(map[string]*managedPlugin),
		logger:   logger,
		basePort: 9000, // External plugins start on port 9000+
	}
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
	}

	return nil
}

// loadPlugin loads a single plugin.
func (m *PluginManager) loadPlugin(ctx context.Context, config PluginConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already loaded
	if _, exists := m.plugins[config.Name]; exists {
		return fmt.Errorf("plugin already loaded: %s", config.Name)
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
		addr = fmt.Sprintf("localhost:%d", port)

		var err error
		process, err = m.launchPlugin(ctx, config, port)
		if err != nil {
			return fmt.Errorf("failed to launch plugin %s (binary=%s, port=%d): %w",
				config.Name, config.Binary, port, err)
		}
		m.logger.Infof("Started '%s' plugin process (pid=%d, port=%d, addr=%s)",
			config.Name, process.Pid, port, addr)

		// Wait for plugin to be ready (5 second timeout for faster failure detection)
		if err := m.waitForPlugin(ctx, addr, 5*time.Second); err != nil {
			process.Kill()
			return fmt.Errorf("plugin %s failed to start (binary=%s, addr=%s, pid=%d): %w",
				config.Name, config.Binary, addr, process.Pid, err)
		}
	} else if config.Binary != "" {
		// Binary specified but auto_start is false
		m.logger.Warnw("Plugin binary specified but auto_start is false",
			"name", config.Name,
			"binary", config.Binary,
		)
		return nil
	} else {
		return fmt.Errorf("plugin %s: either address or binary must be specified", config.Name)
	}

	// Connect to the plugin
	client, err := NewExternalDomainProxy(addr, m.logger)
	if err != nil {
		if process != nil {
			process.Kill()
		}
		return fmt.Errorf("failed to connect to plugin %s at %s: %w", config.Name, addr, err)
	}

	// Validate plugin metadata matches config
	actualName := client.Metadata().Name
	if actualName != config.Name {
		if process != nil {
			process.Kill()
		}
		return fmt.Errorf("plugin metadata mismatch: binary at %s reports name='%s' but config expects '%s' (wrong binary installed?)",
			config.Binary, actualName, config.Name)
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
// Uses deterministic iteration to ensure consistent port allocation.
func (m *PluginManager) allocatePort() int {
	port := m.basePort

	// Collect all allocated ports and find max deterministically
	// Map iteration is non-deterministic in Go, so we track the max explicitly
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

// launchPlugin starts a plugin binary.
func (m *PluginManager) launchPlugin(ctx context.Context, config PluginConfig, port int) (*os.Process, error) {
	binary := config.Binary

	// Resolve relative paths
	if !filepath.IsAbs(binary) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory for plugin %s: %w", config.Name, err)
		}
		binary = filepath.Join(home, ".qntx", "plugins", binary)
	}

	// Check if binary exists
	if _, err := os.Stat(binary); os.IsNotExist(err) {
		return nil, fmt.Errorf("plugin binary not found for %s: %s", config.Name, binary)
	}

	// Build command arguments
	args := append([]string{"--port", strconv.Itoa(port)}, config.Args...)

	cmd := exec.CommandContext(ctx, binary, args...)

	// Set environment
	cmd.Env = os.Environ()
	for key, value := range config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	// Capture output for debugging
	cmd.Stdout = &pluginLogger{logger: m.logger, name: config.Name, level: "info"}
	cmd.Stderr = &pluginLogger{logger: m.logger, name: config.Name, level: "error"}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start plugin %s (binary=%s, args=%v): %w",
			config.Name, binary, args, err)
	}

	return cmd.Process, nil
}

// waitForPlugin waits for a plugin to become available.
func (m *PluginManager) waitForPlugin(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Try to connect
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
			return nil
		}

		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for plugin at %s", addr)
}

// GetPlugin returns a connected plugin as a DomainPlugin.
// The returned plugin can be registered with the Registry like any built-in plugin.
func (m *PluginManager) GetPlugin(name string) (plugin.DomainPlugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if p, ok := m.plugins[name]; ok {
		return p.client, true
	}
	return nil, false
}

// GetAllPlugins returns all connected plugins as DomainPlugin instances.
// These can be registered with the Registry alongside built-in plugins.
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
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}

// pluginLogger logs plugin output.
type pluginLogger struct {
	logger *zap.SugaredLogger
	name   string
	level  string
	buf    strings.Builder
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
			if l.level == "error" {
				l.logger.Errorf("[%s stderr] %s", l.name, line)
			} else {
				l.logger.Infof("[%s] %s", l.name, line)
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
