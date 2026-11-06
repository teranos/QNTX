package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
	"golang.org/x/oauth2"
)

// 「[Opus 5.5] upstream is another system's MCP server and its authorization server, as the MCP」
// 「[Opus 5.5] authorization spec has them: protected resource metadata, authorization server metadata,」
// 「[Opus 5.5] dynamic client registration, a code for PKCE, and refresh tokens that rotate.」
type upstream struct {
	at        string
	mu        sync.Mutex
	verifier  bool
	refreshed []string
	live      map[string]bool
	// 「[Opus 5.5] open is an upstream that asks for no authorization.」
	open bool
}

const upstreamCode = "the-code"

func standingUpstream(t *testing.T) *upstream {
	t.Helper()
	up := &upstream{live: map[string]bool{}}
	server := mcp.NewServer(&mcp.Implementation{Name: "upstream", Version: "0"}, nil)
	server.AddTool(&mcp.Tool{Name: "issues", Description: "Lists the issues.", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "none"}}}, nil
		})
	serving := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	answer := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		require.NoError(t, json.NewEncoder(w).Encode(body))
	}
	issue := func(w http.ResponseWriter, access, refresh string, expiresIn int) {
		up.mu.Lock()
		up.live[access] = true
		up.mu.Unlock()
		answer(w, http.StatusOK, map[string]any{"access_token": access, "token_type": "Bearer", "refresh_token": refresh, "expires_in": expiresIn})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		answer(w, http.StatusOK, map[string]any{"resource": up.at + "/mcp", "authorization_servers": []string{up.at}, "scopes_supported": []string{"issues"}})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		answer(w, http.StatusOK, map[string]any{
			"issuer": up.at, "authorization_endpoint": up.at + "/authorize", "token_endpoint": up.at + "/token",
			"registration_endpoint": up.at + "/register", "response_types_supported": []string{"code"},
			"code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{"issues", "offline_access"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		var asked map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&asked))
		asked["client_id"] = "qntx-at-upstream"
		asked["token_endpoint_auth_method"] = "none"
		answer(w, http.StatusCreated, asked)
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		back, err := url.Parse(q.Get("redirect_uri"))
		require.NoError(t, err)
		said := back.Query()
		said.Set("code", upstreamCode)
		said.Set("state", q.Get("state"))
		back.RawQuery = said.Encode()
		http.Redirect(w, r, back.String(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			if r.PostForm.Get("code") != upstreamCode {
				answer(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
				return
			}
			up.mu.Lock()
			up.verifier = r.PostForm.Get("code_verifier") != ""
			up.mu.Unlock()
			// 「[Opus 5.5] Expired as it is issued, so the first request refreshes it.」
			issue(w, "access-1", "refresh-1", 1)
		case "refresh_token":
			up.mu.Lock()
			up.refreshed = append(up.refreshed, r.PostForm.Get("refresh_token"))
			n := len(up.refreshed)
			up.mu.Unlock()
			if r.PostForm.Get("refresh_token") != "refresh-1" {
				answer(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
				return
			}
			issue(w, "access-2", "refresh-2", 3600+n)
		}
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		up.mu.Lock()
		live := up.live[bearer]
		up.mu.Unlock()
		if !live && !up.open {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+up.at+`/.well-known/oauth-protected-resource/mcp"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		serving.ServeHTTP(w, r)
	})
	at := httptest.NewTLSServer(mux)
	t.Cleanup(at.Close)
	up.at = at.URL
	reaching := consumedClient
	consumedClient = at.Client
	t.Cleanup(func() { consumedClient = reaching })
	return up
}

// 「[Opus 5.5] consumedAs is a call on the mcp signum by an admission.」
func consumedAs(s *QNTXServer, admitted auth.Admission, name string, sent sigil.Sent) (any, *protocol.Refusal) {
	asked := httptest.NewRequest(http.MethodPost, "/api/mcp", nil)
	ctx := sigil.WithCaller(auth.WithAdmission(context.Background(), admitted), asked)
	return s.consumedSignum().Answers[name](ctx, sent)
}

// 「[Opus 5.5] sayingYes follows the URL to say yes at, and is the request the person's browser makes of the」
// 「[Opus 5.5] callback once the server's authorization sends it back, logged in as admitted.」
func sayingYes(t *testing.T, authorize string, admitted auth.Admission) *http.Request {
	t.Helper()
	client := consumedClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Get(authorize)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusFound, resp.StatusCode)
	back, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, ConsumedCallbackPath, back.Path, "the server sends the person back to the node's callback")
	return httptest.NewRequest(http.MethodGet, back.RequestURI(), nil).WithContext(auth.WithAdmission(context.Background(), admitted))
}

var asROOT = person("https://example.org/root", auth.LevelRoot)

// 「[Opus 5.5] nodeConnected is the node connected to the upstream as tracker, by ROOT saying yes.」
func nodeConnected(t *testing.T, s *QNTXServer, up *upstream) {
	t.Helper()
	connecting, refused := consumedAs(s, asROOT, "connect", sigil.Sent{"name": "tracker", "url": up.at + "/mcp"})
	require.Nil(t, refused)
	back := httptest.NewRecorder()
	s.handleConsumedCallback(back, sayingYes(t, connecting.(consumedConnecting).Authorize, asROOT))
	require.Equal(t, http.StatusOK, back.Code, back.Body.String())
}

// "SO, I COULD HAVE THIS BE THE 1 POINT WHERE I SET UP ALL MY MCP NEEDS AS ROOT FOR THE NODE"
//
// 「[Opus 5.5] ROOT connects the node to a server that asks for authorization: connect answers where to say」
// 「[Opus 5.5] yes, the callback hands the code to the SDK's flow, the token is refreshed as the first request」
// 「[Opus 5.5] finds it expired, and what is kept in system is the newest grant, sealed.」
func TestROOTConnectsTheNodeToAnMCPServerAndTheGrantIsKeptSealed(t *testing.T) {
	s, _, in := runningNamespaceAgents(t)
	up := standingUpstream(t)

	connecting, refused := consumedAs(s, asROOT, "connect", sigil.Sent{"name": "tracker", "url": up.at + "/mcp"})
	require.Nil(t, refused)
	holds(t, s.consumedSignum(), "connect", connecting)
	authorize := connecting.(consumedConnecting).Authorize
	require.True(t, strings.HasPrefix(authorize, up.at+"/authorize?"), authorize)
	assert.Contains(t, authorize, "code_challenge_method=S256")
	assert.Contains(t, authorize, "offline_access")

	back := httptest.NewRecorder()
	s.handleConsumedCallback(back, sayingYes(t, authorize, asROOT))
	require.Equal(t, http.StatusOK, back.Code, back.Body.String())
	assert.Contains(t, back.Body.String(), "The node is connected to tracker, which lists issues")
	assert.True(t, up.verifier, "the code is exchanged with its PKCE verifier")
	assert.Equal(t, []string{"refresh-1"}, up.refreshed, "the expired token is refreshed once")

	listed, refused := consumedAs(s, asROOT, "list", sigil.Sent{})
	require.Nil(t, refused)
	holds(t, s.consumedSignum(), "list", listed)
	require.Len(t, listed.([]consumedServer), 1)
	assert.Equal(t, consumedServer{Name: "tracker", URL: up.at + "/mcp", By: asROOT.Identity, At: listed.([]consumedServer)[0].At}, listed.([]consumedServer)[0])

	reading, err := s.held.Read(consumedIn)
	require.NoError(t, err)
	kept, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{consumedLineSubject}, Limit: ats.EveryRow})
	require.NoError(t, err)
	require.NotEmpty(t, kept)
	for _, row := range kept {
		for _, value := range row.Attributes {
			text, isText := value.(string)
			for _, secret := range []string{"access-1", "access-2", "refresh-1", "refresh-2"} {
				assert.False(t, isText && strings.Contains(text, secret), "the grant is kept sealed: %v", row.Attributes)
			}
		}
	}
	for _, namespace := range []string{"garden", "orchard"} {
		assert.Empty(t, in.store[namespace].rows, "no namespace holds a line of what the node consumes")
	}
	_, grant, err := s.grantOf("tracker")
	require.NoError(t, err)
	require.NotNil(t, grant)
	assert.Equal(t, "refresh-2", grant.Token.RefreshToken, "the token the refresh rotated to is the one kept")
	assert.Equal(t, "qntx-at-upstream", grant.ClientID, "the client registered is kept, since a refresh is asked as it")

	tools, refused := consumedAs(s, asROOT, "tools", sigil.Sent{"name": "tracker"})
	require.Nil(t, refused)
	holds(t, s.consumedSignum(), "tools", tools)
	assert.Equal(t, []consumedTool{{Name: "issues", Description: "Lists the issues."}}, tools)
	assert.Equal(t, []string{"refresh-1"}, up.refreshed, "the grant kept is used as it is while it lives")

	dropped, refused := consumedAs(s, asROOT, "drop", sigil.Sent{"name": "tracker"})
	require.Nil(t, refused)
	holds(t, s.consumedSignum(), "drop", dropped)
	assert.Empty(t, dropped)
	_, refused = consumedAs(s, asROOT, "tools", sigil.Sent{"name": "tracker"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.Says, "dropped")
}

// 「[Opus 5.5] The code goes only to the yes the person logged in asked for: anybody else coming back with」
// 「[Opus 5.5] it is refused, ROOT under another identity too, and the yes still waits for the one who asked.」
func TestTheCodeGoesOnlyToWhoAskedForTheYes(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	up := standingUpstream(t)

	connecting, refused := consumedAs(s, asROOT, "connect", sigil.Sent{"name": "tracker", "url": up.at + "/mcp"})
	require.Nil(t, refused)
	authorize := connecting.(consumedConnecting).Authorize

	stranger := httptest.NewRecorder()
	s.handleConsumedCallback(stranger, sayingYes(t, authorize, person("https://example.org/another-root", auth.LevelRoot)))
	assert.Equal(t, http.StatusForbidden, stranger.Code)
	_, _, err := s.grantOf("tracker")
	assert.Error(t, err, "nothing is kept from a yes somebody else brought back")

	back := httptest.NewRecorder()
	s.handleConsumedCallback(back, sayingYes(t, authorize, asROOT))
	assert.Equal(t, http.StatusOK, back.Code, back.Body.String())

	again := httptest.NewRecorder()
	s.handleConsumedCallback(again, sayingYes(t, authorize, asROOT))
	assert.Equal(t, http.StatusBadRequest, again.Code, "a yes is finished once")
}

// "The thing you built, can we have it root only"
//
// 「[Opus 5.5] Nobody below ROOT uses or handles what the node consumes; a token at ROOT uses it and does not」
// 「[Opus 5.5] connect or drop it, and neither does a connector: the yes is a person's. A URL is https.」
func TestTheMCPServersTheNodeConsumesAreROOTs(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)

	for who, admitted := range map[string]auth.Admission{
		"SUPER": person("https://example.org/super", auth.LevelSuper),
		"USER":  person(gardener, auth.LevelUser, "garden"),
	} {
		for _, sigilName := range []string{"list", "connect", "tools", "drop"} {
			_, refused := consumedAs(s, admitted, sigilName, sigil.Sent{"name": "tracker", "url": "https://tracker.example/mcp", "tool": "issues"})
			require.NotNil(t, refused, who+" "+sigilName)
			assert.Equal(t, sigil.NotAllowed, refused.Why, who+" "+sigilName)
		}
	}

	token := person("https://example.org/root", auth.LevelRoot)
	token.Grant = &access.Grant{}
	connector := person("https://example.org/root", auth.LevelRoot)
	connector.ClientDID = "did:key:z6MkTheConnector"
	for who, admitted := range map[string]auth.Admission{"a token": token, "the connector": connector} {
		for _, handling := range []string{"connect", "drop"} {
			_, refused := consumedAs(s, admitted, handling, sigil.Sent{"name": "tracker", "url": "https://tracker.example/mcp"})
			require.NotNil(t, refused, who+" "+handling)
			assert.Contains(t, refused.Says, who, handling)
		}
	}

	for _, at := range []string{"http://tracker.example/mcp", "tracker.example/mcp", ""} {
		_, refused := consumedAs(s, asROOT, "connect", sigil.Sent{"name": "tracker", "url": at})
		assert.NotNil(t, refused, at)
	}
}

// "THE NODE'S MCP NEEDS"
//
// 「[Opus 5.5] End to end: ROOT connects the node, the Node Agent is given the server in its own MCP list, and」
// 「[Opus 5.5] with its own token, through the node's gate, it lists the server's tools and calls one. The call」
// 「[Opus 5.5] is written down in system, and the upstream's token never reaches the agent.」
func TestTheNodeAgentIsGivenTheServerROOTConnectedAndCallsIt(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	up := standingUpstream(t)
	nodeConnected(t, s, up)
	s.authEnabled = true
	s.rlAuth, s.rlWS, s.rlWrite, s.rlRead, s.rlPublic = newRateLimitGroup(100, 100), newRateLimitGroup(100, 100), newRateLimitGroup(100, 100), newRateLimitGroup(100, 100), newRateLimitGroup(100, 100)
	if s.answering == nil {
		s.setupHTTPRoutes()
	}
	require.NoError(t, s.open())
	node := httptest.NewServer(s.served)
	t.Cleanup(node.Close)

	agent, err := theNodeAgent(s.nodeDID.PrivateKey, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.authHandler.HoldNodeAgent(agent.token, agent.did))

	given := s.consumedForAgent(agent, node.URL)
	require.Len(t, given, 1)
	assert.Equal(t, "tracker", given[0].Name)
	assert.Equal(t, node.URL+ConsumedServedAt+"tracker", given[0].URL, "the agent reaches the server at the node, never at the upstream")
	assert.NotContains(t, given[0].URL, up.at)
	for _, server := range s.piMCP(agent, node.URL) {
		assert.Equal(t, agent.token, server.Bearer, "every server the agent is given is reached with its own token: %s", server.Name)
	}

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "node-agent", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: given[0].URL, HTTPClient: &http.Client{Transport: bearing{token: agent.token}}}, nil)
	require.NoError(t, err, "the node did not serve the server ROOT connected to its agent")
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Len(t, listed.Tools, 1)
	assert.Equal(t, "issues", listed.Tools[0].Name)
	assert.Equal(t, "Lists the issues.", listed.Tools[0].Description)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "issues", Arguments: map[string]any{"state": "open"}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Equal(t, "none", result.Content[0].(*mcp.TextContent).Text)

	reading, err := s.held.Read(consumedIn)
	require.NoError(t, err)
	lines, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{consumedCallSubject}, Limit: ats.EveryRow})
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Equal(t, []string{"issues"}, lines[0].Predicates)
	assert.Equal(t, []string{consumedIn, "mcp:tracker"}, lines[0].Contexts)
	assert.Equal(t, agent.did, lines[0].Attributes["token"], "which token called is written down")
	assert.Equal(t, `{"state":"open"}`, lines[0].Attributes["arguments"])

	listedServers, refused := consumedAs(s, asROOT, "list", sigil.Sent{})
	require.Nil(t, refused)
	require.Len(t, listedServers.([]consumedServer), 1, "a call line is no server: the server the node consumes is still listed")

	stranger, err := http.Post(given[0].URL, "application/json", strings.NewReader(`{}`))
	require.NoError(t, err)
	_ = stranger.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, stranger.StatusCode, "nobody reaches the server the node consumes without a credential")
}

// 「[Opus 5.5] A namespace's agent is given none of what the node consumes.」
func TestANamespacesAgentIsGivenNoneOfWhatTheNodeConsumes(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	nodeConnected(t, s, standingUpstream(t))
	agent, refused := s.holdNamespaceAgent("garden", "claude-opus-5-5", gardener)
	require.Nil(t, refused)
	assert.Empty(t, s.consumedForAgent(agent, "http://node"))
	assert.Len(t, s.piMCP(agent, "http://node"), 1, "the node's own MCP, and nothing it consumes")
}

// 「[Opus 5.5] A server is named as the node's agents are given it: not qntx, the node's own.」
func TestAServerIsNamedAsTheNodesAgentsAreGivenIt(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	for _, name := range []string{"qntx", "Tracker", "track-er", "a b"} {
		_, refused := consumedAs(s, asROOT, "connect", sigil.Sent{"name": name, "url": "https://tracker.example/mcp"})
		require.NotNil(t, refused, name)
		assert.Equal(t, "name", refused.Param, name)
	}
}

// 「[Opus 5.5] The node reaches no address the internet does not route to, whatever a URL names.」
func TestTheNodeReachesNoPrivateAddressForAnMCPServer(t *testing.T) {
	at := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer at.Close()
	_, err := consumedClient().Get(at.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no public address the node reaches")
}

func TestTheTableGivesTheMCPServersTheNodeConsumesToROOT(t *testing.T) {
	compiled, err := reach.Reached()
	require.NoError(t, err)
	s, _, _ := runningNamespaceAgents(t)
	for _, held := range s.consumedSignum().GetSigils() {
		assert.Equal(t, []string{"ROOT"}, compiled[held.GetHttp().GetPath()], held.GetHttp().GetPath())
	}
	assert.Equal(t, []string{"ROOT"}, compiled[ConsumedCallbackPath])
	require.NoError(t, s.consumedSignum().Check())
}

// 「[Opus 5.5] A server that asks for no authorization is kept as open: listed, given to the node's agents, and」
// 「[Opus 5.5] reached as it is, with no grant.」
func TestAServerThatAsksForNoYesIsKeptOpen(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	up := standingUpstream(t)
	up.open = true

	connecting, refused := consumedAs(s, asROOT, "connect", sigil.Sent{"name": "tracker", "url": up.at + "/mcp"})
	require.Nil(t, refused)
	assert.Empty(t, connecting.(consumedConnecting).Authorize)
	assert.Equal(t, []string{"issues"}, connecting.(consumedConnecting).Tools)

	listed, refused := consumedAs(s, asROOT, "list", sigil.Sent{})
	require.Nil(t, refused)
	require.Len(t, listed.([]consumedServer), 1, "a server connected without a yes is still connected")
	_, grant, err := s.grantOf("tracker")
	require.NoError(t, err)
	assert.Nil(t, grant)
	tools, refused := consumedAs(s, asROOT, "tools", sigil.Sent{"name": "tracker"})
	require.Nil(t, refused)
	assert.Equal(t, []consumedTool{{Name: "issues", Description: "Lists the issues."}}, tools)
	agent, err := theNodeAgent(s.nodeDID.PrivateKey, t.TempDir())
	require.NoError(t, err)
	assert.Len(t, s.consumedForAgent(agent, "http://node"), 1)
}

// 「[Opus 5.5] A redirect away from https is not followed: the grant never goes in the clear.」
func TestARedirectAwayFromHTTPSIsNotFollowed(t *testing.T) {
	from := httptest.NewRequest(http.MethodPost, "https://tracker.example/mcp", nil)
	assert.Error(t, consumedRedirect(httptest.NewRequest(http.MethodPost, "http://tracker.example/mcp", nil), []*http.Request{from}))
	assert.NoError(t, consumedRedirect(httptest.NewRequest(http.MethodPost, "https://tracker.example/v2/mcp", nil), []*http.Request{from}))
}

// 「[Opus 5.5] A call that fails is written down too, with why.」
func TestAFailedCallIsWrittenDown(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	nodeConnected(t, s, standingUpstream(t))
	result := s.consumedCall(context.Background(), asROOT, "tracker", "nothing", json.RawMessage(`{"q":1}`))
	require.True(t, result.IsError)

	reading, err := s.held.Read(consumedIn)
	require.NoError(t, err)
	lines, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{consumedCallSubject}, Limit: ats.EveryRow})
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Equal(t, []string{"nothing"}, lines[0].Predicates)
	assert.Equal(t, true, lines[0].Attributes["is_error"])
	assert.NotEmpty(t, lines[0].Attributes["failed"])
	assert.Equal(t, `{"q":1}`, lines[0].Attributes["arguments"])
}

// 「[Opus 5.5] Every connection to a server shares one token source, so a refresh is asked once.」
func TestEveryConnectionToAServerSharesOneTokenSource(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	nodeConnected(t, s, standingUpstream(t))
	at, grant, err := s.grantOf("tracker")
	require.NoError(t, err)
	never := func(context.Context, *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
		return nil, nil
	}
	first, err := s.consumedOAuth(asROOT, "tracker", at, grant, never)
	require.NoError(t, err)
	second, err := s.consumedOAuth(asROOT, "tracker", at, grant, never)
	require.NoError(t, err)
	one, err := first.TokenSource(context.Background())
	require.NoError(t, err)
	other, err := second.TokenSource(context.Background())
	require.NoError(t, err)
	assert.Same(t, one, other)
}

// 「[Opus 5.5] A refresh that keeps the access token and rotates the refresh token is kept.」
func TestARotatedRefreshTokenIsKept(t *testing.T) {
	var kept []string
	source := &keptSource{from: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "a", RefreshToken: "r2"}), last: oauth2.Token{AccessToken: "a", RefreshToken: "r1"},
		keep: func(token *oauth2.Token) error { kept = append(kept, token.RefreshToken); return nil }}
	_, err := source.Token()
	require.NoError(t, err)
	assert.Equal(t, []string{"r2"}, kept)
}
