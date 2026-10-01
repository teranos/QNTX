// Package qntxinbox is a User's own mail (ADR-047).
package qntxinbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin"
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// PredicateMailAddress: as <address> is mail:address of <User>.
const PredicateMailAddress = "mail:address"

// "[At least 7 characters]@domain.tld"
const MinLocalPart = 7

// store is the attestation store a token reaches.
type store interface {
	GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error)
	GetAttestations(filters ats.AttestationFilter) ([]*types.As, error)
}

// Plugin is the inbox plugin.
type Plugin struct {
	plugin.Base
	protocol.UnimplementedInboxServiceServer
	namespace    string
	receiveEvery int32
	own          func() store
	bag          bag
	sender       sender
	rule         receiptRule
}

// NewPlugin creates the inbox plugin.
func NewPlugin() *Plugin {
	p := &Plugin{
		Base: plugin.NewBase(plugin.Metadata{
			Name:        "inbox",
			Version:     "0.2.0",
			QNTXVersion: ">= 0.1.0",
			Description: "A User's own mail (ADR-047)",
			Author:      "QNTX Contributors",
			License:     "MIT",
		}),
		receiveEvery: 30,
	}
	p.own = func() store { return p.Services().ATSStore() }
	return p
}

func (p *Plugin) log() *zap.SugaredLogger {
	if p.Services() == nil {
		return zap.NewNop().Sugar()
	}
	return p.Services().Logger("inbox")
}

// Initialize keeps the namespace inbox stands in, and the bucket, receipt rule
// and region the record names.
func (p *Plugin) Initialize(_ context.Context, services plugin.ServiceRegistry) error {
	p.Init(services)
	config := services.Config("inbox")
	p.namespace = strings.TrimSpace(config.GetString(plugingrpc.PluginNamespaceKey))
	mail := awsMail{
		region:  config.GetString("region"),
		bucket:  config.GetString("bucket"),
		ruleSet: config.GetString("rule_set"),
		rule:    config.GetString("rule"),
	}
	if every := config.GetInt("receive_every"); every > 0 {
		p.receiveEvery = int32(every)
	}
	p.bag, p.sender, p.rule = mail, mail, mail
	for _, key := range []string{"region", "bucket", "rule_set", "rule"} {
		if config.GetString(key) == "" {
			no := unset{errors.Newf("the record names no %s, so no mail is received or sent and no address is received for", key)}
			p.bag, p.sender, p.rule = no, no, no
			p.log().Warnw("inbox has no mail until its record names it", "missing", key)
			break
		}
	}
	return nil
}

// ConfigSchema is what inbox's record may hold.
func (p *Plugin) ConfigSchema() map[string]plugin.ConfigField {
	return map[string]plugin.ConfigField{
		"region":        {Type: "string", Description: "AWS region of SES and the bucket", Required: true},
		"bucket":        {Type: "string", Description: "S3 bucket SES stores received mail in, under inbound/", Required: true},
		"rule_set":      {Type: "string", Description: "SES receipt rule set", Required: true},
		"rule":          {Type: "string", Description: "SES receipt rule whose recipients are the granted addresses", Required: true},
		"receive_every": {Type: "int", Description: "Seconds between receivings", DefaultValue: "30"},
	}
}

// unset is mail whose record does not name it yet.
type unset struct{ err error }

func (u unset) List(context.Context, string) ([]string, error) { return nil, u.err }
func (u unset) Get(context.Context, string) ([]byte, error)    { return nil, u.err }
func (u unset) Move(context.Context, string, string) error     { return u.err }
func (u unset) Delete(context.Context, string) error           { return u.err }
func (u unset) Send(context.Context, outgoing) (string, error) { return "", u.err }
func (u unset) Add(context.Context, string) error              { return u.err }

// DeclaredRoutes are the routes QNTX makes sigils and MCP tools.
func (p *Plugin) DeclaredRoutes() []*protocol.RouteInfo {
	return []*protocol.RouteInfo{
		{
			Method:      http.MethodPost,
			Path:        "/identity",
			Description: "Give a User an address: as <email> is mail:address of <user_id>. Takes user_id and email.",
		},
		{
			Method:      http.MethodPost,
			Path:        "/send",
			Description: "Send a text mail from an address the caller holds. Takes from, to, subject and text_body.",
		},
		{
			Method:      http.MethodGet,
			Path:        "/mailbox",
			Description: "The mail of an address the caller holds, newest first. Takes address and mailbox (inbox, junk or sent). ROOT reading another User's mail is attested.",
		},
		{
			Method:      http.MethodGet,
			Path:        "/addresses",
			Description: "The addresses the caller holds; for ROOT and SUPER, every address and the User holding it.",
		},
	}
}

// RegisterHTTP answers the declared routes.
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
	mux.HandleFunc("POST /identity", p.identity)
	mux.HandleFunc("POST /send", p.send)
	mux.HandleFunc("GET /mailbox", p.mailbox)
	mux.HandleFunc("GET /addresses", p.addressesOf)
	return nil
}

// call is what the node handed for one sigil call.
type call struct {
	token     string
	asker     string
	user      string
	level     string
	namespace string
}

type callKey struct{}

func callOf(r *http.Request) context.Context {
	return context.WithValue(r.Context(), callKey{}, call{
		token:     r.Header.Get("X-Qntx-Store-Token"),
		asker:     r.Header.Get("X-Qntx-Asker"),
		user:      r.Header.Get("X-Qntx-Asker-User"),
		level:     r.Header.Get("X-Qntx-Asker-Level"),
		namespace: r.Header.Get("X-Qntx-Namespace"),
	})
}

// asking is a User's call, by the User and level the node names. A User's own
// token may reach no store, so inbox reads and writes mail through its own,
// in the namespace its record names, and decides whose mail it is itself.
func (p *Plugin) asking(ctx context.Context) (call, error) {
	c, ok := ctx.Value(callKey{}).(call)
	switch {
	case p.namespace == "" || p.namespace == auth.NamespaceSystem || p.namespace == auth.NamespaceDefault:
		return c, &refused{http.StatusForbidden, "mail is never in " + quoted(p.namespace)}
	case !ok || c.asker == "" || c.user == "":
		return c, &refused{http.StatusForbidden, "mail is reached only through the node, by a User it names"}
	}
	return c, nil
}

func (c call) root() bool { return c.level == string(auth.LevelRoot) }

func (c call) rootOrSuper() bool { return c.root() || c.level == string(auth.LevelSuper) }

// refused is a no, with the status it is said with.
type refused struct {
	status int
	says   string
}

func (r *refused) Error() string { return r.says }

// CreateMailIdentity writes as <email> is mail:address of <user_id>, with the
// asker the node named as actor, and has SES receive mail for the address.
func (p *Plugin) CreateMailIdentity(ctx context.Context, req *protocol.CreateMailIdentityRequest) (*protocol.CreateMailIdentityResponse, error) {
	c, err := p.asking(ctx)
	if err != nil {
		return nil, err
	}
	email := strings.ToLower(req.GetEmail())
	switch {
	// "they get it via ROOT approval"
	case !c.rootOrSuper():
		return nil, &refused{http.StatusForbidden, "an address is given by ROOT or SUPER, and the node names this caller " + c.level}
	case req.GetUserId() == "":
		return nil, &refused{http.StatusBadRequest, "user_id is required: an address is a User's"}
	case !strings.Contains(email, "@") || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@"):
		return nil, &refused{http.StatusBadRequest, quoted(req.GetEmail()) + " is not an address"}
	case utf8.RuneCountInString(email[:strings.LastIndex(email, "@")]) < MinLocalPart:
		return nil, &refused{http.StatusBadRequest, quoted(req.GetEmail()) + " has fewer than 7 characters before the @"}
	}

	as, err := p.own().GenerateAndCreateAttestation(ctx, &types.AsCommand{
		Subjects:   []string{email},
		Predicates: []string{PredicateMailAddress},
		Contexts:   []string{req.GetUserId()},
		Actors:     []string{c.asker},
		Source:     p.Metadata().Name,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "%s is not attested as mail:address of %s", email, req.GetUserId())
	}
	if err := p.rule.Add(ctx, email); err != nil {
		return nil, errors.Wrapf(err, "%s is attested as %s's (%s) and SES does not receive for it", email, req.GetUserId(), as.ID)
	}
	return &protocol.CreateMailIdentityResponse{Created: &protocol.MailIdentity{Id: as.ID, Email: email}}, nil
}

func quoted(s string) string { return `"` + s + `"` }

func (p *Plugin) identity(w http.ResponseWriter, r *http.Request) {
	var req protocol.CreateMailIdentityRequest
	if err := decode(r, &req); err != nil {
		http.Error(w, "the body is not a JSON object of user_id and email: "+err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := p.CreateMailIdentity(callOf(r), &req)
	p.answer(w, resp, err)
}

// decode reads a request body as the proto's JSON, by its field names.
func decode(r *http.Request, into proto.Message) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return protojson.Unmarshal(body, into)
}

func (p *Plugin) answer(w http.ResponseWriter, resp any, err error) {
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
		http.Error(w, "the answer did not marshal: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body); err != nil {
		p.log().Errorw("the answer did not reach the node", "error", err)
	}
}

var _ plugin.DomainPlugin = (*Plugin)(nil)
var _ plugin.ConfigurablePlugin = (*Plugin)(nil)
