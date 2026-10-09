package server

import (
	"context"
	"net/http"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

// Namespaces is the signum of the namespaces a node keeps (ADR-026, ADR-039):
// listing them, making one, switching one on or off, ending one, and emptying
// default.

// listNamespacesResponse names the count so an empty list and a backend that
// keeps none are visibly different answers.
type listNamespacesResponse struct {
	Namespaces []storage.Namespace `json:"namespaces"`
	Count      int                 `json:"count"`
}

func (s *QNTXServer) namespacesSignum() sigil.Signum {
	name := &protocol.Param{Name: "name", Required: true, Says: "The namespace, by its name."}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "namespaces",
			Description: "The namespaces the node keeps, who owns each and whether it is switched on: listed, made, switched off and on, ended, and default emptied.",
			Tags:        []string{"namespaces", "tenancy", "ownership"},
			Sigils: []*protocol.Sigil{
				{
					Name: "list",
					Does: "Every namespace the node keeps, with who owns it, whether it is switched on, and the kinds it holds.",
					Gives: []*protocol.Field{
						{Name: "namespaces", Says: "One row per namespace."},
						{Name: "count", Says: "How many there are, so none and a backend that keeps none are different answers."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/namespaces"},
				},
				{
					Name:  "create",
					Does:  "Make a namespace, switched on, owned by whoever asked.",
					Takes: []*protocol.Param{{Name: "name", Required: true, Says: "The new namespace's name: one path segment."}},
					Gives: []*protocol.Field{
						{Name: "name", Says: "The namespace made."},
						{Name: "definition", Says: "Its owner, that it is switched on, and when it was made."},
						{Name: "kinds", Says: "The kinds it holds: none yet."},
					},
					Http: &protocol.Endpoint{Method: http.MethodPost, Path: "/api/namespaces"},
				},
				{
					Name:   "disable",
					Does:   "Switch a namespace off. Not system or default, and not the one the caller stands in.",
					Takes:  []*protocol.Param{name},
					Answer: "protocol.NamespaceActedOn",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: "/api/namespaces/{name}/disable"},
				},
				{
					Name:   "enable",
					Does:   "Switch a namespace back on.",
					Takes:  []*protocol.Param{name},
					Answer: "protocol.NamespaceActedOn",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: "/api/namespaces/{name}/enable"},
				},
				{
					Name:   "delete",
					Does:   "End a switched-off namespace, draining what it held into default. ROOT's, standing in system.",
					Takes:  []*protocol.Param{name},
					Answer: "protocol.NamespaceActedOn",
					Http:   &protocol.Endpoint{Method: http.MethodDelete, Path: "/api/namespaces/{name}"},
				},
				{
					Name:   "nuke",
					Does:   "Empty default without ending it: the one place data leaves. Reached standing in system.",
					Answer: "protocol.NamespaceActedOn",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: "/api/namespaces/default/nuke"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"list":   s.namespacesList,
			"create": s.namespacesCreate,
			"disable": func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
				return s.namespacesSwitch(ctx, sent["name"], false)
			},
			"enable": func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
				return s.namespacesSwitch(ctx, sent["name"], true)
			},
			"delete": s.namespacesDelete,
			"nuke":   s.namespacesNuke,
		},
	}
}

func (s *QNTXServer) namespacesList(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	namespaces, refusal := s.superNamespaces(ctx)
	if refusal != nil {
		return nil, refusal
	}
	found, err := namespaces.List()
	if err != nil {
		s.logger.Errorw("failed to list namespaces", "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrap(err, "failed to list namespaces").Error()}
	}
	return listNamespacesResponse{Namespaces: found, Count: len(found)}, nil
}

func (s *QNTXServer) namespacesCreate(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	namespaces, refusal := s.superNamespaces(ctx)
	if refusal != nil {
		return nil, refusal
	}
	name := sent["name"]
	if name == "" {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "name", Says: "name is required"}
	}

	// An identity owns a namespace, so a request nobody was admitted for has
	// nobody to own what it would create.
	asked := askedBy(ctx)
	if asked == "" {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this request carries no identity, so there is nobody to own a namespace"}
	}

	// Who asked is the owner. It does not come from the request, because a
	// request naming its own owner names somebody else's.
	definition := storage.NamespaceDefinition{
		Owner:     asked,
		Enabled:   true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := namespaces.Create(name, definition); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "name", Says: err.Error()}
	}

	s.logger.Infow("namespace created", "namespace", name, "by", definition.Owner)
	return storage.Namespace{Name: name, Definition: &definition, Kinds: []string{}}, nil
}

// namespacesSwitch puts one in or out of service. The store refuses system and
// default, because a disabled system is a node that cannot read who anybody is.
func (s *QNTXServer) namespacesSwitch(ctx context.Context, name string, enabled bool) (any, *protocol.Refusal) {
	namespaces, refusal := s.superNamespaces(ctx)
	if refusal != nil {
		return nil, refusal
	}
	if refusal := s.notStandingIn(ctx, name); refusal != nil {
		return nil, refusal
	}
	if err := namespaces.SetEnabled(name, enabled); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "name", Says: err.Error()}
	}
	// The door held from before the switch would keep serving what was just
	// switched off. Dropped either way: re-enabling reopens it on the next call.
	s.held.Forget(name)
	s.logger.Infow("namespace switched", "namespace", name, "enabled", enabled, "by", askedBy(ctx))
	return &protocol.NamespaceActedOn{Name: name}, nil
}

// namespacesDelete ends one, draining what it held into default. The store
// refuses system, default, and a namespace still enabled.
func (s *QNTXServer) namespacesDelete(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	namespaces, refusal := s.superNamespaces(ctx)
	if refusal != nil {
		return nil, refusal
	}
	name := sent["name"]
	if name == "" {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "name", Says: "no namespace named"}
	}
	if refusal := s.notStandingIn(ctx, name); refusal != nil {
		return nil, refusal
	}
	// "consider it additive when i say that i want the same to apply for namespace deletion as well"
	// "that you need to stand in system for it and you also need to be root for it"
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated || !admitted.MayEndNamespaces() {
		return nil, &protocol.Refusal{Why: sigil.NotAllowed, Says: "ending a namespace is ROOT's"}
	}
	if standing := s.namespaceOf(admitted); standing != auth.NamespaceSystem {
		return nil, &protocol.Refusal{Why: sigil.NotAllowed,
			Says: "ending a namespace is reached from " + auth.NamespaceSystem + ", and you are standing in " + standing}
	}

	// The door closes first. Its last flush writes what is still buffered, so
	// the drain below carries those rows too rather than leaving a tick to
	// write them into a prefix that is no longer there.
	s.held.Forget(name)
	if err := namespaces.Delete(name); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "name", Says: err.Error()}
	}
	// "thats an issue, fix now"
	if err := s.held.Ended(name); err != nil {
		s.logger.Errorw("namespace ended, and its landing file was not removed", "namespace", name, "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed,
			Says: errors.Wrapf(err, "namespace %s ended, and its landing file was not removed", name).Error()}
	}
	s.logger.Infow("namespace deleted", "namespace", name, "by", askedBy(ctx))
	return &protocol.NamespaceActedOn{Name: name}, nil
}

// namespacesNuke empties default without ending it. Everything a delete drains
// lands in default, so it is the one namespace that would otherwise only grow,
// and emptying it is the one place data leaves.
//
// You stand in the node to empty the project, never in the thing you are
// emptying: refused standing anywhere but system. The open door is dropped so
// the next caller opens default and finds it empty.
func (s *QNTXServer) namespacesNuke(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	namespaces, refusal := s.superNamespaces(ctx)
	if refusal != nil {
		return nil, refusal
	}
	admitted, gated := auth.AdmissionFrom(ctx)
	if standing := s.namespaceOf(admitted); !gated || standing != auth.NamespaceSystem {
		return nil, &protocol.Refusal{Why: sigil.NotAllowed,
			Says: "nuking " + auth.NamespaceDefault + " is reached from " + auth.NamespaceSystem}
	}
	if err := namespaces.Nuke(); err != nil {
		s.logger.Errorw("default was not nuked", "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	// The open door holds a store whose files are gone. The next caller opens
	// default again and finds it empty, which is what it now is.
	s.held.Forget(auth.NamespaceDefault)
	s.logger.Infow("default nuked", "by", askedBy(ctx))
	return &protocol.NamespaceActedOn{Name: auth.NamespaceDefault}, nil
}

// notStandingIn refuses switching off or ending the namespace the caller
// stands in. The UI says so by refusing the right-click; this is what makes it
// true for a caller that never opened the UI.
func (s *QNTXServer) notStandingIn(ctx context.Context, name string) *protocol.Refusal {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return nil
	}
	if standing := s.namespaceOf(admitted); slug.Of(standing) == slug.Of(name) {
		return &protocol.Refusal{Why: sigil.NotAllowed, Param: "name",
			Says: "you are standing in " + standing + "; step somewhere else first"}
	}
	return nil
}

// superNamespaces answers both questions a namespace sigil has: does this
// backend keep namespaces at all, and was this call asked through a gate.
func (s *QNTXServer) superNamespaces(ctx context.Context) (storage.Namespaces, *protocol.Refusal) {
	known := s.held.Known()
	if known == nil {
		// Which store is running is the whole of the answer: nothing the caller
		// can send makes this work. See ADR-026 — the reference stays in the
		// source, where somebody can go and read it.
		said := "namespaces exist only on the parquet backend, and this node keeps every attestation in one namespace"
		if s.store != "" {
			said = "namespaces exist only on the parquet backend; this node runs the " + s.store +
				" backend, which keeps every attestation in one namespace"
		}
		return nil, &protocol.Refusal{Why: sigil.NotFound, Says: said}
	}

	// Which levels reach this is server/reach's. No caller means the sigil was
	// asked outside the gate, which is a wiring mistake rather than a refusal.
	if _, ok := auth.AdmissionFrom(ctx); !ok {
		return nil, &protocol.Refusal{Why: sigil.NotAllowed, Says: "refused"}
	}
	return known, nil
}

// askedBy is the identity a call was admitted as, or empty when it was asked
// outside the gate.
func askedBy(ctx context.Context) string {
	if admitted, ok := auth.AdmissionFrom(ctx); ok {
		return admitted.Identity
	}
	return ""
}
