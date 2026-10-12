package server

import (
	"path/filepath"
	"sync"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/plugin"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	errors "github.com/teranos/sacred-error"
)

type pluginServicesSubsystem struct{}

func (pluginServicesSubsystem) Name() string { return "plugin-services" }

func (pluginServicesSubsystem) Init(s *QNTXServer) error {
	pluginRegistry := s.pluginRegistry
	s.pluginHandler = NewPluginHandler(pluginRegistry, s.logger, s.pluginHealth,
		s.pluginSigilRows, s.pluginRecords().Plugins)
	grpcplugin.SetPluginRecords(s.pluginRecords())
	s.statusLineHandler = NewStatusLineHandler(pluginRegistry, s.logger, s.pluginHealth,
		// Fetched per request: the backend supplies the watcher store after
		// this handler is built.
		func() storage.Watchers { return s.held.ServedUniverse().Watchers() },
		func() *handlerFailureLog { return s.handlerFailures },
		s)
	s.statusLineHandler.news = func() *newsLog { return s.news }
	// The auth subsystem ran first and left how the peers are asked, or noPeers.
	s.statusLineHandler.peers = s.askPeers

	queue := s.daemon.GetQueue()

	// Start gRPC services for plugins (Issue #138)
	// These services allow plugins to call back to QNTX core
	// The node DID subsystem runs first and is fatal, so the node has its DID here.
	servicesManager := grpcplugin.NewServicesManager(s.deps.cfg.LLM, s.deps.cfg.Fetch, s.nodeDID.DID, s.logger)
	filesDir := filepath.Join(filepath.Dir(s.dbPath), "files")

	// Services that did not start are held with why: each plugin asking for
	// one in its Initialize is refused with it, and the node boots without.
	var endpoints grpcplugin.Endpoints
	if err := servicesManager.Start(s.ctx, s.held.Served(), queue, s.scheduleStore, filesDir); err != nil {
		endpoints = grpcplugin.UnservedEndpoints(errors.Wrap(err, "the node's plugin services did not start, so no plugin has ATS, queue or schedule access"))
	} else {
		endpoints = grpcplugin.ServedEndpoints(servicesManager)
		started := servicesManager.Endpoints()
		s.logger.Debugw("Plugin services started",
			"ats_store", started.ATSStore,
			"queue", started.Queue,
			"schedule", started.Schedule,
			"file_service", started.FileService,
			"llm", started.LLM,
			"embedding", started.Embedding,
			"search", started.Search,
			"mail", started.Mail,
		)
	}

	// Wrap config provider to inject service endpoints for plugins. A plugin
	// whose record names a namespace is handed a token of its own for it.
	configProvider := grpcplugin.NewConfigProvider(endpoints, s.pluginToken, s.logger)
	services := plugin.NewServiceRegistry(pluginRegistry, s.nodeDB, s.logger, s.held.Served(), configProvider, queue)

	// Wire version resolver: ATSStore and FetchService auto-stamp source_version
	// from the plugin registry, so individual plugins don't need to set it.
	servicesManager.SetVersionResolver(func(source string) (string, bool) {
		if p, ok := pluginRegistry.Get(source); ok {
			return p.Metadata().Version, true
		}
		return "", false
	})

	// A plugin answering a sigil reads and writes where its caller acts.
	servicesManager.SetCallStores(s.storeOfCall)

	// A plugin whose record names a namespace reads and writes there (ADR-046).
	servicesManager.SetPluginStores(s.storeOfPlugin)

	// "a schedule remembers who created it and where", and each run goes there.
	servicesManager.SetCallers(s.callerOf)
	servicesManager.SetOpenRun(s.openRun)

	// Mail to a User on a plugin's behalf (ADR-041): the Users exist once auth
	// has run, and the node's DID once nodedid has.
	servicesManager.SetMail(s.mailWiring())
	// The node's own mail goes by the same service (ADR-042), which Start made
	// whether or not its server came to serve plugins.
	s.nodeMailer = servicesManager.MailServer()

	s.servicesManager = servicesManager
	s.services = services

	// Wire services manager to plugin manager for LLM provider re-registration
	// after restart. A node running without a plugin manager has nothing to
	// wire, and says why to whoever asks it for one.
	switch managing := s.pluginManager.(type) {
	case managerRuns:
		managing.manager.SetServicesManager(servicesManager)
	case noPluginManager:
	}

	// Log plugin registry state — plugins load asynchronously, so the manager
	// is typically nil here. This captures the registry state in the structured log.
	states := pluginRegistry.GetAllStates()
	for name, state := range states {
		if errMsg, failed := pluginRegistry.GetError(name); failed {
			s.logger.Debugw("Plugin state at server startup",
				"plugin", name, "state", state, "error", errMsg)
			continue
		}
		s.logger.Debugw("Plugin state at server startup", "plugin", name, "state", state)
	}

	return nil
}

// pluginManaging is how the node runs plugins: with its plugin manager, or
// with none, and why.
type pluginManaging interface {
	managesPlugins()
}

// managerRuns is the plugin manager the node runs plugins with.
type managerRuns struct {
	manager *grpcplugin.PluginManager
}

// noPluginManager is a node running no plugins, and why.
type noPluginManager struct {
	why error
}

func (managerRuns) managesPlugins()     {}
func (noPluginManager) managesPlugins() {}

var (
	handedPluginManagerMu sync.RWMutex
	handedPluginManager   pluginManaging = noPluginManager{why: errors.New("no plugin manager was handed to the node")}
)

// HandPluginManager hands the node the plugin manager it runs plugins with.
func HandPluginManager(manager *grpcplugin.PluginManager) {
	handedPluginManagerMu.Lock()
	defer handedPluginManagerMu.Unlock()
	handedPluginManager = managerRuns{manager: manager}
}

// RunWithoutPluginManager says the node runs no plugins, and why: every
// request for one is answered with it.
func RunWithoutPluginManager(why error) {
	handedPluginManagerMu.Lock()
	defer handedPluginManagerMu.Unlock()
	handedPluginManager = noPluginManager{why: why}
}

// handedManaging is the plugin manager the node was handed, or why it has none.
func handedManaging() pluginManaging {
	handedPluginManagerMu.RLock()
	defer handedPluginManagerMu.RUnlock()
	return handedPluginManager
}

// getPluginManager returns the plugin manager, or why the node runs without one.
func (s *QNTXServer) getPluginManager() (*grpcplugin.PluginManager, error) {
	switch managing := s.pluginManager.(type) {
	case managerRuns:
		return managing.manager, nil
	case noPluginManager:
		return nil, managing.why
	}
	return nil, errors.Newf("the node says neither the plugin manager it runs plugins with, nor why it runs none: %T", s.pluginManager)
}
