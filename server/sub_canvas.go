package server

import (
	"github.com/teranos/QNTX/glyph/handlers"
	appcfg "github.com/teranos/QNTX/internal/config"
)

type canvasSubsystem struct{}

func (canvasSubsystem) Name() string { return "canvas" }

func (canvasSubsystem) Init(s *QNTXServer) error {
	// A canvas lives in one namespace and only that one (ADR-026).
	canvasStore := s.held.ServedUniverse().Canvas()
	var canvasOpts []handlers.CanvasHandlerOption
	if s.watcherEngine != nil {
		canvasOpts = append(canvasOpts, handlers.WithWatcherEngine(s.watcherEngine, s.logger))
	}
	serverPort := appcfg.DefaultServerPort
	if s.deps.cfg.Server.Port != nil {
		serverPort = *s.deps.cfg.Server.Port
	}
	canvasOpts = append(canvasOpts, handlers.WithServerPort(serverPort))
	s.canvasHandler = handlers.NewCanvasHandler(canvasStore, canvasOpts...)
	s.conversationAssembler = NewConversationAssembler(canvasStore, s.held.ServedUniverse().Queries())
	s.logger.Debugw("Canvas state handlers initialized")
	return nil
}
