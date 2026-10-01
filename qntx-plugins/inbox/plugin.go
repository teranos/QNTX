// Package qntxinbox is a User's own mail (ADR-047).
package qntxinbox

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/QNTX/plugin"
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// PredicateMailAddress: as <address> is mail:address of <User>.
const PredicateMailAddress = "mail:address"

// attester is the store one call's token reaches.
type attester interface {
	GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error)
}

// Plugin is the inbox plugin.
type Plugin struct {
	plugin.Base
	protocol.UnimplementedInboxServiceServer
	storeEndpoint string
	dial          func(ctx context.Context, endpoint, token string) (attester, func() error, error)
}

// NewPlugin creates the inbox plugin.
func NewPlugin() *Plugin {
	p := &Plugin{
		Base: plugin.NewBase(plugin.Metadata{
			Name:        "inbox",
			Version:     "0.1.0",
			QNTXVersion: ">= 0.1.0",
			Description: "A User's own mail (ADR-047)",
			Author:      "QNTX Contributors",
			License:     "MIT",
		}),
	}
	p.dial = p.dialStore
	return p
}

func (p *Plugin) dialStore(ctx context.Context, endpoint, token string) (attester, func() error, error) {
	store, err := plugingrpc.NewRemoteATSStore(ctx, endpoint, token, p.Services().Logger("inbox"))
	if err != nil {
		return nil, nil, errors.Wrapf(err, "the store at %s could not be reached", endpoint)
	}
	return store, store.Close, nil
}

// Initialize keeps where the store is.
func (p *Plugin) Initialize(_ context.Context, services plugin.ServiceRegistry) error {
	p.Init(services)
	p.storeEndpoint = services.Config("inbox").GetString("_ats_store_endpoint")
	if p.storeEndpoint == "" {
		return errors.New("the node named no ATS store endpoint, so no address can be written")
	}
	return nil
}

// DeclaredRoutes are the routes QNTX makes sigils and MCP tools.
func (p *Plugin) DeclaredRoutes() []*protocol.RouteInfo {
	return []*protocol.RouteInfo{{
		Method:      http.MethodPost,
		Path:        "/identity",
		Description: "Give a User an address: as <email> is mail:address of <user_id>. Takes user_id and email.",
	}}
}

// RegisterHTTP answers the declared routes.
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
	mux.HandleFunc("POST /identity", p.identity)
	return nil
}

// call is what the node handed for one sigil call.
type call struct {
	token     string
	asker     string
	namespace string
}

type callKey struct{}

// refused is a no, with the status it is said with.
type refused struct {
	status int
	says   string
}

func (r *refused) Error() string { return r.says }

// CreateMailIdentity writes as <email> is mail:address of <user_id>, with the
// call's token and the asker the node named as actor.
func (p *Plugin) CreateMailIdentity(ctx context.Context, req *protocol.CreateMailIdentityRequest) (_ *protocol.CreateMailIdentityResponse, err error) {
	c, handed := ctx.Value(callKey{}).(call)
	switch {
	case !handed || c.token == "":
		return nil, &refused{http.StatusForbidden, "an address is given only through the node's sigil: no call token was handed"}
	case c.asker == "":
		return nil, &refused{http.StatusForbidden, "the node named no asker, so nobody would have given this address"}
	case c.namespace == "" || c.namespace == auth.NamespaceSystem || c.namespace == auth.NamespaceDefault:
		return nil, &refused{http.StatusForbidden, "mail is never in " + quoted(c.namespace)}
	case req.GetUserId() == "":
		return nil, &refused{http.StatusBadRequest, "user_id is required: an address is a User's"}
	case !strings.Contains(req.GetEmail(), "@") || strings.HasPrefix(req.GetEmail(), "@") || strings.HasSuffix(req.GetEmail(), "@"):
		return nil, &refused{http.StatusBadRequest, quoted(req.GetEmail()) + " is not an address"}
	}

	store, closeStore, err := p.dial(ctx, p.storeEndpoint, c.token)
	if err != nil {
		return nil, err
	}
	defer func() { err = sqlclose.With(err, closeStore(), "the connection to the store at "+p.storeEndpoint) }()

	as, err := store.GenerateAndCreateAttestation(ctx, &types.AsCommand{
		Subjects:   []string{req.GetEmail()},
		Predicates: []string{PredicateMailAddress},
		Contexts:   []string{req.GetUserId()},
		Actors:     []string{c.asker},
		Source:     p.Metadata().Name,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "%s is not attested as mail:address of %s", req.GetEmail(), req.GetUserId())
	}
	return &protocol.CreateMailIdentityResponse{Created: &protocol.MailIdentity{Id: as.ID, Email: req.GetEmail()}}, nil
}

func quoted(s string) string { return `"` + s + `"` }

func (p *Plugin) identity(w http.ResponseWriter, r *http.Request) {
	var req protocol.CreateMailIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "the body is not a JSON object of user_id and email: "+err.Error(), http.StatusBadRequest)
		return
	}
	ctx := context.WithValue(r.Context(), callKey{}, call{
		token:     r.Header.Get("X-Qntx-Store-Token"),
		asker:     r.Header.Get("X-Qntx-Asker"),
		namespace: r.Header.Get("X-Qntx-Namespace"),
	})
	resp, err := p.CreateMailIdentity(ctx, &req)
	if err != nil {
		var no *refused
		if errors.As(err, &no) {
			http.Error(w, no.says, no.status)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "the address was attested and the answer did not marshal: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body); err != nil && p.Services() != nil {
		p.Services().Logger("inbox").Errorw("the address was attested and the answer did not reach the node", "address", req.GetEmail(), "user", req.GetUserId(), "error", err)
	}
}

var _ plugin.DomainPlugin = (*Plugin)(nil)
