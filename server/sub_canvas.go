package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"

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
		if err := canvasStore.Create(context.Background(), "default", ""); err != nil {
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
	if s.authHandler != nil {
		canvasOpts = append(canvasOpts, handlers.WithPeople(peopleOf{s.authHandler}))
	}
	if s.nodeMailer != nil {
		canvasOpts = append(canvasOpts, handlers.WithMailer(s.nodeMailer))
	}
	canvasOpts = append(canvasOpts, handlers.WithInviteLink(s.inviteLink))
	s.canvasHandler = handlers.NewCanvasHandler(canvasStore, canvasOpts...)
	s.conversationAssembler = NewConversationAssembler(canvasStore, s.held.ServedUniverse().Queries())
	s.logger.Debugw("Canvas state handlers initialized")
	return nil
}

// peopleOf names a canvas's owners and finds an invitee, from the Users the
// node keeps.
type peopleOf struct{ h *auth.Handler }

func (p peopleOf) Person(id string) (handlers.PersonView, bool) {
	u, found, err := p.h.UserByID(id)
	if err != nil || !found {
		return handlers.PersonView{}, false
	}
	return handlers.PersonView{ID: u.ID, Name: u.Name(), Picture: u.Picture()}, true
}

func (p peopleOf) PersonByEmail(email string) (handlers.PersonView, bool) {
	held, err := p.h.Users()
	if err != nil {
		return handlers.PersonView{}, false
	}
	for _, u := range held {
		for _, address := range u.EmailAddresses {
			if strings.EqualFold(address, email) {
				return handlers.PersonView{ID: u.ID, Name: u.Name(), Picture: u.Picture()}, true
			}
		}
	}
	return handlers.PersonView{}, false
}

// inviteLink is where an invitee accepts: the page, which is the first
// rp_origin, with the token on it. A node with none is reached on its own
// port.
func (s *QNTXServer) inviteLink(token string) string {
	page := ""
	if origins := s.deps.cfg.Auth.RPOrigins; len(origins) > 0 {
		page = strings.TrimSuffix(origins[0], "/")
	}
	return page + "/?canvas-invite=" + url.QueryEscape(token)
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
