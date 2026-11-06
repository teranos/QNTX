package server

// 「[Opus 5.5] The node as a client of another system's MCP server (ADR-038).」
//
// "Think QNTX being a client of MCP in addition to just being the server."
//
// "SO, I COULD HAVE THIS BE THE 1 POINT WHERE I SET UP ALL MY MCP NEEDS AS ROOT FOR THE NODE"
//
// "THE NODE'S MCP NEEDS"
//
// "The thing you built, can we have it root only"
//
// 「[Opus 5.5] The OAuth is the SDK's (go-sdk auth). What is the node's: where the code comes back to,」
// 「[Opus 5.5] where the grant is kept, and what the node lets itself be pointed at.」

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// 「[Opus 5.5] consumedLineSubject is what a line about an MCP server the node consumes is about.」
const consumedLineSubject = "MCP"

// 「[Opus 5.5] consumedIn is where the node keeps its lines about what it consumes: system, its own.」
const consumedIn = auth.NamespaceSystem

// 「[Opus 5.5] consumedCallSubject is what a line about one call of a server's tool is about.」
const consumedCallSubject = "MCP_CALL"

// 「[Opus 5.5] consumedContext is what the lines about one server's calls are in, beside system.」
func consumedContext(name string) string { return "mcp:" + name }

// 「[Opus 5.5] A line holds the server's URL and its grant sealed, or says the server was dropped.」
const (
	consumedURL     = "url"
	consumedSealed  = "sealed"
	consumedDropped = "dropped"
	// 「[Opus 5.5] consumedOpen is a server that asked for no authorization: it holds no grant, and is reached as it is.」
	consumedOpen = "open"
)

// 「[Opus 5.5] consumedName is what a server is named: its name is the MCP server's name in the node's agents,」
// 「[Opus 5.5] and the variable its bearer is read from.」
var consumedName = regexp.MustCompile(`^[a-z0-9_]+$`)

// 「[Opus 5.5] ConsumedServedAt is where the node serves each server it consumes to its agents, by name.」
const ConsumedServedAt = "/api/mcp/servers/"

// 「[Opus 5.5] consumedPurpose is what a grant is sealed for, beside keys and push credentials.」
const consumedPurpose = "mcp"

// 「[Opus 5.5] ConsumedCallbackPath is where the server's authorization sends the person back with the code.」
const ConsumedCallbackPath = "/auth/mcp/callback"

// 「[Opus 5.5] consumedFlowLife is how long a yes is waited on, as a ceremony is (auth.bindingFlowTTL).」
const consumedFlowLife = 10 * time.Minute

// 「[Opus 5.5] consumedWait is how long mcp connect waits to be told where to say yes, or that no yes was needed.」
var consumedWait = 30 * time.Second

// 「[Opus 5.5] consumedDialTimeout bounds reaching the server's address, and nothing after: its stream stays open.」

// 「[Opus 5.5] consumedCallLife bounds one call of the node's agents through to the server: listing its tools,」
// 「[Opus 5.5] or one tool's call. An upstream that stops answering does not hold the node past it.」
var consumedCallLife = 5 * time.Minute

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
	// 「[Opus 5.5] No response is waited on for a set time here: a tool's call answers when the tool is done.」
	// 「[Opus 5.5] consumedCallLife bounds the whole of it instead.」
	return &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: consumedRedirect}
}

// 「[Opus 5.5] consumedRedirect follows a redirect over https alone, so a request carrying the grant never」
// 「[Opus 5.5] goes in the clear.」
func consumedRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return errors.Newf("%s redirected to %s, and the node reaches an MCP server over https alone", via[len(via)-1].URL.Host, req.URL.Redacted())
	}
	if len(via) >= 10 {
		return errors.Newf("%s redirected ten times", via[0].URL.Host)
	}
	return nil
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
	// 「[Opus 5.5] sources is each server's token source, one for the node, so a refresh is asked once however」
	// 「[Opus 5.5] many calls find the token expired at once: a provider spends a refresh token on its first use.」
	sources sync.Map
}

// 「[Opus 5.5] consumedAttempt is one connecting: where to say yes once the server asks for it, and how it」
// 「[Opus 5.5] ended once it is over.」
type consumedAttempt struct {
	authorize chan string
	over      chan struct{}
	tools     []string
	err       error
}

// 「[Opus 5.5] consumedHandling is who connects or drops a server the node consumes: ROOT, as a person. The」
// 「[Opus 5.5] yes at the server is a person's, and the code comes back to the browser of whoever asked, so a」
// 「[Opus 5.5] token and a connector are refused, ROOT's included.」
func consumedHandling(ctx context.Context) (auth.Admission, *protocol.Refusal) {
	admitted, refused := consumedUsing(ctx)
	if refused != nil {
		return auth.Admission{}, refused
	}
	switch admitted.Asker().(type) {
	case admission.TokenAsked:
		return auth.Admission{}, &protocol.Refusal{Why: sigil.NotAllowed, Says: "the MCP servers the node consumes are connected by a person, and a token asked"}
	}
	if client, err := named.TextOf("the connector's client", admitted.ClientDID); err == nil {
		return auth.Admission{}, &protocol.Refusal{Why: sigil.NotAllowed, Says: "the MCP servers the node consumes are connected by a person, and the connector " + client.String() + " asked"}
	}
	return admitted, nil
}

// 「[Opus 5.5] consumedUsing is who uses a server the node consumes: whoever stands at ROOT, its agents'」
// 「[Opus 5.5] tokens included, "as ROOT, everything ROOT reaches".」
func consumedUsing(ctx context.Context) (auth.Admission, *protocol.Refusal) {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return auth.Admission{}, &protocol.Refusal{Why: sigil.Failed, Says: "the MCP servers the node consumes are asked for through the gate, and this asking came through none"}
	}
	if !admitted.IsRoot() {
		return auth.Admission{}, &protocol.Refusal{Why: sigil.NotAllowed, Says: "the MCP servers the node consumes are ROOT's"}
	}
	return admitted, nil
}

// 「[Opus 5.5] consumedSignum is the MCP servers the node consumes.」
func (s *QNTXServer) consumedSignum() sigil.Signum {
	at := "/api/mcp"
	name := &protocol.Param{Name: "name", Required: true, Says: "What the node calls the server, e.g. linear."}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "mcp",
			Description: "The MCP servers of other systems the node consumes: ROOT's alone, connected by ROOT saying yes at the server, the grant kept sealed and never answered back, and served to the node's agents as MCP servers of their own, through the node.",
			Tags:        []string{"mcp"},
			Sigils: []*protocol.Sigil{
				{
					Name: "list",
					Does: "The MCP servers the node consumes: each one's name, URL, who connected it and when. A grant is never answered.",
					Gives: []*protocol.Field{
						{Name: "name", Says: "What the node calls the server."},
						{Name: "url", Says: "Where the server answers."},
						{Name: "by", Says: "Who connected it, or whose call last refreshed its token."},
						{Name: "at", Says: "When, as RFC 3339."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: at},
				},
				{
					Name: "connect",
					Does: "Connects the node to an MCP server at a URL. A server that asks for authorization answers with where to say yes; once the yes comes back, the grant is kept sealed under the name. A server that asks for none answers with its tools.",
					Takes: []*protocol.Param{name,
						{Name: "url", Required: true, Says: "Where the server answers, over https."}},
					Gives: []*protocol.Field{
						{Name: "name", Says: "What the node calls the server."},
						{Name: "url", Says: "Where the server answers."},
						{Name: "authorize", Says: "Where to say yes, in the browser logged in here. Empty when the server asked for none."},
						{Name: "tools", Says: "The names of the server's tools, when it answered without a yes. Empty while a yes is waited on."},
					},
					Http: &protocol.Endpoint{Method: http.MethodPost, Path: at},
				},
				{
					Name:  "tools",
					Does:  "The tools an MCP server the node consumes lists, asked of it on the grant kept for it. A token that expired is refreshed, and what the refresh issued is kept.",
					Takes: []*protocol.Param{name},
					Gives: []*protocol.Field{
						{Name: "name", Says: "The tool's name, as the server lists it."},
						{Name: "description", Says: "What the server says the tool does."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: at + "/tools"},
				},
				{
					Name:  "drop",
					Does:  "Drops an MCP server the node consumes, and the grant kept for it.",
					Takes: []*protocol.Param{name},
					Gives: []*protocol.Field{
						{Name: "name", Says: "What the node calls the server."},
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

func (s *QNTXServer) consumedList(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	if _, refused := consumedUsing(ctx); refused != nil {
		return nil, refused
	}
	return s.consumedOf()
}

func (s *QNTXServer) consumedDrop(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	admitted, refused := consumedHandling(ctx)
	return then(admitted, refused, func(admitted auth.Admission) (any, *protocol.Refusal) {
		name, err := named.TextOf("name", strings.TrimSpace(sent["name"]))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "name", Says: err.Error()}
		}
		s.consuming.sources.Delete(name.String())
		if err := s.writeConsumedLine(admitted, consumedLineSubject, name.String(), map[string]any{consumedDropped: true}); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		s.logger.Infow("an MCP server the node consumes was dropped", "server", name.String(), "by", admitted.Identity)
		return s.consumedOf()
	})
}

// 「[Opus 5.5] consumedConnect starts connecting and waits until the server says where to say yes, or that」
// 「[Opus 5.5] it needed none, or consumedWait passes.」
func (s *QNTXServer) consumedConnect(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	admitted, refused := consumedHandling(ctx)
	return then(admitted, refused, func(admitted auth.Admission) (any, *protocol.Refusal) {
		name, err := named.TextOf("name", strings.TrimSpace(sent["name"]))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "name", Says: err.Error()}
		}
		if !consumedName.MatchString(name.String()) || name.String() == nodeAgentMCP {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "name", Says: "the node's agents are given the server under its name, so it is lowercase letters, digits and _, and not " + nodeAgentMCP + ", the node's own"}
		}
		at, refused := consumedURLOf(sent["url"])
		if refused != nil {
			return nil, refused
		}
		if s.authHandler == nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node serves no auth, so no authorization can come back to it"}
		}
		attempt := s.consume(admitted, name.String(), at)
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
func (s *QNTXServer) consume(admitted auth.Admission, name, at string) *consumedAttempt {
	attempt := &consumedAttempt{authorize: make(chan string, 1), over: make(chan struct{})}
	sacred.Go("connecting the node to an MCP server", func() {
		defer close(attempt.over)
		ctx, cancel := context.WithTimeout(context.Background(), consumedFlowLife)
		defer cancel()
		fetch := func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
			return s.awaitYes(ctx, attempt, admitted, name, args.URL)
		}
		handler, err := s.consumedOAuth(admitted, name, at, nil, fetch)
		if err != nil {
			attempt.err = err
			return
		}
		began := time.Now()
		attempt.tools, attempt.err = s.toolNamesAt(ctx, at, handler)
		if attempt.err == nil {
			attempt.err = s.keepOpenUnlessGranted(admitted, name, at, began)
		}
		if attempt.err == nil {
			s.logger.Infow("the node is connected to an MCP server", "server", name, "url", at, "tools", len(attempt.tools), "by", admitted.Identity)
		}
	})
	return attempt
}

// 「[Opus 5.5] keepOpenUnlessGranted writes the server down as open when it answered without asking for a yes:」
// 「[Opus 5.5] no grant was kept for it since began.」
func (s *QNTXServer) keepOpenUnlessGranted(admitted auth.Admission, name, at string, began time.Time) error {
	newest, err := s.newestConsumed()
	if err != nil {
		return err
	}
	if line, kept := newest[name]; kept && !line.Timestamp.Before(began) {
		return nil
	}
	s.consuming.sources.Delete(name)
	return s.writeConsumedLine(admitted, consumedLineSubject, name, map[string]any{consumedURL: at, consumedOpen: true})
}

// 「[Opus 5.5] awaitYes hands the URL to say yes at to whoever asked, and waits for the callback's code.」
func (s *QNTXServer) awaitYes(ctx context.Context, attempt *consumedAttempt, admitted auth.Admission, name, authorizeURL string) (*mcpauth.AuthorizationResult, error) {
	at, err := url.Parse(authorizeURL)
	if err != nil {
		return nil, errors.Wrapf(err, "the URL to say yes at for %s did not parse", name)
	}
	state := at.Query().Get("state")
	if state == "" {
		return nil, errors.Newf("the URL to say yes at for %s names no state", name)
	}
	flow := &consumedFlow{by: admitted.Identity, name: name,
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
// 「[Opus 5.5] and keeping each token it is issued, sealed, in system.」
func (s *QNTXServer) consumedOAuth(admitted auth.Admission, name, at string, kept *consumedGrant, fetch mcpauth.AuthorizationCodeFetcher) (*mcpauth.AuthorizationCodeHandler, error) {
	redirect := s.authHandler.PublicOrigin() + ConsumedCallbackPath
	keep := func(cfg *oauth2.Config, token *oauth2.Token) error {
		return s.keepGrant(admitted, name, at, consumedGrant{
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
			source := &keptSource{from: cfg.TokenSource(ctx, token), last: *token, keep: func(token *oauth2.Token) error { return keep(cfg, token) }}
			s.consuming.sources.Store(name, source)
			return source, nil
		},
	}
	if kept != nil {
		cfg := kept.config(redirect)
		refreshing := context.WithValue(context.Background(), oauth2.HTTPClient, reaching)
		held, _ := s.consuming.sources.LoadOrStore(name, &keptSource{from: cfg.TokenSource(refreshing, kept.Token), last: *kept.Token,
			keep: func(token *oauth2.Token) error { return keep(cfg, token) }})
		config.InitialTokenSource = held.(*keptSource)
	}
	handler, err := mcpauth.NewAuthorizationCodeHandler(config)
	return handler, errors.Wrapf(err, "no authorization for %s at %s", name, at)
}

// 「[Opus 5.5] keptSource keeps every token its source hands out that it has not kept yet: a refresh rotates」
// 「[Opus 5.5] the refresh token, and one not kept is a grant the next start cannot use.」
//
// 「[Opus 5.5] One is held per server for the node (consumedFlows.sources), so its refresh is asked once; a」
// 「[Opus 5.5] refresh is kept as whoever's call first made the source.」
type keptSource struct {
	from oauth2.TokenSource
	mu   sync.Mutex
	last oauth2.Token
	keep func(*oauth2.Token) error
}

func (k *keptSource) Token() (*oauth2.Token, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	token, err := k.from.Token()
	if err != nil {
		return nil, err
	}
	if token.AccessToken != k.last.AccessToken || token.RefreshToken != k.last.RefreshToken {
		if err := k.keep(token); err != nil {
			return nil, err
		}
		k.last = *token
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
func (s *QNTXServer) consumedConn(ctx context.Context, admitted auth.Admission, name string) (*mcpclient.Conn, *protocol.Refusal) {
	at, grant, err := s.grantOf(name)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "name", Says: err.Error()}
	}
	var handler mcpauth.OAuthHandler
	if grant != nil {
		if s.authHandler == nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node serves no auth, so no authorization can come back to it"}
		}
		again := func(context.Context, *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
			return nil, errors.Newf("%s no longer takes the grant the node keeps for it: say yes again with mcp connect", at)
		}
		held, err := s.consumedOAuth(admitted, name, at, grant, again)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		handler = held
	}
	conn, err := mcpclient.Dial(ctx, mcpclient.Remote{URL: at, HTTPClient: consumedClient(), OAuth: handler},
		mcpclient.Client{Name: "qntx", Version: version.Get().Version})
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return conn, nil
}

func (s *QNTXServer) consumedTools(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	admitted, refused := consumedUsing(ctx)
	return then(admitted, refused, func(admitted auth.Admission) (any, *protocol.Refusal) {
		name, err := named.TextOf("name", strings.TrimSpace(sent["name"]))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "name", Says: err.Error()}
		}
		ctx, cancel := context.WithTimeout(ctx, consumedCallLife)
		defer cancel()
		conn, refused := s.consumedConn(ctx, admitted, name.String())
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

// 「[Opus 5.5] keepGrant writes the grant down for the server, sealed, as whoever's call it was issued on.」
func (s *QNTXServer) keepGrant(admitted auth.Admission, name, at string, grant consumedGrant) error {
	body, err := json.Marshal(grant)
	if err != nil {
		return errors.Wrapf(err, "the grant for %s did not marshal", name)
	}
	sealed, err := s.sealFor(consumedPurpose, consumedIn, string(body))
	if err != nil {
		return err
	}
	return s.writeConsumedLine(admitted, consumedLineSubject, name, map[string]any{consumedURL: at, consumedSealed: sealed})
}

// 「[Opus 5.5] writeConsumedLine writes one line about a server the node consumes, in system, as whoever's call it was.」
func (s *QNTXServer) writeConsumedLine(admitted auth.Admission, subject, name string, attributes map[string]any, contexts ...string) error {
	store, err := s.held.Write(admitted, consumedIn)
	if err != nil {
		return errors.Wrapf(err, "no store to write the %s line %s in", subject, name)
	}
	id, err := identity.GenerateASUIDWithRetry("AS", subject, name, consumedIn, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line %s", subject, name)
	}
	at := time.Now()
	return errors.Wrapf(store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{subject}, Predicates: []string{name}, Contexts: append([]string{consumedIn}, contexts...),
		Actors: []string{admitted.Identity}, Timestamp: at, CreatedAt: at, Source: "mcp",
		Attributes: attributes,
	}), "the %s line %s was not written", subject, name)
}

// 「[Opus 5.5] newestConsumed is the newest line in system for each server the node consumes.」
func (s *QNTXServer) newestConsumed() (map[string]*types.As, error) {
	reading, err := s.held.Read(consumedIn)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines", consumedLineSubject)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{consumedLineSubject}, Limit: ats.EveryRow})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines", consumedLineSubject)
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

// 「[Opus 5.5] consumedOf is the servers the node consumes: each one whose newest line holds a grant.」
func (s *QNTXServer) consumedOf() ([]consumedServer, *protocol.Refusal) {
	newest, err := s.newestConsumed()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	servers := []consumedServer{}
	for name, as := range newest {
		_, holds := as.Attributes[consumedSealed].(string)
		if open, _ := as.Attributes[consumedOpen].(bool); !holds && !open {
			continue
		}
		at, _ := as.Attributes[consumedURL].(string)
		by, err := named.ItemsOf(consumedLineSubject+" line "+as.ID+", who connected it,", as.Actors)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		servers = append(servers, consumedServer{Name: name, URL: at, By: by.First(), At: as.Timestamp.UTC().Format(time.RFC3339)})
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	return servers, nil
}

// 「[Opus 5.5] grantOf is the server's URL and its grant, unsealed; no grant for a server that asked for none.」
func (s *QNTXServer) grantOf(name string) (string, *consumedGrant, error) {
	newest, err := s.newestConsumed()
	if err != nil {
		return "", nil, err
	}
	as, set := newest[name]
	if !set {
		return "", nil, errors.Newf("the node consumes no MCP server %s: connect one with mcp connect", name)
	}
	where, _ := as.Attributes[consumedURL].(string)
	sealed, holds := as.Attributes[consumedSealed].(string)
	if open, _ := as.Attributes[consumedOpen].(bool); !holds && !open {
		return "", nil, errors.Newf("the MCP server %s was dropped: connect it again with mcp connect", name)
	}
	at, err := named.TextOf(consumedLineSubject+" line "+as.ID+", its url,", where)
	if err != nil {
		return "", nil, err
	}
	if !holds {
		return at.String(), nil, nil
	}
	opened, err := s.unsealFor(consumedPurpose, consumedIn, sealed)
	if err != nil {
		return "", nil, err
	}
	var grant consumedGrant
	if err := json.Unmarshal([]byte(opened), &grant); err != nil {
		return "", nil, errors.Wrapf(err, "the grant for %s did not read", name)
	}
	if grant.Token == nil {
		return "", nil, errors.Newf("the grant for %s holds no token", name)
	}
	return at.String(), &grant, nil
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
		s.logger.Infow("an MCP server's yes refused: finished by somebody who did not ask for it", "server", flow.name)
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
		say(w, http.StatusAccepted, true, "The node is still connecting to "+flow.name+": mcp list says when it is")
		return
	}
	if flow.attempt.err != nil {
		say(w, http.StatusBadGateway, false, "The node is not connected to "+flow.name+": "+flow.attempt.err.Error())
		return
	}
	say(w, http.StatusOK, true, "The node is connected to "+flow.name+", which lists "+strings.Join(flow.attempt.tools, ", "))
}

// "THE NODE'S MCP NEEDS"
//
// "I meant ROOT accessible agents"
//
// 「[Opus 5.5] consumedForAgent is the MCP servers the node consumes, as a ROOT accessible agent is given them:」
// 「[Opus 5.5] each at the node, under its name, reached with the agent's own token. The ROOT accessible agents」
// 「[Opus 5.5] are the ones in system, the Node Agent and each element's, whose tokens stand at ROOT」
// 「[Opus 5.5] (holdRootsAgent). A namespace's agent holds a TOKEN, and is given none.」
func (s *QNTXServer) consumedForAgent(agent *rootAgent, own string) []consumedServer {
	if agent.namespace != auth.NamespaceSystem {
		return nil
	}
	servers, refused := s.consumedOf()
	if refused != nil {
		s.logger.Warnw("the node's agent is given none of the MCP servers the node consumes: they did not read", "agent", agent.called, "error", refused.GetSays())
		return nil
	}
	for i := range servers {
		servers[i].URL = own + ConsumedServedAt + servers[i].Name
	}
	return servers
}

// 「[Opus 5.5] HandleConsumedServer serves one server the node consumes as an MCP server of the node's own: the」
// 「[Opus 5.5] tools it lists, each called on the grant the node keeps, and each call written down as whoever」
// 「[Opus 5.5] called it. The upstream's token never leaves the node; the caller presents its own.」
func (s *QNTXServer) HandleConsumedServer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	admitted, refused := consumedUsing(r.Context())
	if refused != nil {
		writeError(w, http.StatusForbidden, refused.GetSays())
		return
	}
	if _, _, err := s.grantOf(name); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, int64(nodeBodyMost)+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "the MCP request body did not read: "+err.Error())
			return
		}
		if len(body) > nodeBodyMost {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the MCP request body is more than the %d bytes %s takes", nodeBodyMost, r.URL.Path))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
	}
	mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return s.consumedServerFor(r.Context(), admitted, name)
	}, &mcp.StreamableHTTPOptions{Stateless: true, DisableLocalhostProtection: true}).ServeHTTP(w, r)
}

// 「[Opus 5.5] consumedServerFor is the server one request is answered by: one tool per tool the upstream」
// 「[Opus 5.5] lists, as it lists it. An upstream that does not answer is a server with no tools, and the log」
// 「[Opus 5.5] says why.」
func (s *QNTXServer) consumedServerFor(ctx context.Context, admitted auth.Admission, name string) *mcp.Server {
	ctx, done := apartFrom(ctx)
	defer done()
	ctx, cancel := context.WithTimeout(ctx, consumedCallLife)
	defer cancel()
	server := mcp.NewServer(&mcp.Implementation{Name: name, Version: version.VersionTag}, nil)
	conn, refused := s.consumedConn(ctx, admitted, name)
	if refused != nil {
		s.logger.Warnw("an MCP server the node consumes is served with no tools: it did not answer", "server", name, "error", refused.GetSays())
		return server
	}
	listed, err := conn.Tools(ctx)
	if closed := conn.Close(); err != nil || closed != nil {
		s.logger.Warnw("an MCP server the node consumes is served with no tools: its tools did not list", "server", name, "error", errors.WithSecondaryError(err, closed))
		return server
	}
	for _, tool := range listed {
		upstream := tool.Name
		served := &mcp.Tool{Name: tool.Name, Title: tool.Title, Description: tool.Description, InputSchema: tool.InputSchema,
			OutputSchema: tool.OutputSchema, Annotations: tool.Annotations}
		if err := addUpstreamTool(server, served, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return s.consumedCall(ctx, admitted, name, upstream, req.Params.Arguments), nil
		}); err != nil {
			s.logger.Warnw("a tool of an MCP server the node consumes is not served: the SDK does not take it", "server", name, "tool", upstream, "error", err)
		}
	}
	return server
}

// 「[Opus 5.5] addUpstreamTool adds a tool another system listed. The SDK panics on a tool it does not take,」
// 「[Opus 5.5] an input schema that is no object among them, and what an upstream lists is not the node's to」
// 「[Opus 5.5] trust that far: such a tool is not served.」
func addUpstreamTool(server *mcp.Server, tool *mcp.Tool, handler mcp.ToolHandler) (err error) {
	defer func() {
		if refused := recover(); refused != nil {
			err = errors.Newf("%v", refused)
		}
	}()
	server.AddTool(tool, handler)
	return nil
}

// 「[Opus 5.5] consumedCall calls one tool of the server on the grant kept for it, and writes the call down:」
// 「[Opus 5.5] who called which tool of which server, with what, and whether the tool said it failed. What」
// 「[Opus 5.5] the server answered is handed back as it answered it.」
func (s *QNTXServer) consumedCall(ctx context.Context, admitted auth.Admission, name, tool string, arguments json.RawMessage) *mcp.CallToolResult {
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	ctx, done := apartFrom(ctx)
	defer done()
	ctx, cancel := context.WithTimeout(ctx, consumedCallLife)
	defer cancel()
	said := map[string]any{"server": name, "arguments": string(arguments)}
	// 「[Opus 5.5] A token carries the identity that minted it, so which token called is said beside it.」
	if admitted.Grant != nil && admitted.Grant.DID != "" {
		said["token"] = admitted.Grant.DID
	}
	// 「[Opus 5.5] A call that did not reach the tool, or that failed, is written down too, with why.」
	failed := func(why string) *mcp.CallToolResult {
		said["is_error"], said["failed"] = true, why
		if err := s.writeConsumedLine(admitted, consumedCallSubject, tool, said, consumedContext(name)); err != nil {
			why = errors.WithSecondaryError(errors.New(why), err).Error()
		}
		return refusedTool(why)
	}
	conn, refused := s.consumedConn(ctx, admitted, name)
	if refused != nil {
		return failed(refused.GetSays())
	}
	result, err := conn.Call(ctx, tool, arguments)
	if closed := conn.Close(); closed != nil {
		s.logger.Infow("a session with an MCP server the node consumes closed with an error", "server", name, "tool", tool, "error", closed)
	}
	if err != nil {
		return failed(err.Error())
	}
	said["is_error"] = result.IsError
	if err := s.writeConsumedLine(admitted, consumedCallSubject, tool, said, consumedContext(name)); err != nil {
		return refusedTool(errors.Wrapf(err, "%s of %s answered, and the call was not written down, so its answer is not handed on", tool, name).Error())
	}
	return result
}

// 「[Opus 5.5] apartFrom is a context that ends when ctx does and carries none of its values: what the node's」
// 「[Opus 5.5] own MCP server put on the request it answers, its protocol version among them, is not said to the」
// 「[Opus 5.5] upstream.」
func apartFrom(ctx context.Context) (context.Context, func()) {
	apart, cancel := context.WithCancel(context.Background())
	stop := context.AfterFunc(ctx, cancel)
	return apart, func() { stop(); cancel() }
}

func refusedTool(said string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: said}}}
}
