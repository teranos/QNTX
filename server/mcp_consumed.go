package server

// 「[Opus 5.5] A namespace as a client of another system's MCP server (ADR-038).」
//
// "Think QNTX being a client of MCP in addition to just being the server."
//
// "So, I guess that means the Node itself, and each individual Namespace becomes a consumer of other systems MCP"
//
// "SO, I COULD HAVE THIS BE THE 1 POINT WHERE I SET UP ALL MY MCP NEEDS"
//
// 「[Opus 5.5] The OAuth is the SDK's (go-sdk auth). What is the node's: where the code comes back to,」
// 「[Opus 5.5] where the grant is kept, and what the node lets itself be pointed at.」

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/admission"
	"github.com/teranos/QNTX/internal/mcpclient"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"golang.org/x/oauth2"
)

// 「[Opus 5.5] consumedLineSubject is what a line about an MCP server the namespace consumes is about.」
const consumedLineSubject = "MCP"

// 「[Opus 5.5] consumedCallSubject is what a line about one call of a server's tool is about.」
const consumedCallSubject = "MCP_CALL"

// 「[Opus 5.5] consumedContext is what the lines about one server's calls are in, beside the namespace.」
func consumedContext(name string) string { return "mcp:" + name }

// 「[Opus 5.5] A line holds the server's URL and its grant sealed, or says the server was dropped.」
const (
	consumedURL     = "url"
	consumedSealed  = "sealed"
	consumedDropped = "dropped"
)

// 「[Opus 5.5] consumedPurpose is what a grant is sealed for, beside a namespace's keys and push credentials.」
const consumedPurpose = "mcp"

// 「[Opus 5.5] ConsumedCallbackPath is where the server's authorization sends the person back with the code.」
const ConsumedCallbackPath = "/auth/mcp/callback"

// 「[Opus 5.5] consumedFlowLife is how long a yes is waited on, as a ceremony is (auth.bindingFlowTTL).」
const consumedFlowLife = 10 * time.Minute

// 「[Opus 5.5] consumedWait is how long mcp connect waits to be told where to say yes, or that no yes was needed.」
var consumedWait = 30 * time.Second

// 「[Opus 5.5] consumedDialTimeout bounds reaching the server's address, and nothing after: its stream stays open.」
const consumedDialTimeout = 15 * time.Second

// 「[Opus 5.5] consumedAddress is whether the node reaches ip for an MCP server a person named: what the」
// 「[Opus 5.5] internet routes to (auth.IsPublicIP), so a URL is never the node reading its own network.」
var consumedAddress = auth.IsPublicIP

// 「[Opus 5.5] consumedClient reaches the MCP server and its authorization server, every address it dials」
// 「[Opus 5.5] asked again: DNS can answer differently at dial than when the URL was named.」
var consumedClient = func() *http.Client {
	dialer := &net.Dialer{Timeout: consumedDialTimeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return errors.Wrapf(err, "MCP server address %q is not host:port", address)
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return errors.Newf("MCP server address %q did not resolve to an IP", address)
		}
		if !consumedAddress(ip) {
			return errors.Newf("the MCP server resolves to %s, which is no public address the node reaches", ip)
		}
		return nil
	}}
	return &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext, ResponseHeaderTimeout: consumedDialTimeout}}
}

// 「[Opus 5.5] consumedGrant is what is sealed: the client the server's authorization knows the node by, and」
// 「[Opus 5.5] the token it issued. The client is kept because a refresh is asked as it.」
type consumedGrant struct {
	ClientID     string           `json:"client_id"`
	ClientSecret string           `json:"client_secret,omitempty"`
	AuthURL      string           `json:"auth_url"`
	TokenURL     string           `json:"token_url"`
	AuthStyle    oauth2.AuthStyle `json:"auth_style"`
	Scopes       []string         `json:"scopes,omitempty"`
	Token        *oauth2.Token    `json:"token"`
}

func (g consumedGrant) config(redirect string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: g.ClientID, ClientSecret: g.ClientSecret, RedirectURL: redirect, Scopes: g.Scopes,
		Endpoint: oauth2.Endpoint{AuthURL: g.AuthURL, TokenURL: g.TokenURL, AuthStyle: g.AuthStyle},
	}
}

// 「[Opus 5.5] consumedFlow is one yes being waited on: who asked for it, and where the code goes.」
type consumedFlow struct {
	by        string
	namespace string
	name      string
	code      chan consumedReturn
	attempt   *consumedAttempt
	startedAt time.Time
}

// 「[Opus 5.5] consumedReturn is what the server's authorization sent back: the code, or why there is none.」
type consumedReturn struct {
	result *mcpauth.AuthorizationResult
	err    error
}

// 「[Opus 5.5] consumedFlows is every yes being waited on, by the state the SDK put in its URL.」
type consumedFlows struct {
	pending sync.Map
}

// 「[Opus 5.5] consumedAttempt is one connecting: where to say yes once the server asks for it, and how it」
// 「[Opus 5.5] ended once it is over.」
type consumedAttempt struct {
	authorize chan string
	over      chan struct{}
	tools     []string
	err       error
}

// 「[Opus 5.5] consumedAsking is the namespace a call on the mcp signum names, when a person asks it. The yes」
// 「[Opus 5.5] at the server is a person's, and the code comes back to the browser of whoever asked, so a token」
// 「[Opus 5.5] and a connector are refused, as for keys.」
//
// "SO, I COULD HAVE THIS BE THE 1 POINT WHERE I SET UP ALL MY MCP NEEDS AS ROOT FOR THE NODE"
//
// 「[Opus 5.5] The node consumes in system, where it keeps its own lines, and only ROOT handles what it consumes.」
func (s *QNTXServer) consumedAsking(ctx context.Context, sent sigil.Sent) (anAsking, *protocol.Refusal) {
	if sent["namespace"] == auth.NamespaceSystem {
		admitted, gated := auth.AdmissionFrom(ctx)
		if !gated {
			return anAsking{}, &protocol.Refusal{Why: sigil.Failed, Says: "the MCP servers the node consumes are asked for through the gate, and this asking came through none"}
		}
		if refused := consumedByAPerson(admitted, "the node consumes"); refused != nil {
			return anAsking{}, refused
		}
		if !admitted.IsRoot() {
			return anAsking{}, &protocol.Refusal{Why: sigil.NotAllowed, Param: "namespace", Says: "the MCP servers the node consumes are ROOT's to handle"}
		}
		return anAsking{admitted: admitted, namespace: auth.NamespaceSystem}, nil
	}
	asked, refused := agentNamespaceOf(ctx, sent)
	return then(asked, refused, func(asked anAsking) (anAsking, *protocol.Refusal) {
		if refused := consumedByAPerson(asked.admitted, "a namespace consumes"); refused != nil {
			return anAsking{}, refused
		}
		return asked, s.mayHandle(asked.admitted, asked.namespace, "the MCP servers "+asked.namespace+" consumes are handled")
	})
}

// "Node Agent uses them (B)"
//
// 「[Opus 5.5] consumedUsing is the namespace a call that uses a server names, rather than handles one: listing」
// 「[Opus 5.5] them, their tools, and calling one. The node's are used by whoever stands at ROOT, its agents'」
// 「[Opus 5.5] tokens and ROOT's client included, since "as ROOT, everything ROOT reaches". A namespace's are」
// 「[Opus 5.5] handled and used alike.」
func (s *QNTXServer) consumedUsing(ctx context.Context, sent sigil.Sent) (anAsking, *protocol.Refusal) {
	if sent["namespace"] != auth.NamespaceSystem {
		return s.consumedAsking(ctx, sent)
	}
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return anAsking{}, &protocol.Refusal{Why: sigil.Failed, Says: "the MCP servers the node consumes are asked for through the gate, and this asking came through none"}
	}
	if !admitted.IsRoot() {
		return anAsking{}, &protocol.Refusal{Why: sigil.NotAllowed, Param: "namespace", Says: "the MCP servers the node consumes are ROOT's to use"}
	}
	return anAsking{admitted: admitted, namespace: auth.NamespaceSystem}, nil
}

// 「[Opus 5.5] consumedByAPerson refuses a token and a connector: the MCP servers whoever consumes are a person's to handle.」
func consumedByAPerson(admitted auth.Admission, whose string) *protocol.Refusal {
	switch admitted.Asker().(type) {
	case admission.TokenAsked:
		return &protocol.Refusal{Why: sigil.NotAllowed, Says: "the MCP servers " + whose + " are a person's to handle, and a token asked"}
	}
	if client, err := named.TextOf("the connector's client", admitted.ClientDID); err == nil {
		return &protocol.Refusal{Why: sigil.NotAllowed, Says: "the MCP servers " + whose + " are a person's to handle, and the connector " + client.String() + " asked"}
	}
	return nil
}

// 「[Opus 5.5] consumedSignum is the MCP servers a namespace consumes, at the namespace's path.」
func (s *QNTXServer) consumedSignum() sigil.Signum {
	at := "/api/mcp/{namespace}"
	namespace := &protocol.Param{Name: "namespace", Required: true, Says: "The namespace that consumes the server. system is the node, ROOT's alone."}
	name := &protocol.Param{Name: "name", Required: true, Says: "What the namespace calls the server, e.g. linear."}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "mcp",
			Description: "The MCP servers of other systems a namespace consumes, or the node in system: connected by the namespace's owner, ROOT or SUPER, and the node's by ROOT alone, saying yes at the server, the grant kept sealed and never answered back.",
			Tags:        []string{"mcp", "namespaces"},
			Sigils: []*protocol.Sigil{
				{
					Name:  "list",
					Does:  "The MCP servers the namespace consumes: each one's name, URL, who connected it and when. A grant is never answered.",
					Takes: []*protocol.Param{namespace},
					Gives: []*protocol.Field{
						{Name: "name", Says: "What the namespace calls the server."},
						{Name: "url", Says: "Where the server answers."},
						{Name: "by", Says: "Who connected it, or whose call last refreshed its token."},
						{Name: "at", Says: "When, as RFC 3339."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: at},
				},
				{
					Name: "connect",
					Does: "Connects the namespace to an MCP server at a URL. A server that asks for authorization answers with where to say yes; once the yes comes back, the grant is kept sealed under the name. A server that asks for none answers with its tools.",
					Takes: []*protocol.Param{namespace, name,
						{Name: "url", Required: true, Says: "Where the server answers, over https."}},
					Gives: []*protocol.Field{
						{Name: "name", Says: "What the namespace calls the server."},
						{Name: "url", Says: "Where the server answers."},
						{Name: "authorize", Says: "Where to say yes, in the browser logged in here. Empty when the server asked for none."},
						{Name: "tools", Says: "The names of the server's tools, when it answered without a yes. Empty while a yes is waited on."},
					},
					Http: &protocol.Endpoint{Method: http.MethodPost, Path: at},
				},
				{
					Name:  "tools",
					Does:  "The tools an MCP server the namespace consumes lists, asked of it on the grant kept for it. A token that expired is refreshed, and what the refresh issued is kept.",
					Takes: []*protocol.Param{namespace, name},
					Gives: []*protocol.Field{
						{Name: "name", Says: "The tool's name, as the server lists it."},
						{Name: "description", Says: "What the server says the tool does."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: at + "/tools"},
				},
				{
					Name: "call",
					Does: "Calls one tool of an MCP server the namespace consumes, on the grant kept for it, and writes the call down: who called which tool, with what, and whether it said it failed. The node's servers, in system, are called by whoever stands at ROOT, its agents included.",
					Takes: []*protocol.Param{namespace, name,
						{Name: "tool", Required: true, Says: "The tool, by the name mcp tools gives it."},
						{Name: "arguments", Says: "The tool's arguments, as a JSON object in text. Naming none sends none."}},
					Gives: []*protocol.Field{
						{Name: "content", Says: "What the tool answered, as the MCP server gave it: a list of content."},
						{Name: "is_error", Says: "Whether the tool said its own call failed."},
					},
					Http: &protocol.Endpoint{Method: http.MethodPost, Path: at + "/call"},
				},
				{
					Name:  "drop",
					Does:  "Drops an MCP server the namespace consumes, and the grant kept for it.",
					Takes: []*protocol.Param{namespace, name},
					Gives: []*protocol.Field{
						{Name: "name", Says: "What the namespace calls the server."},
						{Name: "url", Says: "Where the server answers."},
						{Name: "by", Says: "Who connected it, or whose call last refreshed its token."},
						{Name: "at", Says: "When, as RFC 3339."},
					},
					Http: &protocol.Endpoint{Method: http.MethodPost, Path: at + "/drop"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"list":    s.consumedList,
			"connect": s.consumedConnect,
			"tools":   s.consumedTools,
			"drop":    s.consumedDrop,
			"call":    s.consumedCall,
		},
	}
}

// 「[Opus 5.5] consumedServer is one server as list answers it.」
type consumedServer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	By   string `json:"by"`
	At   string `json:"at"`
}

// 「[Opus 5.5] consumedConnecting is what connect answers.」
type consumedConnecting struct {
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Authorize string   `json:"authorize"`
	Tools     []string `json:"tools"`
}

// 「[Opus 5.5] consumedTool is one tool as tools answers it.」
type consumedTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *QNTXServer) consumedList(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	asked, refused := s.consumedUsing(ctx, sent)
	return then(asked, refused, func(asked anAsking) (any, *protocol.Refusal) {
		return s.consumedOf(asked.namespace)
	})
}

func (s *QNTXServer) consumedDrop(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	asked, refused := s.consumedAsking(ctx, sent)
	return then(asked, refused, func(asked anAsking) (any, *protocol.Refusal) {
		name, err := named.TextOf("name", strings.TrimSpace(sent["name"]))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "name", Says: err.Error()}
		}
		if err := s.writeConsumedLine(asked.admitted, consumedLineSubject, asked.namespace, name.String(), map[string]any{consumedDropped: true}); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		s.logger.Infow("an MCP server a namespace consumes was dropped", "namespace", asked.namespace, "server", name.String(), "by", asked.admitted.Identity)
		return s.consumedOf(asked.namespace)
	})
}

// 「[Opus 5.5] consumedConnect starts connecting and waits until the server says where to say yes, or that」
// 「[Opus 5.5] it needed none, or consumedWait passes.」
func (s *QNTXServer) consumedConnect(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	asked, refused := s.consumedAsking(ctx, sent)
	return then(asked, refused, func(asked anAsking) (any, *protocol.Refusal) {
		name, err := named.TextOf("name", strings.TrimSpace(sent["name"]))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "name", Says: err.Error()}
		}
		at, refused := consumedURLOf(sent["url"])
		if refused != nil {
			return nil, refused
		}
		if s.authHandler == nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node serves no auth, so no authorization can come back to it"}
		}
		attempt := s.consume(asked, name.String(), at)
		answered := consumedConnecting{Name: name.String(), URL: at, Tools: []string{}}
		select {
		case answered.Authorize = <-attempt.authorize:
			return answered, nil
		case <-attempt.over:
			if attempt.err != nil {
				return nil, &protocol.Refusal{Why: sigil.Failed, Says: attempt.err.Error()}
			}
			answered.Tools = attempt.tools
			return answered, nil
		case <-time.After(consumedWait):
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: at + " neither asked for a yes nor answered within " + consumedWait.String()}
		}
	})
}

// 「[Opus 5.5] consumedURLOf is the server's URL, refused when it is not one the node goes to: https, with a host.」
func consumedURLOf(raw string) (string, *protocol.Refusal) {
	at, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || at.Host == "" {
		return "", &protocol.Refusal{Why: sigil.Missing, Param: "url", Says: "url is where the MCP server answers, and " + raw + " is no URL with a host"}
	}
	if at.Scheme != "https" {
		return "", &protocol.Refusal{Why: sigil.NotAllowed, Param: "url", Says: "the node reaches an MCP server over https, and " + at.String() + " is " + at.Scheme}
	}
	return at.String(), nil
}

// 「[Opus 5.5] consume connects to the server apart from the call that asked, since the yes outlives it: the」
// 「[Opus 5.5] SDK's authorization waits in the fetcher until the callback hands it the code.」
func (s *QNTXServer) consume(asked anAsking, name, at string) *consumedAttempt {
	attempt := &consumedAttempt{authorize: make(chan string, 1), over: make(chan struct{})}
	sacred.Go("connecting a namespace to an MCP server", func() {
		defer close(attempt.over)
		ctx, cancel := context.WithTimeout(context.Background(), consumedFlowLife)
		defer cancel()
		fetch := func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
			return s.awaitYes(ctx, attempt, asked, name, args.URL)
		}
		handler, err := s.consumedOAuth(asked.admitted, asked.namespace, name, at, nil, fetch)
		if err != nil {
			attempt.err = err
			return
		}
		attempt.tools, attempt.err = s.toolNamesAt(ctx, at, handler)
		if attempt.err == nil {
			s.logger.Infow("a namespace is connected to an MCP server", "namespace", asked.namespace, "server", name, "url", at, "tools", len(attempt.tools), "by", asked.admitted.Identity)
		}
	})
	return attempt
}

// 「[Opus 5.5] awaitYes hands the URL to say yes at to whoever asked, and waits for the callback's code.」
func (s *QNTXServer) awaitYes(ctx context.Context, attempt *consumedAttempt, asked anAsking, name, authorizeURL string) (*mcpauth.AuthorizationResult, error) {
	at, err := url.Parse(authorizeURL)
	if err != nil {
		return nil, errors.Wrapf(err, "the URL to say yes at for %s did not parse", name)
	}
	state := at.Query().Get("state")
	if state == "" {
		return nil, errors.Newf("the URL to say yes at for %s names no state", name)
	}
	flow := &consumedFlow{by: asked.admitted.Identity, namespace: asked.namespace, name: name,
		code: make(chan consumedReturn, 1), attempt: attempt, startedAt: time.Now()}
	s.consuming.pending.Store(state, flow)
	defer s.consuming.pending.Delete(state)
	select {
	case attempt.authorize <- authorizeURL:
	default:
		return nil, errors.Newf("%s asked for a second yes while connecting", name)
	}
	select {
	case back := <-flow.code:
		return back.result, back.err
	case <-ctx.Done():
		return nil, errors.Wrapf(ctx.Err(), "no yes came back for %s within %s", name, consumedFlowLife)
	}
}

// 「[Opus 5.5] consumedOAuth is the SDK's authorization code flow, as the node's: registering itself by」
// 「[Opus 5.5] dynamic client registration with its own callback, reaching out only through consumedClient,」
// 「[Opus 5.5] and keeping each token it is issued, sealed, in the namespace.」
func (s *QNTXServer) consumedOAuth(admitted auth.Admission, namespace, name, at string, kept *consumedGrant, fetch mcpauth.AuthorizationCodeFetcher) (*mcpauth.AuthorizationCodeHandler, error) {
	redirect := s.authHandler.PublicOrigin() + ConsumedCallbackPath
	keep := func(cfg *oauth2.Config, token *oauth2.Token) error {
		return s.keepGrant(admitted, namespace, name, at, consumedGrant{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, AuthURL: cfg.Endpoint.AuthURL, TokenURL: cfg.Endpoint.TokenURL,
			AuthStyle: cfg.Endpoint.AuthStyle, Scopes: cfg.Scopes, Token: token,
		})
	}
	reaching := consumedClient()
	config := &mcpauth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &mcpauth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
			ClientName:   "QNTX",
			RedirectURIs: []string{redirect},
			GrantTypes:   []string{"authorization_code", "refresh_token"},
		}},
		RedirectURL:              redirect,
		AuthorizationCodeFetcher: fetch,
		RequestRefreshToken:      true,
		Client:                   reaching,
		NewTokenSource: func(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token) (oauth2.TokenSource, error) {
			if err := keep(cfg, token); err != nil {
				return nil, err
			}
			return &keptSource{from: cfg.TokenSource(ctx, token), last: token.AccessToken, keep: func(token *oauth2.Token) error { return keep(cfg, token) }}, nil
		},
	}
	if kept != nil {
		cfg := kept.config(redirect)
		refreshing := context.WithValue(context.Background(), oauth2.HTTPClient, reaching)
		config.InitialTokenSource = &keptSource{from: cfg.TokenSource(refreshing, kept.Token), last: kept.Token.AccessToken,
			keep: func(token *oauth2.Token) error { return keep(cfg, token) }}
	}
	handler, err := mcpauth.NewAuthorizationCodeHandler(config)
	return handler, errors.Wrapf(err, "no authorization for %s at %s", name, at)
}

// 「[Opus 5.5] keptSource keeps every token its source hands out that it has not kept yet: a refresh rotates」
// 「[Opus 5.5] the refresh token, and one not kept is a grant the next start cannot use.」
type keptSource struct {
	from oauth2.TokenSource
	mu   sync.Mutex
	last string
	keep func(*oauth2.Token) error
}

func (k *keptSource) Token() (*oauth2.Token, error) {
	token, err := k.from.Token()
	if err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if token.AccessToken != k.last {
		if err := k.keep(token); err != nil {
			return nil, err
		}
		k.last = token.AccessToken
	}
	return token, nil
}

// 「[Opus 5.5] toolNamesAt opens the session with the server on handler, and is the names of its tools.」
func (s *QNTXServer) toolNamesAt(ctx context.Context, at string, handler mcpauth.OAuthHandler) ([]string, error) {
	tools, err := s.toolsAt(ctx, at, handler)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names, err
}

func (s *QNTXServer) toolsAt(ctx context.Context, at string, handler mcpauth.OAuthHandler) ([]consumedTool, error) {
	conn, err := mcpclient.Dial(ctx, mcpclient.Remote{URL: at, HTTPClient: consumedClient(), OAuth: handler},
		mcpclient.Client{Name: "qntx", Version: version.Get().Version})
	if err != nil {
		return nil, err
	}
	listed, err := conn.Tools(ctx)
	closed := conn.Close()
	if err != nil {
		return nil, errors.WithSecondaryError(err, closed)
	}
	tools := make([]consumedTool, 0, len(listed))
	for _, tool := range listed {
		tools = append(tools, consumedTool{Name: tool.Name, Description: tool.Description})
	}
	return tools, closed
}

// 「[Opus 5.5] consumedConn opens the session with the server on the grant kept for it. A grant the server no」
// 「[Opus 5.5] longer takes is not said yes to again here: that is mcp connect, in a browser.」
func (s *QNTXServer) consumedConn(ctx context.Context, asked anAsking, name string) (*mcpclient.Conn, *protocol.Refusal) {
	at, grant, err := s.grantOf(asked.namespace, name)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "name", Says: err.Error()}
	}
	if s.authHandler == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node serves no auth, so no authorization can come back to it"}
	}
	again := func(context.Context, *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
		return nil, errors.Newf("%s no longer takes the grant %s keeps for it: say yes again with mcp connect", at, asked.namespace)
	}
	handler, err := s.consumedOAuth(asked.admitted, asked.namespace, name, at, &grant, again)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	conn, err := mcpclient.Dial(ctx, mcpclient.Remote{URL: at, HTTPClient: consumedClient(), OAuth: handler},
		mcpclient.Client{Name: "qntx", Version: version.Get().Version})
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return conn, nil
}

func (s *QNTXServer) consumedTools(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	asked, refused := s.consumedUsing(ctx, sent)
	return then(asked, refused, func(asked anAsking) (any, *protocol.Refusal) {
		name, err := named.TextOf("name", strings.TrimSpace(sent["name"]))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "name", Says: err.Error()}
		}
		conn, refused := s.consumedConn(ctx, asked, name.String())
		if refused != nil {
			return nil, refused
		}
		listed, err := conn.Tools(ctx)
		if closed := conn.Close(); err != nil || closed != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: errors.WithSecondaryError(err, closed).Error()}
		}
		tools := make([]consumedTool, 0, len(listed))
		for _, tool := range listed {
			tools = append(tools, consumedTool{Name: tool.Name, Description: tool.Description})
		}
		return tools, nil
	})
}

// 「[Opus 5.5] consumedCalled is what call answers: the tool's result as the server gave it.」
type consumedCalled struct {
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"is_error"`
}

// 「[Opus 5.5] consumedCall calls one tool of the server on the grant kept for it, and writes the call down:」
// 「[Opus 5.5] who called which tool of which server, with what, and whether the tool said it failed.」
func (s *QNTXServer) consumedCall(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	asked, refused := s.consumedUsing(ctx, sent)
	return then(asked, refused, func(asked anAsking) (any, *protocol.Refusal) {
		name, err := named.TextOf("name", strings.TrimSpace(sent["name"]))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "name", Says: err.Error()}
		}
		tool, err := named.TextOf("tool", strings.TrimSpace(sent["tool"]))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "tool", Says: err.Error()}
		}
		arguments := map[string]any{}
		if raw := strings.TrimSpace(sent["arguments"]); raw != "" {
			if err := json.Unmarshal([]byte(raw), &arguments); err != nil {
				return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "arguments", Says: "arguments is the tool's arguments as a JSON object, and " + err.Error()}
			}
		}
		conn, refused := s.consumedConn(ctx, asked, name.String())
		if refused != nil {
			return nil, refused
		}
		result, err := conn.Call(ctx, tool.String(), arguments)
		if closed := conn.Close(); err != nil || closed != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: errors.WithSecondaryError(err, closed).Error()}
		}
		content, err := json.Marshal(result.Content)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "what %s of %s answered did not marshal", tool.String(), name.String()).Error()}
		}
		said, err := json.Marshal(arguments)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "the arguments to %s of %s did not marshal", tool.String(), name.String()).Error()}
		}
		if err := s.writeConsumedLine(asked.admitted, consumedCallSubject, asked.namespace, tool.String(),
			map[string]any{"server": name.String(), "arguments": string(said), "is_error": result.IsError}, consumedContext(name.String())); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		return consumedCalled{Content: content, IsError: result.IsError}, nil
	})
}

// 「[Opus 5.5] keepGrant writes the grant down for the server, sealed, as whoever's call it was issued on.」
func (s *QNTXServer) keepGrant(admitted auth.Admission, namespace, name, at string, grant consumedGrant) error {
	body, err := json.Marshal(grant)
	if err != nil {
		return errors.Wrapf(err, "the grant for %s of %s did not marshal", name, namespace)
	}
	sealed, err := s.sealFor(consumedPurpose, namespace, string(body))
	if err != nil {
		return err
	}
	return s.writeConsumedLine(admitted, consumedLineSubject, namespace, name, map[string]any{consumedURL: at, consumedSealed: sealed})
}

// 「[Opus 5.5] writeConsumedLine writes one line about a server the namespace consumes, as whoever's call it was.」
func (s *QNTXServer) writeConsumedLine(admitted auth.Admission, subject, namespace, name string, attributes map[string]any, contexts ...string) error {
	store, err := s.held.Write(admitted, namespace)
	if err != nil {
		return errors.Wrapf(err, "no store to write the %s line %s of %s in", subject, name, namespace)
	}
	id, err := identity.GenerateASUIDWithRetry("AS", subject, name, namespace, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line %s of %s", subject, name, namespace)
	}
	at := time.Now()
	return errors.Wrapf(store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{subject}, Predicates: []string{name}, Contexts: append([]string{namespace}, contexts...),
		Actors: []string{admitted.Identity}, Timestamp: at, CreatedAt: at, Source: "mcp",
		Attributes: attributes,
	}), "the %s line %s of %s was not written", subject, name, namespace)
}

// 「[Opus 5.5] newestConsumed is the newest line of the namespace for each server it names.」
func (s *QNTXServer) newestConsumed(namespace string) (map[string]*types.As, error) {
	reading, err := s.held.Read(namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines of %s", consumedLineSubject, namespace)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{consumedLineSubject}, Limit: ats.EveryRow})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines of %s", consumedLineSubject, namespace)
	}
	newest := map[string]*types.As{}
	for _, as := range found {
		for _, name := range as.Predicates {
			if held, ok := newest[name]; !ok || as.Timestamp.After(held.Timestamp) {
				newest[name] = as
			}
		}
	}
	return newest, nil
}

// 「[Opus 5.5] consumedOf is the servers the namespace consumes: each one whose newest line holds a grant.」
func (s *QNTXServer) consumedOf(namespace string) ([]consumedServer, *protocol.Refusal) {
	newest, err := s.newestConsumed(namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	servers := []consumedServer{}
	for name, as := range newest {
		if _, holds := as.Attributes[consumedSealed].(string); !holds {
			continue
		}
		at, _ := as.Attributes[consumedURL].(string)
		by, err := named.ItemsOf(consumedLineSubject+" line "+as.ID+" of "+namespace+", who connected it,", as.Actors)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		servers = append(servers, consumedServer{Name: name, URL: at, By: by.First(), At: as.Timestamp.UTC().Format(time.RFC3339)})
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	return servers, nil
}

// 「[Opus 5.5] grantOf is the server's URL and its grant, unsealed.」
func (s *QNTXServer) grantOf(namespace, name string) (string, consumedGrant, error) {
	newest, err := s.newestConsumed(namespace)
	if err != nil {
		return "", consumedGrant{}, err
	}
	as, set := newest[name]
	if !set {
		return "", consumedGrant{}, errors.Newf("%s consumes no MCP server %s: connect one with mcp connect", namespace, name)
	}
	sealed, holds := as.Attributes[consumedSealed].(string)
	if !holds {
		return "", consumedGrant{}, errors.Newf("the MCP server %s of %s was dropped: connect it again with mcp connect", name, namespace)
	}
	where, _ := as.Attributes[consumedURL].(string)
	at, err := named.TextOf(consumedLineSubject+" line "+as.ID+" of "+namespace+", its url,", where)
	if err != nil {
		return "", consumedGrant{}, err
	}
	opened, err := s.unsealFor(consumedPurpose, namespace, sealed)
	if err != nil {
		return "", consumedGrant{}, err
	}
	var grant consumedGrant
	if err := json.Unmarshal([]byte(opened), &grant); err != nil {
		return "", consumedGrant{}, errors.Wrapf(err, "the grant for %s of %s did not read", name, namespace)
	}
	if grant.Token == nil {
		return "", consumedGrant{}, errors.Newf("the grant for %s of %s holds no token", name, namespace)
	}
	return at.String(), grant, nil
}

// 「[Opus 5.5] handleConsumedCallback is where the server's authorization sends the person back. The code is」
// 「[Opus 5.5] handed only to the yes this same person asked for: "the browser finishing has to be the browser」
// 「[Opus 5.5] that started" (auth.handleBindingCallback), and here the person logged in is who asked.」
func (s *QNTXServer) handleConsumedCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.authHandler == nil {
		http.Error(w, "this node serves no auth, so no yes comes back to it", http.StatusNotFound)
		return
	}
	say := s.authHandler.RenderCeremonyPage
	q := r.URL.Query()
	held, open := s.consuming.pending.Load(q.Get("state"))
	if !open {
		say(w, http.StatusBadRequest, false, "This yes is unknown, already finished, or older than ten minutes")
		return
	}
	flow, isFlow := held.(*consumedFlow)
	if !isFlow {
		say(w, http.StatusInternalServerError, false, "What waits under this state is not a yes the node asked for")
		return
	}
	admitted, gated := auth.AdmissionFrom(r.Context())
	if !gated || admitted.Identity != flow.by {
		s.logger.Infow("an MCP server's yes refused: finished by somebody who did not ask for it", "namespace", flow.namespace, "server", flow.name)
		say(w, http.StatusForbidden, false, "This yes was asked for by somebody else. Connect from your own window, logged in here.")
		return
	}
	if time.Since(flow.startedAt) > consumedFlowLife {
		say(w, http.StatusBadRequest, false, "This yes is older than ten minutes: connect again")
		return
	}
	back := consumedReturn{result: &mcpauth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}}
	if refused := q.Get("error"); refused != "" {
		back = consumedReturn{err: errors.Newf("%s refused the yes: %s %s", flow.name, refused, q.Get("error_description"))}
	} else if back.result.Code == "" {
		back = consumedReturn{err: errors.Newf("%s sent the person back without a code", flow.name)}
	}
	select {
	case flow.code <- back:
	default:
		say(w, http.StatusConflict, false, "This yes already came back")
		return
	}
	select {
	case <-flow.attempt.over:
	case <-time.After(consumedWait):
		say(w, http.StatusAccepted, true, flow.namespace+" is still connecting to "+flow.name+": mcp list says when it is")
		return
	}
	if flow.attempt.err != nil {
		say(w, http.StatusBadGateway, false, flow.namespace+" is not connected to "+flow.name+": "+flow.attempt.err.Error())
		return
	}
	say(w, http.StatusOK, true, flow.namespace+" is connected to "+flow.name+", which lists "+strings.Join(flow.attempt.tools, ", "))
}
