package server

import (
	"path/filepath"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/plugin"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
)

type pluginServicesSubsystem struct{}

func (pluginServicesSubsystem) Name() string { return "plugin-services" }

func (pluginServicesSubsystem) Init(s *QNTXServer) error {
	pluginRegistry := s.pluginRegistry
	s.pluginHandler = NewPluginHandler(pluginRegistry, s.logger, s.pluginHealth)
	s.pluginHandler.sigils = s.pluginSigilRows
	s.pluginHandler.records = s.pluginRecords().Plugins
	grpcplugin.SetPluginRecords(s.pluginRecords())
	s.statusLineHandler = NewStatusLineHandler(pluginRegistry, s.logger, s.pluginHealth,
		// Fetched per request: the backend supplies the watcher store after
		// this handler is built.
		func() storage.Watchers { return s.held.ServedUniverse().Watchers() },
		func() *handlerFailureLog { return s.handlerFailures },
		s)
	s.statusLineHandler.news = func() *newsLog { return s.news }

	queue := s.daemon.GetQueue()

	// Start gRPC services for plugins (Issue #138)
	// These services allow plugins to call back to QNTX core
	// The node DID subsystem runs first and is fatal, so the node has its DID here.
	servicesManager := grpcplugin.NewServicesManager(s.deps.cfg.LLM, s.deps.cfg.Fetch, s.nodeDID.DID, s.logger)
	filesDir := filepath.Join(filepath.Dir(s.dbPath), "files")

	endpoints, err := servicesManager.Start(s.ctx, s.held.Served(), queue, s.scheduleStore, filesDir, s.deps.cfg.GroundDBPath)
	if err != nil {
		s.logger.Errorw("Plugin services did not start; every plugin runs without ATS, queue or schedule access", "error", err)
		endpoints = nil
	} else {
		s.logger.Debugw("Plugin services started",
			"ats_store", endpoints.ATSStoreAddress,
			"queue", endpoints.QueueAddress,
			"schedule", endpoints.ScheduleAddress,
			"file_service", endpoints.FileServiceAddress,
			"llm", endpoints.LLMAddress,
			"embedding", endpoints.EmbeddingAddress,
			"search", endpoints.SearchAddress,
			"mail", endpoints.MailAddress,
		)
	}

	// Wrap config provider to inject service endpoints for plugins. A plugin
	// whose record names a namespace is handed a token of its own for it.
	configProvider := grpcplugin.NewConfigProvider(endpoints, s.pluginToken, s.logger)
	services := plugin.NewServiceRegistry(pluginRegistry, s.nodeDB, s.logger, s.held.Served(), configProvider, queue)

	// Wire version resolver: ATSStore and FetchService auto-stamp source_version
	// from the plugin registry, so individual plugins don't need to set it.
	servicesManager.SetVersionResolver(func(source string) string {
		if p, ok := pluginRegistry.Get(source); ok {
			return p.Metadata().Version
		}
		return ""
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

	s.servicesManager = servicesManager
	s.services = services

	// Wire services manager to plugin manager for LLM provider re-registration after restart.
	if s.pluginManager != nil {
		s.pluginManager.SetServicesManager(servicesManager)
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
