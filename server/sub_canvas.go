package server

import (
	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/glyph/handlers"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
)

type canvasSubsystem struct{}

func (canvasSubsystem) Name() string { return "canvas" }

func (canvasSubsystem) Init(s *QNTXServer) error {
	canvasStore := glyphstorage.NewCanvasStore(s.db)
	var canvasOpts []handlers.CanvasHandlerOption
	if s.watcherEngine != nil {
		canvasOpts = append(canvasOpts, handlers.WithWatcherEngine(s.watcherEngine, s.logger))
	}
	serverPort := appcfg.DefaultServerPort
	if s.deps.config.Server.Port != nil {
		serverPort = *s.deps.config.Server.Port
	}
	canvasOpts = append(canvasOpts, handlers.WithServerPort(serverPort))
	s.canvasHandler = handlers.NewCanvasHandler(canvasStore, canvasOpts...)
	s.conversationAssembler = NewConversationAssembler(canvasStore, storage.NewSQLQueryStore(s.db))
	s.logger.Debugw("Canvas state handlers initialized")
	return nil
}
