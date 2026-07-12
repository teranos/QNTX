package server

import (
	"path/filepath"

	"github.com/teranos/QNTX/plugin"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
)

type pluginServicesSubsystem struct{}

func (pluginServicesSubsystem) Name() string { return "plugin-services" }

func (pluginServicesSubsystem) Init(s *QNTXServer) error {
	pluginRegistry := plugin.GetDefaultRegistry()
	if pluginRegistry == nil {
		return nil
	}
	s.pluginRegistry = pluginRegistry
	s.pluginHandler = NewPluginHandler(pluginRegistry, s.logger)

	queue := s.daemon.GetQueue()

	// Start gRPC services for plugins (Issue #138)
	// These services allow plugins to call back to QNTX core
	servicesManager := grpcplugin.NewServicesManager(s.deps.cfg.LLM, s.deps.cfg.Fetch, s.logger)
	filesDir := filepath.Join(filepath.Dir(s.dbPath), "files")

	endpoints, err := servicesManager.Start(s.ctx, s.atsStore, queue, s.scheduleStore, filesDir, s.deps.cfg.GroundDBPath)
	if err != nil {
		s.logger.Warnw("Failed to start plugin services, plugins will not have service access", "error", err)
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
		)
	}

	// Wrap config provider to inject service endpoints for plugins
	configProvider := grpcplugin.NewConfigProvider(endpoints)
	services := plugin.NewServiceRegistry(s.db, s.logger, s.atsStore, configProvider, queue)

	// Wire version resolver: ATSStore and FetchService auto-stamp source_version
	// from the plugin registry, so individual plugins don't need to set it.
	servicesManager.SetVersionResolver(func(source string) string {
		if p, ok := pluginRegistry.Get(source); ok {
			return p.Metadata().Version
		}
		return ""
	})

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
		errMsg, _ := pluginRegistry.GetError(name)
		s.logger.Debugw("Plugin state at server startup",
			"plugin", name, "state", state, "error", errMsg)
	}

	return nil
}
