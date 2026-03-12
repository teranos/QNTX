package grpc

import (
	"context"
	"encoding/json"
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
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// isPackageJSONPlugin checks if a directory contains a package.json with qntx-plugin marker
func isPackageJSONPlugin(path string) bool {
	// If path is a directory, check for package.json
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	var packageJSONPath string
	if info.IsDir() {
		packageJSONPath = filepath.Join(path, "package.json")
	} else {
		// If it's a file, check the parent directory
		packageJSONPath = filepath.Join(filepath.Dir(path), "package.json")
	}

	data, err := os.ReadFile(packageJSONPath)
	if err != nil {
		return false
	}

	var pkg struct {
		QNTXPlugin bool `json:"qntx-plugin"`
	}

	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}

	return pkg.QNTXPlugin
}

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
	mu                sync.RWMutex
	plugins           map[string]*managedPlugin
	failedPlugins     map[string]string // plugin name → error message for plugins that failed to load
	logger            *zap.SugaredLogger
	basePort          int
	nextPort          int        // Track the next port to allocate
	portMu            sync.Mutex // Separate mutex for port allocation
	typescriptRuntime string     // Path to TypeScript runtime (main.ts)
	shutdownCtx       context.Context    // cancelled on Shutdown to stop retry goroutines
	shutdownCancel    context.CancelFunc
}

// managedPlugin tracks a running plugin.
type managedPlugin struct {
	config       PluginConfig
	client       *ExternalDomainProxy
	process      *os.Process
	port         int
	stdoutLogger *pluginLogger
	stderrLogger *pluginLogger
	logBuffer    *LogBuffer
}

const (
	// DefaultPluginBasePort is the starting port for plugin allocation
	// Uses 38700 to avoid conflicts with common development tools
	DefaultPluginBasePort = 38700
)

// NewPluginManager creates a new plugin manager.
func NewPluginManager(logger *zap.SugaredLogger, typescriptRuntime string) *PluginManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &PluginManager{
		plugins:           make(map[string]*managedPlugin),
		failedPlugins:     make(map[string]string),
		logger:            logger,
		basePort:          DefaultPluginBasePort,
		nextPort:          DefaultPluginBasePort,
		typescriptRuntime: typescriptRuntime,
		shutdownCtx:       ctx,
		shutdownCancel:    cancel,
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
// Enabled plugins are retried forever — enabled means the operator is certain
// this plugin must run. Disabled plugins are skipped entirely.
func (m *PluginManager) LoadPlugins(ctx context.Context, configs []PluginConfig) error {
	for _, config := range configs {
		if !config.Enabled {
			m.logger.Infow("Skipping disabled plugin", "name", config.Name)
			continue
		}

		if err := m.loadPlugin(ctx, config); err != nil {
			m.logger.Errorf("Failed to load plugin '%s' (binary=%s, address=%s): %v",
				config.Name, config.Binary, config.Address, err)
			m.failedPlugins[config.Name] = err.Error()

			// Enabled means forever — retry until server shuts down
			go m.retryPluginForever(m.shutdownCtx, config)
			continue
		}
	}

	return nil
}

// retryPluginForever kills and relaunches a plugin process until it loads.
// Each cycle: launch process → try connecting 4 times (1s, 3s, 9s, 27s) → if all fail, kill and relaunch.
func (m *PluginManager) retryPluginForever(ctx context.Context, config PluginConfig) {
	cycle := 0
	for {
		cycle++
		select {
		case <-ctx.Done():
			m.logger.Warnf("Context cancelled, stopping retry for plugin '%s'", config.Name)
			return
		default:
		}

		m.logger.Infof("Restarting plugin '%s' process (cycle %d)", config.Name, cycle)

		// Clean up previous state so loadPlugin doesn't see "already loaded"
		m.mu.Lock()
		if old, exists := m.plugins[config.Name]; exists {
			if old.process != nil {
				old.process.Kill()
			}
			delete(m.plugins, config.Name)
		}
		m.mu.Unlock()

		err := m.loadPlugin(ctx, config)
		if err == nil {
			delete(m.failedPlugins, config.Name)
			m.logger.Infof("Plugin '%s' loaded successfully on restart cycle %d", config.Name, cycle)
			return
		}

		m.logger.Errorf("Plugin '%s' restart cycle %d failed: %v", config.Name, cycle, err)
		m.failedPlugins[config.Name] = err.Error()

		// Wait before next full restart cycle
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// loadPlugin loads a single plugin.
// Lock is only held for state checks and the final registration — all I/O
// (process launch, connection attempts, metadata fetch) runs unlocked.
func (m *PluginManager) loadPlugin(ctx context.Context, config PluginConfig) error {
	// Check if already loaded
	m.mu.RLock()
	_, exists := m.plugins[config.Name]
	m.mu.RUnlock()
	if exists {
		err := errors.Newf("plugin already loaded: %s", config.Name)
		return errors.WithHint(err, "check for duplicate plugin entries in am.plugins.toml or ~/.qntx/plugins/")
	}

	var addr string
	var process *os.Process
	var port int
	var stdoutLogger *pluginLogger
	var stderrLogger *pluginLogger
	var logBuf *LogBuffer

	if config.Address != "" {
		addr = config.Address
		m.logger.Infow("Connecting to existing plugin", "name", config.Name, "address", addr)
	} else if config.Binary != "" && config.AutoStart {
		port = m.allocatePort()
		addr = fmt.Sprintf("127.0.0.1:%d", port)

		var err error
		var actualPort int
		process, actualPort, stdoutLogger, stderrLogger, logBuf, err = m.launchPlugin(ctx, config, port)
		if err != nil {
			return errors.Wrapf(err, "failed to launch plugin %s (binary=%s, port=%d)",
				config.Name, config.Binary, port)
		}

		if actualPort != 0 && actualPort != port {
			port = actualPort
			addr = fmt.Sprintf("127.0.0.1:%d", port)
			m.logger.Infow("Plugin bound to different port", "name", config.Name, "actual_port", actualPort)
		}

		m.logger.Infof("Started '%s' plugin process (pid=%d, port=%d, addr=%s)",
			config.Name, process.Pid, port, addr)

		if err := m.waitForPlugin(ctx, config.Name, addr, 5*time.Second); err != nil {
			process.Kill()
			return errors.Wrapf(err, "plugin %s failed to start (binary=%s, addr=%s, pid=%d)",
				config.Name, config.Binary, addr, process.Pid)
		}
	} else if config.Binary != "" {
		m.logger.Warnw("Plugin binary specified but auto_start is false",
			"name", config.Name,
			"binary", config.Binary,
		)
		return nil
	} else {
		err := errors.Newf("plugin %s: either address or binary must be specified", config.Name)
		return errors.WithHint(err, "set 'address' for remote plugins or 'binary' with 'auto_start=true' in plugin config")
	}

	// Connect to the plugin with retry: 1s, 3s, 9s, 27s backoff.
	// If all attempts fail, caller (retryPluginForever) kills the process and relaunches.
	connectBackoffs := []time.Duration{1 * time.Second, 3 * time.Second, 9 * time.Second}
	var client *ExternalDomainProxy
	var connectErr error
	for attempt, backoff := range connectBackoffs {
		client, connectErr = NewExternalDomainProxy(addr, m.logger)
		if connectErr == nil {
			break
		}
		m.logger.Warnf("Connection attempt %d/%d to plugin '%s' at %s failed: %v",
			attempt+1, len(connectBackoffs), config.Name, addr, connectErr)
		if attempt < len(connectBackoffs)-1 {
			time.Sleep(backoff)
		}
	}
	if connectErr != nil {
		if process != nil {
			process.Kill()
		}
		return errors.Wrapf(connectErr, "failed to connect to plugin %s at %s after %d attempts",
			config.Name, addr, len(connectBackoffs))
	}

	// Validate plugin metadata matches config
	meta := client.Metadata()
	if meta.Name != config.Name {
		if process != nil {
			process.Kill()
		}
		err := errors.Newf("plugin metadata mismatch: binary at %s reports name='%s' but config expects '%s'",
			config.Binary, meta.Name, config.Name)
		return errors.WithHint(err, "verify the correct plugin binary is installed or update the plugin name in config")
	}

	// Update logger names with version information
	nameWithVersion := fmt.Sprintf("%s v%s", meta.Name, meta.Version)
	if stdoutLogger != nil {
		stdoutLogger.updateName(nameWithVersion)
	}
	if stderrLogger != nil {
		stderrLogger.updateName(nameWithVersion)
	}

	// Register — lock only for the final state write
	m.mu.Lock()
	m.plugins[config.Name] = &managedPlugin{
		config:       config,
		client:       client,
		process:      process,
		port:         port,
		stdoutLogger: stdoutLogger,
		stderrLogger: stderrLogger,
		logBuffer:    logBuf,
	}
	m.mu.Unlock()

	m.logger.Infof("Plugin '%s' v%s loaded and ready - %s",
		config.Name, meta.Version, meta.Description)

	return nil
}

// allocatePort finds the next available port for a plugin.
// Probes each port to ensure it's actually free before returning it.
// This prevents connecting to orphaned plugin processes from previous runs.
// Thread-safe for concurrent plugin loading.
func (m *PluginManager) allocatePort() int {
	m.portMu.Lock()
	defer m.portMu.Unlock()

	const maxAttempts = 1000
	for i := 0; i < maxAttempts; i++ {
		port := m.nextPort
		m.nextPort++

		// Probe: try to listen on the port to verify it's free
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			m.logger.Debugw("Port in use, skipping", "port", port)
			continue
		}
		ln.Close()

		m.logger.Debugw("Allocated port for plugin", "port", port, "next_port", m.nextPort)
		return port
	}

	// Fallback: return next port and let the plugin handle conflicts
	port := m.nextPort
	m.nextPort++
	m.logger.Warnw("Could not find free port after probing, using next available", "port", port)
	return port
}

// launchPlugin starts a plugin binary and returns the process, actual port, loggers, and log buffer.
// If the plugin outputs QNTX_PLUGIN_PORT=XXXX, that port is returned instead of the requested port.
func (m *PluginManager) launchPlugin(ctx context.Context, config PluginConfig, port int) (*os.Process, int, *pluginLogger, *pluginLogger, *LogBuffer, error) {
	binary := config.Binary

	// Resolve relative paths
	if !filepath.IsAbs(binary) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, 0, nil, nil, nil, errors.Wrapf(err, "failed to get home directory for plugin %s", config.Name)
		}
		binary = filepath.Join(home, ".qntx", "plugins", binary)
	}

	// Check if binary exists
	if _, err := os.Stat(binary); os.IsNotExist(err) {
		err := errors.Newf("plugin binary not found for %s: %s", config.Name, binary)
		return nil, 0, nil, nil, nil, errors.WithHint(err, "install the plugin binary to ~/.qntx/plugins/ or specify the full path in config")
	}

	// Detect TypeScript plugins and wrap with Bun runtime
	var cmd *exec.Cmd
	isTypeScriptPlugin := strings.HasSuffix(binary, ".ts") || isPackageJSONPlugin(binary)

	if isTypeScriptPlugin {
		// TypeScript plugin - launch via Bun runtime
		runtimePath := m.typescriptRuntime
		if runtimePath == "" {
			err := errors.New("TypeScript runtime not configured")
			return nil, 0, nil, nil, nil, errors.WithHint(err,
				"set plugin.runtime.typescript_runtime in am.toml or QNTX_ROOT environment variable")
		}

		// Validate runtime exists
		if _, err := os.Stat(runtimePath); os.IsNotExist(err) {
			err := errors.Newf("TypeScript runtime not found at %s", runtimePath)
			return nil, 0, nil, nil, nil, errors.WithHint(err,
				"verify QNTX installation or set correct path in plugin.runtime.typescript_runtime")
		}

		args := []string{
			"run",
			runtimePath,
			"--plugin-path", binary,
			"--grpc-port", strconv.Itoa(port),
		}
		args = append(args, config.Args...)

		m.logger.Debugw("Launching TypeScript plugin via Bun runtime",
			"name", config.Name,
			"plugin_path", binary,
			"runtime_path", runtimePath,
			"port", port)

		// TODO(#624): Replace primitive exec.Command with Runtime abstraction.
		// Current approach assumes "bun" in PATH, no version checking, delayed failure.
		cmd = exec.Command("bun", args...)
	} else {
		// Native binary plugin (Go, Python, etc.)
		args := append([]string{"--port", strconv.Itoa(port)}, config.Args...)
		cmd = exec.Command(binary, args...)
	}

	// NOTE: Using exec.Command instead of exec.CommandContext intentionally.
	// This prevents plugins from being killed when parent context is cancelled (e.g., during
	// graceful shutdown), allowing proper plugin shutdown via gRPC Shutdown() call.
	//
	// TRADEOFF: If QNTX crashes or is killed (SIGKILL), plugin processes become orphans.
	// TODO: Consider implementing process group management or pidfile tracking for cleanup
	// of orphaned plugins on next QNTX startup.

	// Set environment
	cmd.Env = os.Environ()
	for key, value := range config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	// Create a channel to receive the actual port from plugin output
	portChan := make(chan int, 1)

	// Create shared log buffer for live streaming
	logBuf := NewLogBuffer(200)

	// Capture output for debugging and port discovery
	stdoutLogger := &pluginLogger{
		logger:    m.logger,
		name:      config.Name,
		level:     "info",
		portChan:  portChan,
		logBuffer: logBuf,
	}
	stderrLogger := &pluginLogger{
		logger:    m.logger,
		name:      config.Name,
		level:     "error",
		logBuffer: logBuf,
		// Don't pass portChan to stderr - port announcement should be on stdout
	}

	cmd.Stdout = stdoutLogger
	cmd.Stderr = stderrLogger

	if err := cmd.Start(); err != nil {
		return nil, 0, nil, nil, nil, errors.Wrapf(err, "failed to start plugin %s (cmd=%v)",
			config.Name, cmd.Args)
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

	return cmd.Process, actualPort, stdoutLogger, stderrLogger, logBuf, nil
}

// waitForPlugin waits for a plugin's gRPC server to become ready.
// This polls the gRPC metadata endpoint rather than just checking TCP connectivity
// to ensure the plugin is actually ready to handle requests.
// It also verifies that the correct plugin (by name) is responding at the given address.
func (m *PluginManager) waitForPlugin(ctx context.Context, expectedName string, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	start := time.Now()
	attempt := 0

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		attempt++

		// Try gRPC connection with short timeout
		connCtx, cancel := context.WithTimeout(ctx, time.Second)
		conn, err := grpc.DialContext(connCtx, addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		cancel()

		if err != nil {
			m.logger.Debugw("Plugin not yet reachable",
				"plugin", expectedName, "addr", addr,
				"attempt", attempt, "elapsed_ms", time.Since(start).Milliseconds(),
				"error", err,
			)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Connection succeeded, verify gRPC service is ready by calling metadata
		client := protocol.NewDomainPluginServiceClient(conn)
		metaCtx, metaCancel := context.WithTimeout(ctx, time.Second)
		metaResp, metaErr := client.Metadata(metaCtx, &protocol.Empty{})
		metaCancel()
		conn.Close()

		if metaErr != nil {
			m.logger.Debugw("Plugin connected but Metadata RPC failed",
				"plugin", expectedName, "addr", addr,
				"attempt", attempt, "elapsed_ms", time.Since(start).Milliseconds(),
				"error", metaErr,
			)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// gRPC service is ready, but is it the right plugin?
		if metaResp.Name == expectedName {
			m.logger.Infow("Plugin ready",
				"plugin", expectedName, "addr", addr,
				"attempts", attempt, "elapsed_ms", time.Since(start).Milliseconds(),
			)
			return nil
		}

		// Wrong plugin at this address (likely from another QNTX instance)
		m.logger.Warnw("Wrong plugin at address",
			"expected", expectedName, "found", metaResp.Name,
			"addr", addr, "attempt", attempt,
		)
		time.Sleep(100 * time.Millisecond)
	}

	err := errors.Newf("timeout after %d attempts (%dms) waiting for plugin '%s' gRPC service at %s",
		attempt, time.Since(start).Milliseconds(), expectedName, addr)
	return errors.WithHint(err, "check plugin logs for startup errors, verify no other plugin is using this port, or increase timeout")
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

// GetLogBuffer returns the log buffer for a managed plugin.
// Returns nil if the plugin doesn't exist or has no log buffer (e.g., remote plugins).
func (m *PluginManager) GetLogBuffer(name string) *LogBuffer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if p, ok := m.plugins[name]; ok {
		return p.logBuffer
	}
	return nil
}

// GetFailedPlugins returns a map of plugin names to error messages for plugins that failed to load.
func (m *PluginManager) GetFailedPlugins() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]string, len(m.failedPlugins))
	for name, errMsg := range m.failedPlugins {
		result[name] = errMsg
	}
	return result
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

	// Call ForceInitialize to bypass the once-guard (this is an explicit re-init)
	if err := p.client.ForceInitialize(ctx, services); err != nil {
		wrappedErr := errors.Wrapf(err, "failed to reinitialize plugin %s", pluginName)
		return errors.WithHintf(wrappedErr, "check plugin logs and verify configuration is valid")
	}

	m.logger.Infof("Successfully reinitialized plugin '%s' with updated configuration", pluginName)
	return nil
}

// Shutdown stops all managed plugins and retry goroutines.
func (m *PluginManager) Shutdown(ctx context.Context) error {
	// Stop retry goroutines first
	m.shutdownCancel()

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
	logger    *zap.SugaredLogger
	name      string
	level     string
	buf       strings.Builder
	portChan  chan int     // Optional channel to send discovered port
	logBuffer *LogBuffer   // Optional ring buffer for log streaming
	mu        sync.RWMutex // Protects name field for dynamic updates
}

// updateName safely updates the logger's name with version info
func (l *pluginLogger) updateName(newName string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.name = newName
}

// getName safely retrieves the current name
func (l *pluginLogger) getName() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.name
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

			// Write to log buffer for live streaming
			if l.logBuffer != nil {
				source := "stdout"
				if l.level == "error" {
					source = "stderr"
				}
				l.logBuffer.Write(LogEntry{
					Timestamp: time.Now(),
					Level:     actualLevel,
					Line:      line,
					Source:    source,
				})
			}

			// Log at the actual level from JSON, or fallback to configured level for non-JSON
			name := l.getName()
			switch actualLevel {
			case "debug":
				l.logger.Debugf("[%s] %s", name, line)
			case "info":
				l.logger.Infof("[%s] %s", name, line)
			case "warn":
				l.logger.Warnf("[%s] %s", name, line)
			case "error":
				l.logger.Errorf("[%s stderr] %s", name, line)
			default:
				// Unknown level or non-JSON
				l.logger.Warnf("[%s UNKNOWN] %s", name, line)
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
