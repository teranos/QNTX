package server

import (
	"context"
	"net/http"

	"github.com/teranos/QNTX/element/handlers"
	elementstorage "github.com/teranos/QNTX/element/storage"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

type canvasSubsystem struct{}

func (canvasSubsystem) Name() string { return "canvas" }

func (canvasSubsystem) Init(s *QNTXServer) error {
	// A canvas lives in one namespace and only that one (ADR-026).
	canvasStore := s.held.ServedUniverse().Canvas()

	// "default namespace has default canvas, whihc is the current canvas i am working with."
	if _, err := canvasStore.Name(context.Background()); errors.Is(err, elementstorage.ErrNoCanvas) {
		if err := canvasStore.Create(context.Background(), "default"); err != nil {
			return errors.Wrap(err, "default could not be given its canvas")
		}
	} else if err != nil {
		return errors.Wrap(err, "default's canvas could not be read")
	}

	var canvasOpts []handlers.CanvasHandlerOption
	if s.watcherEngine != nil {
		canvasOpts = append(canvasOpts, handlers.WithWatcherEngine(s.watcherEngine, s.logger))
	}
	serverPort := appcfg.DefaultServerPort
	if s.deps.cfg.Server.Port != nil {
		serverPort = *s.deps.cfg.Server.Port
	}
	canvasOpts = append(canvasOpts, handlers.WithServerPort(serverPort))
	canvasOpts = append(canvasOpts, handlers.WithCanvasFor(s.canvasFor))
	s.canvasHandler = handlers.NewCanvasHandler(canvasStore, canvasOpts...)
	s.conversationAssembler = NewConversationAssembler(canvasStore, s.held.ServedUniverse().Queries())
	s.logger.Debugw("Canvas state handlers initialized")
	return nil
}

// canvasFor is the canvas of the namespace a request acts in.
func (s *QNTXServer) canvasFor(r *http.Request) (*elementstorage.CanvasStore, error) {
	admitted, gated := auth.AdmissionFrom(r.Context())
	u, err := s.universeFor(admitted, gated)
	if err != nil {
		return nil, err
	}
	if u.Canvas() == nil {
		return nil, errors.Wrapf(elementstorage.ErrNoCanvas, "%s", u.Name())
	}
	return u.Canvas(), nil
}
