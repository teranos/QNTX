package grpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/errors"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
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
	mu                       sync.RWMutex
	plugins                  map[string]*managedPlugin
	failedPlugins            map[string]string // plugin name → error message for plugins that failed to load
	logger                   *zap.SugaredLogger
	rootLogger               *zap.SugaredLogger // un-named root logger for creating per-plugin named loggers
	logDir                   string             // directory for per-plugin log files (default: "tmp")
	basePort                 int
	nextPort                 int             // Track the next port to allocate
	portMu                   sync.Mutex      // Separate mutex for port allocation
	typescriptRuntime        string          // Path to TypeScript runtime (main.ts)
	shutdownCtx              context.Context // cancelled on Shutdown to stop retry goroutines
	shutdownCancel           context.CancelFunc
	servicesManager          *ServicesManager // for re-registering LLM providers after restart
	accumulator              *PluginAccumulator
	onWatchersSetup          func()                                                    // called after plugin watchers are written to DB
	onPluginRestarted        func(name string)                                         // called after auto-restart succeeds (clear HTTP mux state)
	onEmbeddingProviderReady func(name string, client protocol.EmbeddingServiceClient) // called when embedding provider is ready (init or restart)
	onPythonProviderReady    func(name string, client protocol.PythonServiceClient)     // called when a python_provider plugin initializes
	onLifecycleEvent         func(pluginName, version, event string, routes []string)  // called on plugin lifecycle events (started, stopped, restarted, enabled, disabled)
	pidFile                  *pidFile
	db                       *sql.DB                // for schedule setup on restart
	handlerRegistry          *async.HandlerRegistry // for handler re-registration on restart
	retryCancels             map[string]context.CancelFunc // per-plugin retry cancellation
}

// managedPlugin tracks a running plugin.
type managedPlugin struct {
	config       PluginConfig
	client       *ExternalDomainProxy
	cmd          *exec.Cmd   // full command — use cmd.Process.Kill() + cmd.Wait()
	process      *os.Process // shortcut to cmd.Process (nil for remote plugins)
	port         int
	stdoutLogger *pluginLogger
	stderrLogger *pluginLogger
	logBuffer    *LogBuffer
	logFile      *os.File    // per-plugin log file, closed on shutdown/restart
}

// killAndWait terminates the plugin process and waits for exit.
// Sends SIGTERM first for graceful shutdown (lock file cleanup), then SIGKILL.
// Uses a timeout because cmd.Wait() blocks until stdout/stderr pipes close,
// and child processes may inherit those pipes.
func (p *managedPlugin) killAndWait() {
	proc := p.process
	if p.cmd != nil {
		proc = p.cmd.Process
	}
	if proc == nil {
		return
	}

	pid := proc.Pid

	// SIGTERM the entire process group first. If the process was launched with
	// Setpgid (its own group leader), this kills it and all children.
	// If not (legacy launch), the negative-pid kill returns ESRCH and we
	// fall back to signaling the process directly.
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		proc.Signal(os.Interrupt)
	}

	done := make(chan struct{})
	if p.cmd != nil {
		go func() {
			p.cmd.Wait()
			close(done)
		}()
	} else {
		go func() {
			proc.Wait()
			close(done)
		}()
	}

	// Wait up to 3s for graceful exit
	select {
	case <-done:
		return
	case <-time.After(3 * time.Second):
	}

	// Force kill — try process group first, fall back to direct kill
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		proc.Kill()
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

const (
	// DefaultPluginBasePort is the starting port for plugin allocation
	// Uses 38700 to avoid conflicts with common development tools
	DefaultPluginBasePort = 38700
)

// NewPluginManager creates a new plugin manager.
// The logger is used for manager-level messages; rootLogger is used to create
// per-plugin named loggers so each plugin's output shows its own name.
func NewPluginManager(logger *zap.SugaredLogger, rootLogger *zap.SugaredLogger, typescriptRuntime string) *PluginManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &PluginManager{
		plugins:           make(map[string]*managedPlugin),
		failedPlugins:     make(map[string]string),
		retryCancels:      make(map[string]context.CancelFunc),
		logger:            logger,
		rootLogger:        rootLogger,
		logDir:            "tmp",
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

// SetPidFile configures PID file tracking for plugin process cleanup.
// Must be called before LoadPlugins.
func (m *PluginManager) SetPidFile(dir string, serverPort int) {
	name := fmt.Sprintf("plugins-%d.pid", serverPort)
	m.pidFile = newPidFile(filepath.Join(dir, name), m.logger)
}

// SetLogDir sets the directory for per-plugin log files.
func (m *PluginManager) SetLogDir(dir string) {
	m.logDir = dir
}

// SetServicesManager sets the services manager for LLM provider re-registration after restart.
func (m *PluginManager) SetServicesManager(sm *ServicesManager) {
	m.servicesManager = sm
}

// SetPulseResources stores DB and handler registry for schedule/handler re-registration on plugin restart.
func (m *PluginManager) SetPulseResources(db *sql.DB, registry *async.HandlerRegistry) {
	m.db = db
	m.handlerRegistry = registry
}

// SetOnWatchersSetup sets a callback invoked after plugin watchers are written to DB.
// The server uses this to reload the watcher engine's in-memory map.
func (m *PluginManager) SetOnWatchersSetup(fn func()) {
	m.onWatchersSetup = fn
}

// SetOnPluginRestarted sets a callback invoked after a plugin auto-restart succeeds.
// The server uses this to clear stale HTTP mux state (sync.Once + cached ServeMux).
func (m *PluginManager) SetOnPluginRestarted(fn func(name string)) {
	m.onPluginRestarted = fn
}

// SetOnLifecycleEvent sets a callback invoked on plugin lifecycle events
// (started, stopped, restarted, enabled, disabled). The server uses this to
// write deferred news attestations to Ground.
func (m *PluginManager) SetOnLifecycleEvent(fn func(pluginName, version, event string, routes []string)) {
	m.onLifecycleEvent = fn
}

// emitLifecycle fires the lifecycle callback if set.
// Every banner emission is a lifecycle moment worth attesting.
func (m *PluginManager) emitLifecycle(name, version, event string, routes []string) {
	if m.onLifecycleEvent != nil {
		m.onLifecycleEvent(name, version, event, routes)
	}
}

// EmitLifecycle is the exported version for callers outside the package.
func (m *PluginManager) EmitLifecycle(name, version, event string, routes []string) {
	m.emitLifecycle(name, version, event, routes)
}

// SetOnEmbeddingProviderReady sets a callback invoked when an embedding provider
// plugin is ready (on init or after restart). The server uses this to re-wire
// the embedding service with the plugin's fresh gRPC client.
func (m *PluginManager) SetOnEmbeddingProviderReady(fn func(name string, client protocol.EmbeddingServiceClient)) {
	m.onEmbeddingProviderReady = fn
}

// SetOnPythonProviderReady sets a callback invoked when a plugin declaring
// python_provider=true finishes initialization. The server uses this to
// wire the gRPC PythonService executor for "py" glyph execution.
func (m *PluginManager) SetOnPythonProviderReady(fn func(name string, client protocol.PythonServiceClient)) {
	m.onPythonProviderReady = fn
}

// Accumulator returns the plugin banner accumulator.
func (m *PluginManager) Accumulator() *PluginAccumulator {
	return m.accumulator
}

// SetAccumulator sets the plugin banner accumulator.
func (m *PluginManager) SetAccumulator(acc *PluginAccumulator) {
	m.accumulator = acc
}

// LoadPlugins loads and connects to plugins from configuration.
// Enabled plugins are retried forever — enabled means the operator is certain
// this plugin must run. Disabled plugins are skipped entirely.
func (m *PluginManager) LoadPlugins(ctx context.Context, configs []PluginConfig) error {
	// Kill plugin processes orphaned by a previous run
	if m.pidFile != nil {
		m.pidFile.CleanStale()
	}

	for _, config := range configs {
		if !config.Enabled {
			m.logger.Infow("Skipping disabled plugin", "name", config.Name)
			continue
		}

		if err := m.loadPlugin(ctx, config); err != nil {
			m.logger.Errorf("Failed to load plugin '%s' (binary=%s, address=%s): %v",
				config.Name, config.Binary, config.Address, err)
			m.mu.Lock()
			m.failedPlugins[config.Name] = err.Error()
			m.mu.Unlock()

			// Enabled means forever — retry until server shuts down
			// Registry/services not yet available at boot — passed as nil,
			// registration happens later in loadPluginsAsync.
			go m.retryPluginForever(m.shutdownCtx, config, nil, nil)
			continue
		}
	}

	return nil
}

// retryPluginForever kills and relaunches a plugin process until it loads.
// Each cycle: launch process → try connecting 3 times (1s, 3s, 9s) → if all fail, kill and relaunch.
// registry and services may be nil during early boot (before server is ready).
func (m *PluginManager) retryPluginForever(ctx context.Context, config PluginConfig, registry *plugin.Registry, services plugin.ServiceRegistry) {
	// Register per-plugin cancel so EnablePlugin can stop this loop
	retryCtx, retryCancel := context.WithCancel(ctx)
	defer retryCancel()
	m.mu.Lock()
	m.retryCancels[config.Name] = retryCancel
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.retryCancels, config.Name)
		m.mu.Unlock()
	}()

	cycle := 0
	for {
		cycle++
		select {
		case <-retryCtx.Done():
			m.logger.Debugf("Retry cancelled for plugin '%s'", config.Name)
			return
		default:
		}

		if cycle <= 3 || cycle%10 == 0 {
			m.logger.Infof("Restarting plugin '%s' process (cycle %d)", config.Name, cycle)
		}

		// Clean up previous state so loadPlugin doesn't see "already loaded"
		m.mu.Lock()
		if old, exists := m.plugins[config.Name]; exists {
			old.killAndWait()
			delete(m.plugins, config.Name)
		}
		m.mu.Unlock()

		err := m.loadPlugin(retryCtx, config)
		if err == nil {
			m.mu.Lock()
			delete(m.failedPlugins, config.Name)
			m.mu.Unlock()

			if registry != nil {
				m.registerRestarted(retryCtx, config.Name, registry, services, BannerRecovered)
			}

			// Clear stale HTTP mux state so next request re-initializes
			if m.onPluginRestarted != nil {
				m.onPluginRestarted(config.Name)
			}

			m.logger.Debugf("Plugin '%s' loaded successfully on restart cycle %d", config.Name, cycle)
			return
		}

		if cycle <= 3 || cycle%10 == 0 {
			m.logger.Errorf("Plugin '%s' restart cycle %d failed: %v", config.Name, cycle, err)
		}
		m.mu.Lock()
		m.failedPlugins[config.Name] = err.Error()
		m.mu.Unlock()
		if registry != nil {
			registry.MarkFailed(config.Name, err.Error())
		}

		// Wait before next full restart cycle
		select {
		case <-retryCtx.Done():
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
	var pluginCmd *exec.Cmd
	var port int
	var stdoutLogger *pluginLogger
	var stderrLogger *pluginLogger
	var logBuf *LogBuffer
	var logFile *os.File

	if config.Address != "" {
		addr = config.Address
		m.logger.Debugw("Connecting to existing plugin", "name", config.Name, "address", addr)
	} else if config.Binary != "" && config.AutoStart {
		port = m.allocatePort()
		addr = fmt.Sprintf("127.0.0.1:%d", port)

		var err error
		var actualPort int
		pluginCmd, actualPort, stdoutLogger, stderrLogger, logBuf, logFile, err = m.launchPlugin(ctx, config, port)
		if err != nil {
			return errors.Wrapf(err, "failed to launch plugin %s (binary=%s, port=%d)",
				config.Name, config.Binary, port)
		}

		if actualPort != 0 && actualPort != port {
			port = actualPort
			addr = fmt.Sprintf("127.0.0.1:%d", port)
			m.logger.Debugw("Plugin bound to different port", "name", config.Name, "actual_port", actualPort)
		}

		m.logger.Debugf("Started '%s' plugin process (pid=%d, port=%d, addr=%s)",
			config.Name, pluginCmd.Process.Pid, port, addr)

		if m.pidFile != nil {
			m.pidFile.Add(pluginCmd.Process.Pid)
		}

		if err := m.waitForPlugin(ctx, config.Name, addr, 5*time.Second); err != nil {
			pluginCmd.Process.Kill()
			pluginCmd.Wait()
			return errors.Wrapf(err, "plugin %s failed to start (binary=%s, addr=%s, pid=%d)",
				config.Name, config.Binary, addr, pluginCmd.Process.Pid)
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

	// Connect to the plugin with retry: 1s, 3s, 9s backoff.
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
			select {
			case <-ctx.Done():
				if pluginCmd != nil {
					pluginCmd.Process.Kill()
					pluginCmd.Wait()
				}
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	if connectErr != nil {
		if pluginCmd != nil {
			pluginCmd.Process.Kill()
			pluginCmd.Wait()
		}
		return errors.Wrapf(connectErr, "failed to connect to plugin %s at %s after %d attempts",
			config.Name, addr, len(connectBackoffs))
	}

	// Validate plugin metadata matches config
	meta := client.Metadata()
	if meta.Name != config.Name {
		if pluginCmd != nil {
			pluginCmd.Process.Kill()
			pluginCmd.Wait()
		}
		err := errors.Newf("plugin metadata mismatch: binary at %s reports name='%s' but config expects '%s'",
			config.Binary, meta.Name, config.Name)
		return errors.WithHint(err, "verify the correct plugin binary is installed or update the plugin name in config")
	}

	// Wire watcher reload callback so engine picks up plugin-declared watchers
	if m.onWatchersSetup != nil {
		client.OnWatchersSetup = m.onWatchersSetup
	}

	// Derive process shortcut for code that only needs the PID
	var process *os.Process
	if pluginCmd != nil {
		process = pluginCmd.Process
	}

	// Register — lock only for the final state write
	m.mu.Lock()
	m.plugins[config.Name] = &managedPlugin{
		config:       config,
		client:       client,
		cmd:          pluginCmd,
		process:      process,
		port:         port,
		stdoutLogger: stdoutLogger,
		stderrLogger: stderrLogger,
		logBuffer:    logBuf,
		logFile:      logFile,
	}
	m.mu.Unlock()

	m.logger.Debugf("Plugin '%s' v%s loaded and ready - %s",
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

// launchPlugin starts a plugin binary and returns the command, actual port, loggers, and log buffer.
// If the plugin outputs QNTX_PLUGIN_PORT=XXXX, that port is returned instead of the requested port.
func (m *PluginManager) launchPlugin(ctx context.Context, config PluginConfig, port int) (*exec.Cmd, int, *pluginLogger, *pluginLogger, *LogBuffer, *os.File, error) {
	binary := config.Binary

	// Resolve relative paths
	if !filepath.IsAbs(binary) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, 0, nil, nil, nil, nil, errors.Wrapf(err, "failed to get home directory for plugin %s", config.Name)
		}
		binary = filepath.Join(home, ".qntx", "plugins", binary)
	}

	// Check if binary exists
	if _, err := os.Stat(binary); os.IsNotExist(err) {
		err := errors.Newf("plugin binary not found for %s: %s", config.Name, binary)
		return nil, 0, nil, nil, nil, nil, errors.WithHint(err, "install the plugin binary to ~/.qntx/plugins/ or specify the full path in config")
	}

	// Detect TypeScript plugins and wrap with Bun runtime
	var cmd *exec.Cmd
	isTypeScriptPlugin := strings.HasSuffix(binary, ".ts") || isPackageJSONPlugin(binary)

	if isTypeScriptPlugin {
		// TypeScript plugin - launch via Bun runtime
		runtimePath := m.typescriptRuntime
		if runtimePath == "" {
			err := errors.New("TypeScript runtime not configured")
			return nil, 0, nil, nil, nil, nil, errors.WithHint(err,
				"set plugin.runtime.typescript_runtime in am.toml or QNTX_ROOT environment variable")
		}

		// Validate runtime exists
		if _, err := os.Stat(runtimePath); os.IsNotExist(err) {
			err := errors.Newf("TypeScript runtime not found at %s", runtimePath)
			return nil, 0, nil, nil, nil, nil, errors.WithHint(err,
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

	// Using exec.Command instead of exec.CommandContext intentionally.
	// Plugins are not killed on context cancellation — graceful shutdown sends
	// gRPC Shutdown() first. Orphans from crashes are cleaned up on next startup
	// via pidfile tracking (see pidfile.go).

	// Set environment
	cmd.Env = os.Environ()
	for key, value := range config.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	// Create a channel to receive the actual port from plugin output
	portChan := make(chan int, 1)

	// Create shared log buffer for live streaming
	logBuf := NewLogBuffer(200)

	// Open per-plugin log file (e.g. tmp/myplugin.log).
	// Plugin output goes here instead of the main QNTX log.
	var logFile *os.File
	if m.logDir != "" {
		if err := os.MkdirAll(m.logDir, 0755); err != nil {
			m.logger.Warnw("Failed to create plugin log directory", "dir", m.logDir, "error", err)
		} else {
			logPath := filepath.Join(m.logDir, config.Name+".log")
			f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				m.logger.Warnw("Failed to open plugin log file", "path", logPath, "error", err)
			} else {
				logFile = f
				marker := fmt.Sprintf("\n========== %s START %s ==========\n",
					strings.ToUpper(config.Name), time.Now().Format("2006-01-02T15:04:05.000"))
				logFile.WriteString(marker)
			}
		}
	}

	// Capture output for debugging and port discovery.
	stdoutLogger := &pluginLogger{
		file:      logFile,
		level:     "info",
		portChan:  portChan,
		logBuffer: logBuf,
	}
	stderrLogger := &pluginLogger{
		file:      logFile,
		level:     "debug",
		logBuffer: logBuf,
		// Don't pass portChan to stderr - port announcement should be on stdout
	}

	cmd.Stdout = stdoutLogger
	cmd.Stderr = stderrLogger

	if err := cmd.Start(); err != nil {
		return nil, 0, nil, nil, nil, nil, errors.Wrapf(err, "failed to start plugin %s (cmd=%v)",
			config.Name, cmd.Args)
	}

	// Wait for the port announcement with a short timeout (2 seconds)
	// The plugin should announce its port almost immediately after binding
	actualPort := port // Default to requested port
	select {
	case discoveredPort := <-portChan:
		actualPort = discoveredPort
		m.logger.Debugw("Discovered plugin port from stdout",
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

	return cmd, actualPort, stdoutLogger, stderrLogger, logBuf, logFile, nil
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
			m.logger.Debugw("Plugin ready",
				"plugin", expectedName, "addr", addr,
				"attempts", attempt, "elapsed_ms", time.Since(start).Milliseconds(),
			)
			return nil
		}

		// Wrong plugin at this address — log once, not every retry
		if attempt == 1 {
			m.logger.Warnw("Wrong plugin at address",
				"expected", expectedName, "found", metaResp.Name,
				"addr", addr,
			)
		}
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
	m.logger.Debugw("WebSocket configuration applied to plugins",
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

	m.logger.Debugf("Successfully reinitialized plugin '%s' with updated configuration", pluginName)
	return nil
}

// RestartPlugin kills a running plugin and relaunches it from its binary.
// The new process picks up whatever binary is on disk — use after rebuilding.
// If the relaunch fails, retries forever in the background (same as boot).
// searchPaths is needed when the plugin isn't in the map (e.g. removed by health poller
// or retry loop) — pass nil to skip the "not loaded" fallback.
func (m *PluginManager) RestartPlugin(ctx context.Context, name string, searchPaths []string, registry *plugin.Registry, services plugin.ServiceRegistry) error {
	// Grab config and kill old process
	m.mu.Lock()
	// Cancel any active retry loop first
	if cancel, ok := m.retryCancels[name]; ok {
		cancel()
		delete(m.retryCancels, name)
	}
	p, exists := m.plugins[name]
	pluginNames := make([]string, 0, len(m.plugins))
	for n := range m.plugins {
		pluginNames = append(pluginNames, n)
	}
	retryNames := make([]string, 0, len(m.retryCancels))
	for n := range m.retryCancels {
		retryNames = append(retryNames, n)
	}
	m.logger.Infow("RestartPlugin: map check",
		"plugin", name,
		"in_map", exists,
		"all_plugins", pluginNames,
		"all_retries", retryNames)
	if !exists {
		m.mu.Unlock()
		if len(searchPaths) == 0 {
			return errors.Newf("plugin not loaded: %s", name)
		}
		// Plugin not in map — kill any stale OS process, then discover + enable from scratch.
		if m.servicesManager != nil {
			m.servicesManager.CancelATSStreams()
		}
		m.logger.Infow("RestartPlugin: plugin not in map, killing stale processes and re-enabling",
			"plugin", name)
		m.killStalePluginProcesses(name)
		registry.Unregister(name)
		return m.EnablePlugin(ctx, name, searchPaths, registry, services)
	}
	config := p.config

	// Cancel active ATSStore streams before killing — frees the database mutex
	// so the new process can initialize without contention.
	if m.servicesManager != nil {
		m.servicesManager.CancelATSStreams()
	}

	p.killAndWait()
	if p.logFile != nil {
		p.logFile.Close()
	}
	delete(m.plugins, name)
	m.mu.Unlock()

	// Close old gRPC connection
	p.client.Shutdown(ctx)

	// Unregister from registry so Register doesn't hit "already registered"
	registry.Unregister(name)

	m.logger.Debugf("Killed plugin '%s', relaunching from %s", name, config.Binary)

	// Relaunch — if it fails, retry forever in background
	if err := m.loadPlugin(ctx, config); err != nil {
		m.logger.Errorf("Restart of '%s' failed: %v — retrying in background", name, err)
		m.mu.Lock()
		m.failedPlugins[name] = err.Error()
		m.mu.Unlock()
		registry.MarkFailed(name, err.Error())
		go m.retryPluginForever(m.shutdownCtx, config, registry, services)
		return nil // not an error to the caller — retry is in progress
	}

	m.registerRestarted(ctx, name, registry, services, BannerRecovered)

	// Clear stale HTTP mux and pre-register new proxy routes.
	// Must run AFTER registerRestarted which registers the plugin in the registry.
	if m.onPluginRestarted != nil {
		m.onPluginRestarted(config.Name)
	}

	return nil
}

// killStalePluginProcesses finds and kills any OS process running a plugin binary
// for the given plugin name. Uses process group kill to also terminate children
// (e.g. Reticulum). Only targets processes started with --port (plugin mode),
// not --mcp instances.
func (m *PluginManager) killStalePluginProcesses(name string) {
	// Binary naming convention: qntx-{name} or qntx-{name}-plugin
	targets := []string{
		"qntx-" + name + "-plugin",
		"qntx-" + name,
	}

	out, err := exec.Command("ps", "-e", "-o", "pid=,args=").Output()
	if err != nil {
		m.logger.Warnw("Failed to list processes for stale plugin kill", "plugin", name, "error", err)
		return
	}

	myPid := os.Getpid()
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Must contain --port (plugin mode) and NOT --mcp
		if !strings.Contains(line, "--port") || strings.Contains(line, "--mcp") {
			continue
		}

		// Check if line matches any target binary name
		matched := false
		for _, target := range targets {
			if strings.Contains(line, target) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		// Extract PID (first whitespace-delimited field)
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == myPid {
			continue
		}

		m.logger.Infow("Killing stale plugin process", "plugin", name, "pid", pid)

		proc, findErr := os.FindProcess(pid)
		if findErr != nil {
			continue
		}
		proc.Kill()
		proc.Wait()
	}
}

// registerRestarted re-registers a successfully relaunched plugin with the
// registry and reinitializes it with services. Emits the banner after health
// check completes (async) so it shows actual health, not "initializing".
func (m *PluginManager) registerRestarted(ctx context.Context, name string, registry *plugin.Registry, services plugin.ServiceRegistry, reason BannerReason) {
	newPlugin, _ := m.GetPlugin(name)
	// Unregister first to handle races between health poller restarts and
	// manual restarts — both can call registerRestarted concurrently.
	registry.Unregister(name)
	if err := registry.Register(newPlugin); err != nil {
		m.logger.Errorf("Failed to re-register plugin '%s': %v", name, err)
		return
	}
	registry.MarkReady(name)

	if services != nil {
		// Re-read am.toml from disk so plugin gets fresh config without server restart
		if err := am.ReloadPluginSection(name); err != nil {
			m.logger.Warnf("Failed to reload config for plugin '%s' from am.toml: %v", name, err)
		}
		// Initialize with a 30s deadline. Plugin ATS connectivity checks can take
		// 10-15s when the RustStore mutex is contended (5s watchdog alerts).
		// Use a goroutine + select so we never block banner emission.
		m.mu.RLock()
		p, exists := m.plugins[name]
		m.mu.RUnlock()
		if exists {
			m.logger.Infow("registerRestarted: calling Initialize", "plugin", name)
			initDone := make(chan error, 1)
			go func() {
				initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer initCancel()
				initDone <- p.client.Initialize(initCtx, services)
			}()
			select {
			case err := <-initDone:
				if err != nil {
					m.logger.Errorf("Failed to initialize plugin '%s' after restart: %v", name, err)
				}
				m.logger.Infow("registerRestarted: Initialize returned", "plugin", name)
			case <-time.After(30 * time.Second):
				m.logger.Warnw("registerRestarted: Initialize timed out, continuing with banner", "plugin", name)
			}
		}
	}

	// Re-register async handlers and schedules
	m.mu.RLock()
	p, exists := m.plugins[name]
	m.mu.RUnlock()
	if !exists {
		m.logger.Debugf("Plugin '%s' restarted successfully", name)
		return
	}
	proxy := p.client

	if m.handlerRegistry != nil {
		for _, handlerName := range proxy.GetHandlerNames() {
			proxyHandler := NewPluginProxyHandler(name, handlerName, proxy, m.db, m.logger)
			m.handlerRegistry.Replace(proxyHandler)
			m.logger.Debugw("Re-registered plugin async handler", "plugin", name, "handler", handlerName,
				"registry_key", PluginHandlerName(name, handlerName))
		}
	}

	if m.db != nil {
		schedules := proxy.GetSchedules()
		if len(schedules) > 0 {
			if err := SetupPluginSchedules(m.db, name, schedules, m.logger); err != nil {
				m.logger.Errorw("Failed to setup plugin schedules on restart",
					"plugin", name, "error", err)
			}
		}
	}

	var roles []string
	if m.servicesManager != nil {
		if proxy.IsLLMProvider() {
			roles = append(roles, "llm-provider")
			if llmRouter := m.servicesManager.GetLLMRouter(); llmRouter != nil {
				llmRouter.RegisterProvider(name, proxy.LLMServiceClient())
				m.logger.Debugf("Re-registered LLM provider '%s' after restart", name)
			}
		}
		if proxy.IsSearchProvider() {
			roles = append(roles, "search-provider")
			if searchRouter := m.servicesManager.GetSearchRouter(); searchRouter != nil {
				searchRouter.RegisterProvider(name, proxy.SearchServiceClient())
				m.logger.Debugf("Re-registered search provider '%s' after restart", name)
			}
		}
		if proxy.IsEmbeddingProvider() {
			roles = append(roles, "embedding-provider")
			if m.onEmbeddingProviderReady != nil {
				m.onEmbeddingProviderReady(name, proxy.EmbeddingServiceClient())
				m.logger.Debugf("Re-registered embedding provider '%s' after restart", name)
			}
		}
		if proxy.IsPythonProvider() {
			roles = append(roles, "python-provider")
			if m.onPythonProviderReady != nil {
				m.onPythonProviderReady(name, proxy.PythonServiceClient())
			}
		}
	}

	// Populate accumulator for banner (caller emits with appropriate reason)
	if m.accumulator != nil {
		meta := proxy.Metadata()
		m.accumulator.SetLoading(name, meta.Version)
		m.accumulator.SetRoles(name, roles)
		m.accumulator.SetHandlers(name, proxy.GetHandlerNames(), ScheduleNames(proxy.GetSchedules()), WatcherNames(proxy.GetWatchers()), UnfilteredWatcherNames(proxy.GetWatchers()))
		var routeStrs []string
		if routes := proxy.GetHTTPRoutes(); len(routes) > 0 {
			routeStrs = make([]string, len(routes))
			for i, r := range routes {
				routeStrs[i] = r.GetMethod() + " " + r.GetPath()
			}
			m.accumulator.SetHTTPRoutes(name, routeStrs)
		}
		// Collect health asynchronously — synchronous Health() blocks plugin restart
		// while the plugin makes ATS calls back to QNTX.
		// Emit the banner inside the goroutine so it shows actual health status.
		m.logger.Infow("registerRestarted: launching banner goroutine", "plugin", name, "reason", reason)
		go func() {
			m.logger.Infow("registerRestarted: calling Health()", "plugin", name)
			healthCtx, hCancel := context.WithTimeout(context.Background(), 5*time.Second)
			health := proxy.Health(healthCtx)
			hCancel()
			m.logger.Infow("registerRestarted: Health() returned", "plugin", name, "healthy", health.Healthy, "message", health.Message)
			details := make(map[string]string)
			for k, v := range health.Details {
				if s, ok := v.(string); ok {
					details[k] = s
				}
			}
			m.accumulator.SetHealth(name, health.Healthy, health.Message, details)
			m.accumulator.Emit(name, reason)
			m.logger.Infow("registerRestarted: banner emitted", "plugin", name, "reason", reason)
			m.emitLifecycle(name, meta.Version, string(reason), routeStrs)
		}()
	} else {
		m.logger.Warnw("registerRestarted: accumulator is nil, no banner will be emitted", "plugin", name)
	}
	m.logger.Infow("registerRestarted: completed", "plugin", name)
}

// EnablePlugin discovers, loads, registers, and initializes a plugin at runtime.
// The plugin must not already be loaded. Search paths are used to find the binary.
func (m *PluginManager) EnablePlugin(ctx context.Context, name string, searchPaths []string, registry *plugin.Registry, services plugin.ServiceRegistry) error {
	// Skip if a retry loop is already working on this plugin
	m.mu.RLock()
	_, retrying := m.retryCancels[name]
	_, exists := m.plugins[name]
	m.mu.RUnlock()
	if retrying {
		return nil // retry loop will handle it
	}
	if exists {
		return errors.Newf("plugin '%s' is already loaded", name)
	}

	// Discover binary
	config, err := discoverPlugin(name, searchPaths, m.logger)
	if err != nil {
		return errors.Wrapf(err, "failed to discover plugin '%s'", name)
	}

	// Load (launch process + connect gRPC)
	if err := m.loadPlugin(ctx, config); err != nil {
		return errors.Wrapf(err, "failed to load plugin '%s'", name)
	}

	// Register + initialize + setup handlers/watchers/schedules/providers
	// Banner emits asynchronously after health check completes
	m.registerRestarted(ctx, name, registry, services, BannerEnabled)

	// Register HTTP/WS routes for hot-swapped plugin
	if m.onPluginRestarted != nil {
		m.onPluginRestarted(name)
	}

	return nil
}

// DisablePlugin shuts down a running plugin, unregisters it, and kills its process.
// Prunes the plugin's watchers from the DB and removes async handlers.
func (m *PluginManager) DisablePlugin(ctx context.Context, name string, registry *plugin.Registry) error {
	m.mu.Lock()
	p, exists := m.plugins[name]
	if !exists {
		m.mu.Unlock()
		return errors.Newf("plugin '%s' is not loaded", name)
	}
	meta := p.client.Metadata()
	client := p.client
	delete(m.plugins, name)
	delete(m.failedPlugins, name)
	m.mu.Unlock()

	// Cancel active ATSStore streams before killing — frees the database mutex
	// so the new process can initialize without contention.
	if m.servicesManager != nil {
		m.servicesManager.CancelATSStreams()
	}

	// Shutdown via gRPC (best-effort)
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := client.Shutdown(shutdownCtx); err != nil {
		m.logger.Warnw("Plugin shutdown RPC failed (will kill process)", "plugin", name, "error", err)
	}
	cancel()

	// Kill process and wait for exit so file locks are released
	// before the new process launches.
	p.killAndWait()

	// Unregister from plugin registry and mark stopped (not restarting)
	registry.Unregister(name)
	registry.MarkStopped(name)

	// Prune watchers: pass empty list so all watchers with this plugin's prefix are deleted
	if m.db != nil {
		if err := SetupPluginWatchers(m.db, name, nil, nil, m.logger); err != nil {
			m.logger.Warnw("Failed to prune watchers for disabled plugin", "plugin", name, "error", err)
		}
		if m.onWatchersSetup != nil {
			m.onWatchersSetup()
		}
	}

	// Unregister service providers so observers stop routing to dead connections
	if m.servicesManager != nil {
		if client.IsSearchProvider() {
			if searchRouter := m.servicesManager.GetSearchRouter(); searchRouter != nil {
				searchRouter.UnregisterProvider(name)
			}
		}
		if client.IsLLMProvider() {
			if llmRouter := m.servicesManager.GetLLMRouter(); llmRouter != nil {
				llmRouter.UnregisterProvider(name)
			}
		}
	}

	// Unregister async handlers
	if m.handlerRegistry != nil {
		for _, handlerName := range client.GetHandlerNames() {
			m.handlerRegistry.Remove(handlerName)
		}
	}

	// Emit disabled banner
	if m.accumulator != nil {
		m.accumulator.SetLoading(name, meta.Version)
		m.accumulator.Emit(name, BannerDisabled)
	}

	m.emitLifecycle(name, meta.Version, "disabled", nil)

	return nil
}

// LoadedPluginNames returns the names of all currently loaded or retrying plugins.
func (m *PluginManager) LoadedPluginNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := make(map[string]bool, len(m.plugins)+len(m.retryCancels))
	names := make([]string, 0, len(m.plugins)+len(m.retryCancels))
	for name := range m.plugins {
		names = append(names, name)
		seen[name] = true
	}
	for name := range m.retryCancels {
		if !seen[name] {
			names = append(names, name)
		}
	}
	return names
}

// Shutdown stops all managed plugins and retry goroutines.
func (m *PluginManager) Shutdown(ctx context.Context) error {
	// Stop retry goroutines first
	m.shutdownCancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	for name, p := range m.plugins {
		meta := p.client.Metadata()

		// Shutdown the plugin via gRPC
		if err := p.client.Shutdown(ctx); err != nil {
			m.logger.Warnw("Plugin shutdown error", "name", name, "error", err)
			errs = append(errs, err)
		}

		// Signal the process to exit
		if p.process != nil {
			if err := p.process.Signal(os.Interrupt); err != nil {
				p.process.Kill()
			}
		}

		// Close per-plugin log file
		if p.logFile != nil {
			p.logFile.Close()
		}

		m.emitLifecycle(name, meta.Version, "stopped", nil)
	}

	m.plugins = make(map[string]*managedPlugin)

	// Clean shutdown — remove PID file so next startup doesn't kill anything
	if m.pidFile != nil {
		m.pidFile.Remove()
	}

	if len(errs) > 0 {
		return errors.Newf("shutdown errors: %v", errs)
	}
	return nil
}

// pluginLogger captures plugin stdout/stderr, writes to a per-plugin log file,
// and feeds the LogBuffer for WebSocket live streaming.
type pluginLogger struct {
	file      *os.File   // per-plugin log file (e.g. tmp/myplugin.log)
	level     string
	buf       strings.Builder
	portChan  chan int   // Optional channel to send discovered port
	logBuffer *LogBuffer // Optional ring buffer for log streaming
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

			// Write to per-plugin log file with timestamp and level
			if l.file != nil {
				ts := time.Now().Format("2006-01-02T15:04:05.000")
				fmt.Fprintf(l.file, "%s\t%s\t%s\n", ts, strings.ToUpper(actualLevel), line)
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
