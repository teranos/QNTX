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
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// alongside is err with alsoErr said beside it, or whichever of the two
// happened when only one did.
func alongside(err, alsoErr error) error {
	if err == nil {
		return alsoErr
	}
	return errors.WithSecondaryError(err, alsoErr)
}

// isPackageJSONPlugin checks whether path, or the directory a file at path is
// in, holds a package.json with the qntx-plugin marker.
func isPackageJSONPlugin(path string, info os.FileInfo) bool {
	dir := filepath.Dir(path)
	if info.IsDir() {
		dir = path
	}

	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
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

// PluginConfig is a plugin the manager loads: its name, where it runs from,
// and what it is launched with.
type PluginConfig struct {
	// Name is the plugin identifier
	Name string

	// Enabled controls whether the plugin is loaded
	Enabled bool

	// Source is where the plugin runs from.
	Source PluginSource

	// Args are additional arguments passed to a launched plugin binary
	Args []string
}

// sourceKind is how a plugin comes to be running.
type sourceKind int

const (
	// sourceBinary is a binary QNTX launches.
	sourceBinary sourceKind = iota + 1
	// sourceAddress is a plugin something else runs, reached at its address.
	sourceAddress
)

// PluginSource is where a plugin runs from: a binary QNTX launches, or an
// address a plugin already runs at. A PluginSource made by neither names
// nothing, and loading it is refused.
type PluginSource struct {
	how sourceKind
	at  string
}

// LaunchBinary is a plugin QNTX launches from the binary at path. A relative
// path is resolved under ~/.qntx/plugins/.
func LaunchBinary(path string) PluginSource {
	return PluginSource{how: sourceBinary, at: path}
}

// RunningAt is a plugin already running at address (host:port).
func RunningAt(address string) PluginSource {
	return PluginSource{how: sourceAddress, at: address}
}

func (s PluginSource) String() string {
	switch s.how {
	case sourceBinary:
		return "binary " + s.at
	case sourceAddress:
		return "address " + s.at
	}
	return fmt.Sprintf("no source (kind %d)", s.how)
}

// PluginManager manages plugin processes and connections.
type PluginManager struct {
	mu                sync.RWMutex
	plugins           map[string]*managedPlugin
	failedPlugins     map[string]string // plugin name → error message for plugins that failed to load
	logger            *zap.SugaredLogger
	rootLogger        *zap.SugaredLogger // un-named root logger for creating per-plugin named loggers
	logDir            string             // directory for per-plugin log files
	basePort          int
	nextPort          int             // Track the next port to allocate
	portMu            sync.Mutex      // Separate mutex for port allocation
	typescriptRuntime string          // Path to TypeScript runtime (main.ts)
	shutdownCtx       context.Context // cancelled on Shutdown to stop retry goroutines
	shutdownCancel    context.CancelFunc
	accumulator       *PluginAccumulator
	pids              pids
	retryCancels      map[string]context.CancelFunc // per-plugin retry cancellation

	// What the node hands the manager as it comes up, each when it is there.
	// A plugin is joined to what has been handed over, and nothing else.
	wiringMu        sync.RWMutex
	joints          map[string]joint
	servicesManager *ServicesManager                                            // whose store tokens a plugin's async handlers open runs with
	watchersSetup   []func()                                                    // told after plugin watchers are written to DB
	pluginRestarted []func(name string)                                         // told after a plugin comes back (clear HTTP mux state)
	embeddingReady  []func(name string, client protocol.EmbeddingServiceClient) // told when an embedding provider is ready (init or restart)
	pythonReady     []func(name string, client protocol.PythonServiceClient)    // told when a python_provider plugin initializes
	lifecycle       []func(pluginName, version, event string, routes []string)  // told on plugin lifecycle events

	// The node's WebSocket settings, kept so a plugin loaded after they were
	// set gets them too. wsConfigured says ConfigureWebSocket has run.
	wsConfigured bool
	wsKeepalive  KeepaliveConfig
	wsConfig     WebSocketConfig
}

// managedPlugin tracks a running plugin.
type managedPlugin struct {
	pluginCfg PluginConfig
	client    *ExternalDomainProxy
	proc      running
	port      int
}

// running is how a loaded plugin's process is held.
type running interface {
	// stop ends the process and waits for it to be gone.
	stop() error
	// abandon kills a process a failing load is giving up on, and reaps it.
	abandon() error
	// interrupt asks the process to exit, at node shutdown.
	interrupt() error
	// closeLog closes the plugin's own log file.
	closeLog() error
	// logs is the ring of its recent output, for live streaming.
	logs() *LogBuffer
}

// attachedPlugin is a plugin something else runs: QNTX reaches it at its
// address, and has no process to stop and no output to keep.
type attachedPlugin struct{}

func (attachedPlugin) stop() error      { return nil }
func (attachedPlugin) abandon() error   { return nil }
func (attachedPlugin) interrupt() error { return nil }
func (attachedPlugin) closeLog() error  { return nil }
func (attachedPlugin) logs() *LogBuffer { return nil }

// launchedPlugin is a plugin process QNTX started, its output kept in its own
// log file and a ring for live streaming.
type launchedPlugin struct {
	cmd          *exec.Cmd
	stdoutLogger *pluginLogger
	stderrLogger *pluginLogger
	logBuffer    *LogBuffer
	logFile      *os.File
}

// exited is what reaping a stopped plugin says. Its exit, the stop itself, is
// not a failure; a wait that fails otherwise is.
func exited(pid int, waitErr error) error {
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return nil
	}
	return errors.Wrapf(waitErr, "plugin pid %d was not reaped", pid)
}

// stop terminates the plugin process and waits for exit.
// Sends SIGTERM first for graceful shutdown (lock file cleanup), then SIGKILL.
// Uses a timeout because cmd.Wait() blocks until stdout/stderr pipes close,
// and child processes may inherit those pipes.
func (p *launchedPlugin) stop() error {
	proc := p.cmd.Process
	pid := proc.Pid
	reaped := make(chan error, 1)
	go func() { reaped <- p.cmd.Wait() }()

	// SIGTERM the entire process group first. If the process was launched with
	// Setpgid (its own group leader), this kills it and all children.
	// If not (legacy launch), the negative-pid kill returns ESRCH and we
	// fall back to signaling the process directly. A plugin no signal reaches
	// is left to the SIGKILL below, and said if that misses too.
	var termErr error
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		if sigErr := proc.Signal(os.Interrupt); sigErr != nil {
			termErr = errors.Wrapf(sigErr, "plugin pid %d reached by neither group SIGTERM (%v) nor Interrupt", pid, err)
		}
	}

	// Wait up to 3s for graceful exit
	select {
	case err := <-reaped:
		return exited(pid, err)
	case <-time.After(3 * time.Second):
	}

	// Force kill — try process group first, fall back to direct kill
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		if killErr := proc.Kill(); killErr != nil {
			return alongside(errors.Wrapf(killErr, "plugin pid %d survived SIGKILL of its group (%v) and of itself; it keeps its port and DB locks", pid, err), termErr)
		}
	}
	select {
	case err := <-reaped:
		return exited(pid, err)
	case <-time.After(2 * time.Second):
		return alongside(errors.Newf("plugin pid %d did not exit within 5s of being stopped", pid), termErr)
	}
}

// abandon kills a plugin process a failing path is giving up on and reaps it.
// A kill that misses leaves the plugin holding its port and DB locks.
func (p *launchedPlugin) abandon() error {
	pid := p.cmd.Process.Pid
	if err := p.cmd.Process.Kill(); err != nil {
		return alongside(errors.Wrapf(err, "the abandoned plugin pid %d was not killed; it may keep its port", pid), p.closeLog())
	}
	return alongside(exited(pid, p.cmd.Wait()), p.closeLog())
}

// interrupt signals the process to exit, and kills it when the signal does not reach it.
func (p *launchedPlugin) interrupt() error {
	pid := p.cmd.Process.Pid
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		if killErr := p.cmd.Process.Kill(); killErr != nil {
			return errors.Wrapf(killErr, "plugin pid %d reached by neither Interrupt (%v) nor Kill at shutdown", pid, err)
		}
	}
	return nil
}

func (p *launchedPlugin) closeLog() error {
	return errors.Wrapf(p.logFile.Close(), "failed to close the plugin log %s", p.logFile.Name())
}

func (p *launchedPlugin) logs() *LogBuffer { return p.logBuffer }

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
		joints:            make(map[string]joint),
		logger:            logger,
		rootLogger:        rootLogger,
		logDir:            "tmp",
		basePort:          DefaultPluginBasePort,
		nextPort:          DefaultPluginBasePort,
		typescriptRuntime: typescriptRuntime,
		shutdownCtx:       ctx,
		shutdownCancel:    cancel,
		accumulator:       NewPluginAccumulator(logger),
		pids:              unrecordedPids{},
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
	m.pids = newPidFile(filepath.Join(dir, name), m.logger)
}

// SetLogDir sets the directory for per-plugin log files.
func (m *PluginManager) SetLogDir(dir string) {
	m.logDir = dir
}

// SetServicesManager hands the manager the node's plugin services: a plugin
// that comes back is registered with them as a provider again, and one that
// goes is taken out. Handing them again replaces them.
func (m *PluginManager) SetServicesManager(sm *ServicesManager) {
	m.wiringMu.Lock()
	defer m.wiringMu.Unlock()
	m.servicesManager = sm
	m.joints["services"] = servicesJoint{m: m, sm: sm}
}

// SetPulseResources hands the manager Pulse's DB and handler registry, so a
// plugin that comes back has its async handlers and schedules again, and one
// that goes has them and its watchers removed.
func (m *PluginManager) SetPulseResources(db *sql.DB, registry *async.HandlerRegistry) {
	m.wiringMu.Lock()
	defer m.wiringMu.Unlock()
	m.joints["pulse"] = pulseJoint{m: m, db: db, handlers: registry}
}

// SetOnWatchersSetup adds fn to what is told after plugin watchers are written
// to DB. The server uses this to reload the watcher engine's in-memory map.
func (m *PluginManager) SetOnWatchersSetup(fn func()) {
	m.wiringMu.Lock()
	defer m.wiringMu.Unlock()
	m.watchersSetup = append(m.watchersSetup, fn)
}

// SetOnPluginRestarted adds fn to what is told after a plugin comes back.
// The server uses this to clear stale HTTP mux state (sync.Once + cached ServeMux).
func (m *PluginManager) SetOnPluginRestarted(fn func(name string)) {
	m.wiringMu.Lock()
	defer m.wiringMu.Unlock()
	m.pluginRestarted = append(m.pluginRestarted, fn)
}

// SetOnLifecycleEvent adds fn to what is told of plugin lifecycle events
// (started, stopped, restarted, enabled, disabled). The server uses this to
// write deferred news attestations to Ground.
func (m *PluginManager) SetOnLifecycleEvent(fn func(pluginName, version, event string, routes []string)) {
	m.wiringMu.Lock()
	defer m.wiringMu.Unlock()
	m.lifecycle = append(m.lifecycle, fn)
}

// emitLifecycle tells every lifecycle listener.
// Every banner emission is a lifecycle moment worth attesting.
func (m *PluginManager) emitLifecycle(name, version, event string, routes []string) {
	m.wiringMu.RLock()
	listeners := m.lifecycle
	m.wiringMu.RUnlock()
	for _, fn := range listeners {
		fn(name, version, event, routes)
	}
}

// EmitLifecycle is the exported version for callers outside the package.
func (m *PluginManager) EmitLifecycle(name, version, event string, routes []string) {
	m.emitLifecycle(name, version, event, routes)
}

// SetOnEmbeddingProviderReady adds fn to what is told when an embedding
// provider plugin is ready (on init or after restart). The server uses this to
// re-wire the embedding service with the plugin's fresh gRPC client.
func (m *PluginManager) SetOnEmbeddingProviderReady(fn func(name string, client protocol.EmbeddingServiceClient)) {
	m.wiringMu.Lock()
	defer m.wiringMu.Unlock()
	m.embeddingReady = append(m.embeddingReady, fn)
}

// SetOnPythonProviderReady adds fn to what is told when a plugin declaring
// python_provider=true finishes initialization. The server uses this to
// wire the gRPC PythonService executor for "py" element execution.
func (m *PluginManager) SetOnPythonProviderReady(fn func(name string, client protocol.PythonServiceClient)) {
	m.wiringMu.Lock()
	defer m.wiringMu.Unlock()
	m.pythonReady = append(m.pythonReady, fn)
}

// watchersSet tells every listener that plugin watchers were written to DB.
func (m *PluginManager) watchersSet() {
	m.wiringMu.RLock()
	listeners := m.watchersSetup
	m.wiringMu.RUnlock()
	for _, fn := range listeners {
		fn()
	}
}

// restarted tells every listener that a plugin came back.
func (m *PluginManager) restarted(name string) {
	m.wiringMu.RLock()
	listeners := m.pluginRestarted
	m.wiringMu.RUnlock()
	for _, fn := range listeners {
		fn(name)
	}
}

// Accumulator returns the plugin banner accumulator.
func (m *PluginManager) Accumulator() *PluginAccumulator {
	return m.accumulator
}

// SetAccumulator replaces the plugin banner accumulator.
func (m *PluginManager) SetAccumulator(acc *PluginAccumulator) {
	m.accumulator = acc
}

// joint is something of the node's a plugin is joined to when it comes up,
// and parted from when it goes.
type joint interface {
	// release frees what a plugin's old process holds of it, before the
	// process is stopped.
	release()
	// join hands a plugin that came up to it, and says the roles it took there.
	join(name string, proxy *ExternalDomainProxy) ([]string, error)
	// part takes a plugin that goes out of it.
	part(name string, proxy *ExternalDomainProxy) error
}

// wired is every joint handed over, in a fixed order.
func (m *PluginManager) wired() []joint {
	m.wiringMu.RLock()
	defer m.wiringMu.RUnlock()
	names := make([]string, 0, len(m.joints))
	for name := range m.joints {
		names = append(names, name)
	}
	sort.Strings(names)
	joints := make([]joint, 0, len(names))
	for _, name := range names {
		joints = append(joints, m.joints[name])
	}
	return joints
}

// release frees what plugin processes hold of every joint, before one is stopped.
func (m *PluginManager) release() {
	for _, j := range m.wired() {
		j.release()
	}
}

// join hands a plugin that came up to every joint, and says the roles it took.
func (m *PluginManager) join(name string, proxy *ExternalDomainProxy) ([]string, error) {
	var roles []string
	for _, j := range m.wired() {
		taken, err := j.join(name, proxy)
		roles = append(roles, taken...)
		if err != nil {
			return roles, err
		}
	}
	return roles, nil
}

// part takes a plugin that goes out of every joint.
func (m *PluginManager) part(name string, proxy *ExternalDomainProxy) error {
	var partErr error
	for _, j := range m.wired() {
		partErr = alongside(partErr, j.part(name, proxy))
	}
	return partErr
}

// pulseJoint is Pulse: a plugin's async handlers, its schedules and its watchers.
type pulseJoint struct {
	m        *PluginManager
	db       *sql.DB
	handlers *async.HandlerRegistry
}

func (pulseJoint) release() {}

func (j pulseJoint) join(name string, proxy *ExternalDomainProxy) ([]string, error) {
	j.m.wiringMu.RLock()
	sm := j.m.servicesManager
	j.m.wiringMu.RUnlock()
	for _, handlerName := range proxy.GetHandlerNames() {
		j.handlers.Replace(NewPluginProxyHandler(name, handlerName, proxy, j.db, j.m.logger, sm.OpenRunFor))
		j.m.logger.Debugw("Re-registered plugin async handler", "plugin", name, "handler", handlerName,
			"registry_key", PluginHandlerName(name, handlerName))
	}
	// Unconditional. A plugin that now declares no schedules still has to
	// say so, or what it used to declare keeps running.
	if err := SetupPluginSchedules(j.db, name, proxy.GetSchedules(), j.m.logger); err != nil {
		return nil, errors.Wrapf(err, "plugin %s came up and its schedules were not set up", name)
	}
	return nil, nil
}

func (j pulseJoint) part(name string, proxy *ExternalDomainProxy) error {
	for _, handlerName := range proxy.GetHandlerNames() {
		j.handlers.Remove(handlerName)
	}
	// Prune watchers: pass empty list so all watchers with this plugin's prefix are deleted
	if err := SetupPluginWatchers(j.db, name, nil, nil, j.m.logger); err != nil {
		return errors.Wrapf(err, "plugin %s was stopped but its watchers were not pruned — they keep firing at a plugin that is gone", name)
	}
	j.m.watchersSet()
	return nil
}

// servicesJoint is the node's plugin services: the providers a plugin serves.
type servicesJoint struct {
	m  *PluginManager
	sm *ServicesManager
}

// release cancels active ATSStore streams — frees the database mutex so a
// new process can initialize without contention.
func (j servicesJoint) release() {
	j.sm.CancelATSStreams()
}

func (j servicesJoint) join(name string, proxy *ExternalDomainProxy) ([]string, error) {
	var roles []string
	if proxy.IsLLMProvider() {
		roles = append(roles, "llm-provider")
		llmRouter, err := j.sm.GetLLMRouter()
		if err != nil {
			return roles, errors.Wrapf(err, "LLM provider %s is not joined", name)
		}
		llmRouter.RegisterProvider(name, proxy.LLMServiceClient())
		j.m.logger.Debugf("Re-registered LLM provider '%s' after restart", name)
	}
	if proxy.IsSearchProvider() {
		roles = append(roles, "search-provider")
		searchRouter, err := j.sm.GetSearchRouter()
		if err != nil {
			return roles, errors.Wrapf(err, "search provider %s is not joined", name)
		}
		searchRouter.RegisterProvider(name, proxy.SearchServiceClient())
		j.m.logger.Debugf("Re-registered search provider '%s' after restart", name)
	}
	j.m.wiringMu.RLock()
	embeddingReady, pythonReady := j.m.embeddingReady, j.m.pythonReady
	j.m.wiringMu.RUnlock()
	if proxy.IsEmbeddingProvider() {
		roles = append(roles, "embedding-provider")
		for _, fn := range embeddingReady {
			fn(name, proxy.EmbeddingServiceClient())
		}
	}
	if proxy.IsPythonProvider() {
		roles = append(roles, "python-provider")
		for _, fn := range pythonReady {
			fn(name, proxy.PythonServiceClient())
		}
	}
	return roles, nil
}

// part unregisters a plugin's providers so observers stop routing to dead connections.
func (j servicesJoint) part(name string, proxy *ExternalDomainProxy) error {
	var partErr error
	if proxy.IsSearchProvider() {
		searchRouter, err := j.sm.GetSearchRouter()
		if err != nil {
			partErr = alongside(partErr, errors.Wrapf(err, "search provider %s was never joined", name))
		} else {
			searchRouter.UnregisterProvider(name)
		}
	}
	if proxy.IsLLMProvider() {
		llmRouter, err := j.sm.GetLLMRouter()
		if err != nil {
			partErr = alongside(partErr, errors.Wrapf(err, "LLM provider %s was never joined", name))
		} else {
			llmRouter.UnregisterProvider(name)
		}
	}
	return partErr
}

// comeback is what a plugin that failed is handed back to when it loads again.
type comeback interface {
	// back takes the plugin, loaded again, back in.
	back(ctx context.Context, name string) error
	// down says the plugin did not load this time.
	down(name string, err error)
}

// bootComeback is a plugin that failed while the node was booting. Boot
// registers what loaded once it is done loading; a plugin that loads after
// that runs, and is not registered by this.
type bootComeback struct{}

func (bootComeback) back(context.Context, string) error { return nil }
func (bootComeback) down(string, error)                 {}

// registryComeback is a plugin that failed on a running node: it is registered
// and initialized again when it comes back, and marked failed until it does.
type registryComeback struct {
	m        *PluginManager
	registry *plugin.Registry
	services plugin.ServiceRegistry
}

func (c registryComeback) back(ctx context.Context, name string) error {
	return c.m.registerRestarted(ctx, name, c.registry, c.services, BannerRecovered)
}

func (c registryComeback) down(name string, err error) {
	c.registry.MarkFailed(name, err.Error())
}

// LoadPlugins loads and connects to plugins from configuration.
// Enabled plugins are retried forever — enabled means the operator is certain
// this plugin must run. Disabled plugins are skipped entirely.
// A plugin that does not load is held in the failed plugins while it is retried.
func (m *PluginManager) LoadPlugins(ctx context.Context, configs []PluginConfig) error {
	// Kill plugin processes orphaned by a previous run
	if err := m.pids.CleanStale(); err != nil {
		return errors.Wrap(err, "no plugin is launched while what a previous run left is not cleaned up")
	}

	for _, pluginCfg := range configs {
		if !pluginCfg.Enabled {
			m.logger.Infow("Skipping disabled plugin", "name", pluginCfg.Name)
			continue
		}

		if err := m.loadPlugin(ctx, pluginCfg); err != nil {
			m.mu.Lock()
			m.failedPlugins[pluginCfg.Name] = err.Error()
			m.mu.Unlock()

			// Enabled means forever — retry until server shuts down.
			// Registration happens later in loadPluginsAsync.
			go m.retryPluginForever(m.shutdownCtx, pluginCfg, bootComeback{})
		}
	}

	return nil
}

// retryPluginForever kills and relaunches a plugin process until it loads.
// Each cycle: launch process → try connecting 3 times (1s, 3s, 9s) → if all fail, kill and relaunch.
// Each failed cycle is held in the failed plugins and told to cb; the first
// three are said, then one a minute.
func (m *PluginManager) retryPluginForever(ctx context.Context, pluginCfg PluginConfig, cb comeback) {
	// Register per-plugin cancel so EnablePlugin can stop this loop
	retryCtx, retryCancel := context.WithCancel(ctx)
	defer retryCancel()
	m.mu.Lock()
	m.retryCancels[pluginCfg.Name] = retryCancel
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.retryCancels, pluginCfg.Name)
		m.mu.Unlock()
	}()

	cycle := 0
	said := time.Now()
	for {
		cycle++
		if err := retryCtx.Err(); err != nil {
			m.logger.Debugf("Retry cancelled for plugin '%s'", pluginCfg.Name)
			return
		}

		say := cycle <= 3 || time.Since(said) >= time.Minute
		if say {
			said = time.Now()
			m.logger.Infof("Restarting plugin '%s' process (cycle %d)", pluginCfg.Name, cycle)
		}

		err := m.relaunch(retryCtx, pluginCfg)
		if err == nil {
			m.mu.Lock()
			delete(m.failedPlugins, pluginCfg.Name)
			m.mu.Unlock()

			if err := cb.back(retryCtx, pluginCfg.Name); err != nil {
				m.mu.Lock()
				m.failedPlugins[pluginCfg.Name] = err.Error()
				m.mu.Unlock()
			}

			// Clear stale HTTP mux state so next request re-initializes
			m.restarted(pluginCfg.Name)

			m.logger.Debugf("Plugin '%s' loaded successfully on restart cycle %d", pluginCfg.Name, cycle)
			return
		}

		m.mu.Lock()
		m.failedPlugins[pluginCfg.Name] = err.Error()
		m.mu.Unlock()
		cb.down(pluginCfg.Name, err)
		if say {
			m.logger.Infof("Plugin '%s' did not load on restart cycle %d; trying again in 5s: %v", pluginCfg.Name, cycle, err)
		}

		// Wait before next full restart cycle
		select {
		case <-retryCtx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// relaunch stops what is left of a plugin and loads it again.
func (m *PluginManager) relaunch(ctx context.Context, pluginCfg PluginConfig) error {
	// Clean up previous state so loadPlugin doesn't see "already loaded"
	m.mu.Lock()
	var stopErr error
	if old, exists := m.plugins[pluginCfg.Name]; exists {
		stopErr = alongside(old.proc.stop(), old.proc.closeLog())
		delete(m.plugins, pluginCfg.Name)
	}
	m.mu.Unlock()
	if stopErr != nil {
		return errors.Wrapf(stopErr, "what was left of plugin %s did not stop, so it was not launched again", pluginCfg.Name)
	}
	return m.loadPlugin(ctx, pluginCfg)
}

// loadPlugin loads a single plugin.
// Lock is only held for state checks and the final registration — all I/O
// (process launch, connection attempts, metadata fetch) runs unlocked.
func (m *PluginManager) loadPlugin(ctx context.Context, pluginCfg PluginConfig) error {
	// Check if already loaded
	m.mu.RLock()
	_, exists := m.plugins[pluginCfg.Name]
	m.mu.RUnlock()
	if exists {
		return errors.Newf("plugin already loaded: %s", pluginCfg.Name)
	}

	proc, addr, port, err := m.start(ctx, pluginCfg)
	if err != nil {
		return err
	}

	client, err := m.connect(ctx, pluginCfg.Name, addr)
	if err != nil {
		return alongside(err, proc.abandon())
	}

	// Validate plugin metadata matches config
	meta := client.Metadata()
	if meta.Name != pluginCfg.Name {
		err := errors.Newf("plugin metadata mismatch: %s reports name='%s' but config expects '%s'",
			pluginCfg.Source, meta.Name, pluginCfg.Name)
		return alongside(errors.WithHint(err, "verify the correct plugin binary is installed or update the plugin name in config"), proc.abandon())
	}

	// Watcher reload, so the engine picks up plugin-declared watchers
	client.OnWatchersSetup = m.watchersSet
	m.applyWebSocket(client)

	// Register — lock only for the final state write
	m.mu.Lock()
	m.plugins[pluginCfg.Name] = &managedPlugin{
		pluginCfg: pluginCfg,
		client:    client,
		proc:      proc,
		port:      port,
	}
	m.mu.Unlock()

	m.logger.Debugf("Plugin '%s' v%s loaded and ready - %s",
		pluginCfg.Name, meta.Version, meta.Description)

	return nil
}

// start brings a plugin to running from its source: launched from its
// binary, or reached at its address. It says the address to connect to.
func (m *PluginManager) start(ctx context.Context, pluginCfg PluginConfig) (running, string, int, error) {
	switch pluginCfg.Source.how {
	case sourceAddress:
		m.logger.Debugw("Connecting to existing plugin", "name", pluginCfg.Name, "address", pluginCfg.Source.at)
		return attachedPlugin{}, pluginCfg.Source.at, 0, nil
	case sourceBinary:
		return m.launch(ctx, pluginCfg)
	}
	err := errors.Newf("plugin %s names %s: neither a binary to launch nor an address it runs at", pluginCfg.Name, pluginCfg.Source)
	return nil, "", 0, errors.WithHint(err, "a plugin is found as a binary in [plugin] paths, or reached with RunningAt")
}

// launch starts a plugin's binary and waits until it answers as itself.
func (m *PluginManager) launch(ctx context.Context, pluginCfg PluginConfig) (running, string, int, error) {
	port, err := m.allocatePort()
	if err != nil {
		return nil, "", 0, errors.Wrapf(err, "no port to launch plugin %s on", pluginCfg.Name)
	}

	proc, actualPort, err := m.launchPlugin(ctx, pluginCfg, port)
	if err != nil {
		return nil, "", 0, errors.Wrapf(err, "failed to launch plugin %s (%s, port=%d)",
			pluginCfg.Name, pluginCfg.Source, port)
	}
	if actualPort != port {
		m.logger.Debugw("Plugin bound to different port", "name", pluginCfg.Name, "requested_port", port, "actual_port", actualPort)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", actualPort)
	pid := proc.cmd.Process.Pid

	m.logger.Debugf("Started '%s' plugin process (pid=%d, port=%d, addr=%s)",
		pluginCfg.Name, pid, actualPort, addr)

	if err := m.pids.Add(pid); err != nil {
		return nil, "", 0, alongside(errors.Wrapf(err, "plugin %s was launched as pid %d and not recorded, so it is not kept running", pluginCfg.Name, pid), proc.abandon())
	}

	if err := m.waitForPlugin(ctx, pluginCfg.Name, addr, 5*time.Second); err != nil {
		return nil, "", 0, alongside(errors.Wrapf(err, "plugin %s failed to start (%s, addr=%s, pid=%d)",
			pluginCfg.Name, pluginCfg.Source, addr, pid), proc.abandon())
	}
	return proc, addr, actualPort, nil
}

// connect reaches a plugin's gRPC server with retry: 1s, 3s, 9s backoff.
// If all attempts fail, caller (retryPluginForever) kills the process and relaunches.
func (m *PluginManager) connect(ctx context.Context, name, addr string) (*ExternalDomainProxy, error) {
	connectBackoffs := []time.Duration{1 * time.Second, 3 * time.Second, 9 * time.Second}
	var attempts []string
	for attempt, backoff := range connectBackoffs {
		client, err := NewExternalDomainProxy(addr, m.logger)
		if err == nil {
			return client, nil
		}
		attempts = append(attempts, fmt.Sprintf("attempt %d: %v", attempt+1, err))
		m.logger.Debugw("Connection attempt to plugin failed",
			"plugin", name, "addr", addr, "attempt", attempt+1, "of", len(connectBackoffs), "error", err)
		if attempt < len(connectBackoffs)-1 {
			select {
			case <-ctx.Done():
				return nil, errors.Wrapf(ctx.Err(), "stopped connecting to plugin %s at %s: %s", name, addr, strings.Join(attempts, "; "))
			case <-time.After(backoff):
			}
		}
	}
	return nil, errors.Newf("failed to connect to plugin %s at %s after %d attempts: %s",
		name, addr, len(connectBackoffs), strings.Join(attempts, "; "))
}

// allocatePort finds the next available port for a plugin.
// Probes each port to ensure it's actually free before returning it.
// This prevents connecting to orphaned plugin processes from previous runs.
// Thread-safe for concurrent plugin loading.
func (m *PluginManager) allocatePort() (int, error) {
	m.portMu.Lock()
	defer m.portMu.Unlock()

	const maxAttempts = 1000
	first := m.nextPort
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
		if err := ln.Close(); err != nil {
			// A probe listener that will not close is a port that reads free
			// while something still holds it: it is not handed out.
			m.logger.Warnw("Port probe listener close failed; skipping the port", "port", port, "error", err)
			continue
		}

		m.logger.Debugw("Allocated port for plugin", "port", port, "next_port", m.nextPort)
		return port, nil
	}

	return 0, errors.Newf("no free port among the %d from %d", maxAttempts, first)
}

// outputStream is which of a plugin's outputs a line came from.
type outputStream string

const (
	stdoutStream outputStream = "stdout"
	stderrStream outputStream = "stderr"
)

// launchPlugin starts a plugin binary and returns its process and the port it serves on.
// If the plugin outputs QNTX_PLUGIN_PORT=XXXX, that port is returned instead of the requested port.
// A plugin runs with a log of its own; one whose log cannot be opened is not launched.
func (m *PluginManager) launchPlugin(ctx context.Context, pluginCfg PluginConfig, port int) (*launchedPlugin, int, error) {
	binary := pluginCfg.Source.at

	// Resolve relative paths
	if !filepath.IsAbs(binary) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, 0, errors.Wrapf(err, "failed to get home directory for plugin %s", pluginCfg.Name)
		}
		binary = filepath.Join(home, ".qntx", "plugins", binary)
	}

	info, err := os.Stat(binary)
	if err != nil {
		err = errors.Wrapf(err, "plugin binary for %s not readable at %s", pluginCfg.Name, binary)
		return nil, 0, errors.WithHint(err, "install the plugin binary to ~/.qntx/plugins/ or specify the full path in config")
	}

	// Detect TypeScript plugins and wrap with Bun runtime
	var cmd *exec.Cmd
	isTypeScriptPlugin := strings.HasSuffix(binary, ".ts") || isPackageJSONPlugin(binary, info)

	if isTypeScriptPlugin {
		// TypeScript plugin - launch via Bun runtime
		runtimePath := m.typescriptRuntime
		runtimeInfo, err := os.Stat(runtimePath)
		if err != nil {
			err = errors.Wrapf(err, "TypeScript runtime for plugin %s not found at %q", pluginCfg.Name, runtimePath)
			return nil, 0, errors.WithHint(err,
				"set plugin.runtime.typescript_runtime in am.toml or QNTX_ROOT environment variable")
		}
		if runtimeInfo.IsDir() {
			return nil, 0, errors.Newf("TypeScript runtime for plugin %s at %s is a directory, not main.ts", pluginCfg.Name, runtimePath)
		}

		args := []string{
			"run",
			runtimePath,
			"--plugin-path", binary,
			"--grpc-port", strconv.Itoa(port),
		}
		args = append(args, pluginCfg.Args...)

		m.logger.Debugw("Launching TypeScript plugin via Bun runtime",
			"name", pluginCfg.Name,
			"plugin_path", binary,
			"runtime_path", runtimePath,
			"port", port)

		// TODO(#624): Replace primitive exec.Command with Runtime abstraction.
		// Current approach assumes "bun" in PATH, no version checking, delayed failure.
		cmd = exec.Command("bun", args...)
	} else {
		// Native binary plugin (Go, Python, etc.)
		args := append([]string{"--port", strconv.Itoa(port)}, pluginCfg.Args...)
		cmd = exec.Command(binary, args...)
	}

	// Using exec.Command instead of exec.CommandContext intentionally.
	// Plugins are not killed on context cancellation — graceful shutdown sends
	// gRPC Shutdown() first. Orphans from crashes are cleaned up on next startup
	// via pidfile tracking (see pidfile.go).
	cmd.Env = os.Environ()

	// Open per-plugin log file (e.g. tmp/myplugin.log).
	// Plugin output goes here instead of the main QNTX log.
	if err := os.MkdirAll(m.logDir, 0755); err != nil {
		return nil, 0, errors.Wrapf(err, "plugin %s has no log of its own: %s could not be made", pluginCfg.Name, m.logDir)
	}
	logPath := filepath.Join(m.logDir, pluginCfg.Name+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "plugin %s has no log of its own: %s did not open", pluginCfg.Name, logPath)
	}
	marker := fmt.Sprintf("\n========== %s START %s ==========\n",
		strings.ToUpper(pluginCfg.Name), time.Now().Format("2006-01-02T15:04:05.000"))
	if written, err := logFile.WriteString(marker); err != nil {
		err = errors.Wrapf(err, "plugin %s log %s took %d of the %d bytes of its start marker", pluginCfg.Name, logPath, written, len(marker))
		return nil, 0, sqlclose.With(err, logFile.Close(), logPath)
	}

	// Create a channel to receive the actual port from plugin output
	portChan := make(chan int, 1)

	// Create shared log buffer for live streaming
	logBuf := NewLogBuffer(200)

	// Capture output for debugging and port discovery. The port is announced
	// on stdout only.
	stdoutLogger := &pluginLogger{
		file:      logFile,
		stream:    stdoutStream,
		level:     "info",
		portChan:  portChan,
		logBuffer: logBuf,
	}
	stderrLogger := &pluginLogger{
		file:      logFile,
		stream:    stderrStream,
		level:     "debug",
		portChan:  portChan,
		logBuffer: logBuf,
	}

	cmd.Stdout = stdoutLogger
	cmd.Stderr = stderrLogger

	if err := cmd.Start(); err != nil {
		err = errors.Wrapf(err, "failed to start plugin %s (cmd=%v)", pluginCfg.Name, cmd.Args)
		return nil, 0, sqlclose.With(err, logFile.Close(), logPath)
	}

	// Wait for the port announcement with a short timeout (2 seconds)
	// The plugin should announce its port almost immediately after binding
	actualPort := port // Default to requested port
	select {
	case discoveredPort := <-portChan:
		actualPort = discoveredPort
		m.logger.Debugw("Discovered plugin port from stdout",
			"name", pluginCfg.Name,
			"requested_port", port,
			"actual_port", actualPort)
	case <-time.After(2 * time.Second):
		// No port announcement - plugin is using the requested port
		// This is normal for older plugins that don't support auto-increment
		m.logger.Debugw("No port announcement from plugin, assuming requested port",
			"name", pluginCfg.Name,
			"port", port)
	}

	return &launchedPlugin{
		cmd:          cmd,
		stdoutLogger: stdoutLogger,
		stderrLogger: stderrLogger,
		logBuffer:    logBuf,
		logFile:      logFile,
	}, actualPort, nil
}

// waitForPlugin waits for a plugin's gRPC server to become ready.
// This polls the gRPC metadata endpoint rather than just checking TCP connectivity
// to ensure the plugin is actually ready to handle requests.
// It also verifies that the correct plugin (by name) is responding at the given address.
func (m *PluginManager) waitForPlugin(ctx context.Context, expectedName string, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	start := time.Now()
	attempt := 0
	// What answered at addr, for the timeout to say.
	answered := "nothing"

	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
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
		sqlclose.Log(conn.Close(), m.logger, "the readiness probe connection")

		if metaErr != nil {
			answered = "a gRPC server whose Metadata failed: " + metaErr.Error()
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

		answered = fmt.Sprintf("plugin '%s'", metaResp.Name)
		time.Sleep(100 * time.Millisecond)
	}

	err := errors.Newf("timeout after %d attempts (%dms) waiting for plugin '%s' gRPC service at %s; %s answered there",
		attempt, time.Since(start).Milliseconds(), expectedName, addr, answered)
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
		return p.proc.logs()
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

// ConfigureWebSocket sets WebSocket configuration on all loaded plugins, and
// on every plugin loaded after.
func (m *PluginManager) ConfigureWebSocket(keepalive KeepaliveConfig, wsConfig WebSocketConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.wsConfigured, m.wsKeepalive, m.wsConfig = true, keepalive, wsConfig
	for _, p := range m.plugins {
		p.client.SetWebSocketConfig(keepalive, wsConfig)
	}
	m.logger.Debugw("WebSocket configuration applied to plugins",
		"keepalive_enabled", keepalive.Enabled,
		"ping_interval", keepalive.PingInterval,
		"allowed_origins_count", len(wsConfig.AllowedOrigins),
	)
}

// applyWebSocket hands a newly connected plugin the node's WebSocket settings,
// once ConfigureWebSocket has set them.
func (m *PluginManager) applyWebSocket(client *ExternalDomainProxy) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.wsConfigured {
		client.SetWebSocketConfig(m.wsKeepalive, m.wsConfig)
	}
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
// A plugin not loaded (e.g. removed by the health poller or the retry loop)
// is found in searchPaths and enabled from scratch.
func (m *PluginManager) RestartPlugin(ctx context.Context, name string, searchPaths []string, registry *plugin.Registry, services plugin.ServiceRegistry) error {
	p, loaded, stopErr := m.takeLoaded(name)
	if !loaded {
		// Plugin not in map — kill any stale OS process, then discover + enable from scratch.
		m.logger.Infow("RestartPlugin: plugin not in map, killing stale processes and re-enabling",
			"plugin", name)
		m.killStalePluginProcesses(name)
		registry.Unregister(name)
		return m.EnablePlugin(ctx, name, searchPaths, registry, services)
	}
	return m.relaunchLoaded(ctx, name, p, stopErr, registry, services)
}

// restartLoaded kills a running plugin and relaunches it, as RestartPlugin
// does; a plugin that is no longer loaded is refused.
func (m *PluginManager) restartLoaded(ctx context.Context, name string, registry *plugin.Registry, services plugin.ServiceRegistry) error {
	p, loaded, stopErr := m.takeLoaded(name)
	if !loaded {
		return errors.Newf("plugin not loaded: %s", name)
	}
	return m.relaunchLoaded(ctx, name, p, stopErr, registry, services)
}

// takeLoaded cancels a plugin's retry loop and, when it is loaded, stops its
// process and takes it out of the manager. It says whether it was loaded, and
// what stopping it did not do.
func (m *PluginManager) takeLoaded(name string) (*managedPlugin, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	m.logger.Debugw("RestartPlugin: map check",
		"plugin", name,
		"in_map", exists,
		"all_plugins", pluginNames,
		"all_retries", retryNames)

	// What the old process holds is freed before it is stopped.
	m.release()
	if !exists {
		return p, false, nil
	}
	stopErr := alongside(p.proc.stop(), p.proc.closeLog())
	delete(m.plugins, name)
	return p, true, stopErr
}

// relaunchLoaded launches a plugin taken out by takeLoaded again, and joins it.
func (m *PluginManager) relaunchLoaded(ctx context.Context, name string, p *managedPlugin, stopErr error, registry *plugin.Registry, services plugin.ServiceRegistry) error {
	if stopErr != nil {
		stopErr = errors.Wrapf(stopErr, "plugin %s did not stop, so it was not launched again", name)
		registry.MarkFailed(name, stopErr.Error())
		return stopErr
	}
	pluginCfg := p.pluginCfg

	// Close the old gRPC connection; its process is gone.
	closeErr := errors.Wrapf(p.client.Close(), "the old connection to plugin %s did not close", name)

	// Unregister from registry so Register doesn't hit "already registered"
	registry.Unregister(name)

	m.logger.Debugf("Killed plugin '%s', relaunching from %s", name, pluginCfg.Source)

	// Relaunch — if it fails, retry forever in background. The failure is
	// held in the failed plugins and the registry while it is retried.
	if err := m.loadPlugin(ctx, pluginCfg); err != nil {
		m.mu.Lock()
		m.failedPlugins[name] = err.Error()
		m.mu.Unlock()
		registry.MarkFailed(name, err.Error())
		m.logger.Infof("Restart of '%s' failed, retrying in background: %v", name, err)
		go m.retryPluginForever(m.shutdownCtx, pluginCfg, registryComeback{m: m, registry: registry, services: services})
		return closeErr
	}

	initErr := m.registerRestarted(ctx, name, registry, services, BannerRecovered)

	// Clear stale HTTP mux and pre-register new proxy routes.
	// Must run AFTER registerRestarted which registers the plugin in the registry.
	m.restarted(pluginCfg.Name)

	return alongside(initErr, closeErr)
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
		if err := proc.Kill(); err != nil {
			m.logger.Warnw("Stale plugin process not killed; it may keep its port",
				"plugin", name, "pid", pid, "error", err)
			continue
		}
		// A stale process from a previous run is not this process's child, so
		// Wait cannot reap it — ECHILD here is that fact, not a failure.
		state, err := proc.Wait()
		if err != nil {
			m.logger.Debugw("Stale plugin process not reaped", "plugin", name, "pid", pid, "error", err)
			continue
		}
		m.logger.Debugw("Stale plugin process reaped", "plugin", name, "pid", pid, "state", state.String())
	}
}

// registerRestarted re-registers a successfully relaunched plugin with the
// registry, initializes it with services and joins it to what the node has
// handed over. Emits the banner after its health check completes (async) so
// it shows actual health, not "initializing". A plugin that does not come all
// the way up is marked failed, its banner says why, and the caller is told.
func (m *PluginManager) registerRestarted(ctx context.Context, name string, registry *plugin.Registry, services plugin.ServiceRegistry, reason BannerReason) error {
	m.mu.RLock()
	p, exists := m.plugins[name]
	m.mu.RUnlock()
	if !exists {
		return errors.Newf("plugin %s loaded and was gone before it was registered", name)
	}
	proxy := p.client

	// Unregister first to handle races between health poller restarts and
	// manual restarts — both can call registerRestarted concurrently.
	registry.Unregister(name)
	if err := registry.Register(proxy); err != nil {
		return m.notJoined(name, proxy, registry, reason, errors.Wrapf(err, "plugin %s started and was not registered", name))
	}
	registry.MarkReady(name)

	// Initialize with a 30s deadline. Plugin ATS connectivity checks can take
	// 10-15s when the RustStore mutex is contended (5s watchdog alerts).
	// Use a goroutine + select so we never block banner emission.
	m.logger.Debugw("registerRestarted: calling Initialize", "plugin", name)
	initDone := make(chan error, 1)
	go func() {
		initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer initCancel()
		initDone <- proxy.Initialize(initCtx, services)
	}()
	select {
	case err := <-initDone:
		m.logger.Debugw("registerRestarted: Initialize returned", "plugin", name)
		// The plugin is running and did not take its config: that is a
		// failure, and whoever enabled or restarted it is told so.
		if err != nil {
			return m.notJoined(name, proxy, registry, reason, errors.Wrapf(err, "plugin %s did not initialize", name))
		}
	case <-time.After(30 * time.Second):
		return m.notJoined(name, proxy, registry, reason, errors.Newf("plugin %s did not answer Initialize within 30s", name))
	}

	roles, err := m.join(name, proxy)
	if err != nil {
		return m.notJoined(name, proxy, registry, reason, err)
	}

	// Populate accumulator for banner
	meta := proxy.Metadata()
	m.accumulator.SetLoading(name, meta.Version)
	m.accumulator.SetRoles(name, roles)
	m.accumulator.SetHandlers(name, proxy.GetHandlerNames(), ScheduleNames(proxy.GetSchedules()), WatcherNames(proxy.GetWatchers()), UnfilteredWatcherNames(proxy.GetWatchers()))
	routeStrs := proxy.SigilRoutes()
	m.accumulator.SetHTTPRoutes(name, routeStrs)
	// Collect health asynchronously — synchronous Health() blocks plugin restart
	// while the plugin makes ATS calls back to QNTX.
	// Emit the banner inside the goroutine so it shows actual health status.
	m.logger.Debugw("registerRestarted: launching banner goroutine", "plugin", name, "reason", reason)
	go func() {
		m.logger.Debugw("registerRestarted: calling Health()", "plugin", name)
		healthCtx, hCancel := context.WithTimeout(context.Background(), 5*time.Second)
		health := proxy.Health(healthCtx)
		hCancel()
		m.logger.Debugw("registerRestarted: Health() returned", "plugin", name, "healthy", health.Healthy, "message", health.Message)
		details := make(map[string]string)
		for k, v := range health.Details {
			if s, ok := v.(string); ok {
				details[k] = s
			}
		}
		m.accumulator.SetHealth(name, health.Healthy, health.Message, details)
		m.accumulator.Emit(name, reason)
		m.logger.Debugw("registerRestarted: banner emitted", "plugin", name, "reason", reason)
		m.emitLifecycle(name, meta.Version, string(reason), routeStrs)
	}()
	m.logger.Debugw("registerRestarted: completed", "plugin", name)
	return nil
}

// notJoined marks a plugin that came up only part of the way failed, says why
// in its banner, and returns why.
func (m *PluginManager) notJoined(name string, proxy *ExternalDomainProxy, registry *plugin.Registry, reason BannerReason, err error) error {
	registry.MarkFailed(name, err.Error())
	m.accumulator.SetLoading(name, proxy.Metadata().Version)
	m.accumulator.SetFailed(name, err.Error())
	m.accumulator.Emit(name, reason)
	return err
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

	// The build the runner delivered, or one placed by hand. Never fetched (ADR-043).
	pluginCfg, err := discoverPlugin(name, searchPaths, m.logger)
	if err != nil {
		return errors.Wrapf(err, "failed to discover plugin '%s'", name)
	}

	// Load (launch process + connect gRPC)
	if err := m.loadPlugin(ctx, pluginCfg); err != nil {
		return errors.Wrapf(err, "failed to load plugin '%s'", name)
	}

	// Register + initialize + setup handlers/watchers/schedules/providers
	// Banner emits asynchronously after health check completes
	initErr := m.registerRestarted(ctx, name, registry, services, BannerEnabled)

	// Register HTTP/WS routes for hot-swapped plugin
	m.restarted(name)

	return initErr
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

	// What the process holds is freed before it is stopped.
	m.release()

	// Shutdown via gRPC; the kill below answers a plugin that does not, and
	// its banner says which ended it.
	ending := "shut down"
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := client.Shutdown(shutdownCtx); err != nil {
		ending = "killed: its shutdown RPC failed: " + err.Error()
	}
	cancel()

	// Kill process and wait for exit so file locks are released
	// before the new process launches.
	stopErr := alongside(p.proc.stop(), p.proc.closeLog())

	// Unregister from plugin registry and mark stopped (not restarting)
	registry.Unregister(name)
	registry.MarkStopped(name)

	// Out of Pulse and the services: its handlers, watchers and providers.
	partErr := m.part(name, client)

	// Emit disabled banner
	m.accumulator.SetLoading(name, meta.Version)
	m.accumulator.SetHealth(name, true, ending, nil)
	m.accumulator.Emit(name, BannerDisabled)

	m.emitLifecycle(name, meta.Version, "disabled", nil)

	return alongside(errors.Wrapf(stopErr, "plugin %s did not stop", name), partErr)
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

// StopWatching stops health polling and every retry, so nothing restarts a
// plugin the node is shutting down.
func (m *PluginManager) StopWatching() {
	m.shutdownCancel()
}

// Watching reports whether health polling and retries still run.
func (m *PluginManager) Watching() bool {
	err := m.shutdownCtx.Err()
	return err == nil
}

// Shutdown stops all managed plugins and retry goroutines. Every plugin is
// stopped; what did not go cleanly is the error.
func (m *PluginManager) Shutdown(ctx context.Context) error {
	// Stop retry goroutines first
	m.shutdownCancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	var shutdownErr error

	for name, p := range m.plugins {
		meta := p.client.Metadata()

		// Shutdown the plugin via gRPC
		if err := p.client.Shutdown(ctx); err != nil {
			shutdownErr = alongside(shutdownErr, errors.Wrapf(err, "plugin %s did not shut down", name))
		}

		// Signal the process to exit
		shutdownErr = alongside(shutdownErr, p.proc.interrupt())

		// Close per-plugin log file
		shutdownErr = alongside(shutdownErr, p.proc.closeLog())

		m.emitLifecycle(name, meta.Version, "stopped", nil)
	}

	m.plugins = make(map[string]*managedPlugin)

	// Clean shutdown — remove PID file so next startup doesn't kill anything
	return alongside(shutdownErr, m.pids.Remove())
}

// pluginLogger captures plugin stdout/stderr, writes to a per-plugin log file,
// and feeds the LogBuffer for WebSocket live streaming.
type pluginLogger struct {
	file      *os.File // per-plugin log file (e.g. tmp/myplugin.log)
	stream    outputStream
	level     string
	buf       strings.Builder
	portChan  chan int   // where a port announced on stdout is sent
	logBuffer *LogBuffer // ring buffer for log streaming
	// This is the log path, so a failed file write has nowhere to go but the
	// live stream. Said once, or every line would repeat it.
	fileFailed bool
}

// note puts a message from the log path itself into the live stream, the only
// destination left when the file is the thing that failed.
func (l *pluginLogger) note(level, line string) {
	l.logBuffer.Write(LogEntry{Timestamp: time.Now(), Level: level, Line: line, Source: "qntx"})
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

		line = strings.TrimSpace(line)

		// Check for port announcement (QNTX_PLUGIN_PORT=9001)
		if l.stream == stdoutStream && strings.HasPrefix(line, "QNTX_PLUGIN_PORT=") {
			portStr := strings.TrimPrefix(line, "QNTX_PLUGIN_PORT=")
			port, portErr := strconv.Atoi(portStr)
			if portErr != nil {
				// Ignored silently, the plugin never announces a port and
				// startup waits on the channel with nothing to explain it.
				l.note("error", "QNTX_PLUGIN_PORT is not a number: "+portStr)
				continue
			}
			select {
			case l.portChan <- port:
				// Port sent successfully
			default:
				// A port was announced already; the first is the one waited on.
			}
			// Don't log the raw QNTX_PLUGIN_PORT line - it's internal protocol
			continue
		}

		// A JSON log entry says its own level.
		actualLevel := l.level
		var logEntry map[string]any
		if err := json.Unmarshal([]byte(line), &logEntry); err == nil {
			if level, says := logEntry["level"].(string); says {
				actualLevel = level
			}
		}

		// Write to log buffer for live streaming
		l.logBuffer.Write(LogEntry{
			Timestamp: time.Now(),
			Level:     actualLevel,
			Line:      line,
			Source:    string(l.stream),
		})

		// Write to per-plugin log file with timestamp and level
		ts := time.Now().Format("2006-01-02T15:04:05.000")
		if written, err := fmt.Fprintf(l.file, "%s\t%s\t%s\n", ts, strings.ToUpper(actualLevel), line); err != nil && !l.fileFailed {
			l.fileFailed = true
			l.note("error", fmt.Sprintf("this log file stopped accepting writes after %d bytes of a line, so it is now incomplete: %v", written, err))
		}
	}
	return len(p), nil
}
