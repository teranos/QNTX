package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	errors "github.com/teranos/sacred-error"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

// 「[Fable 5.1] The fake server: this test binary, run again with MCP_FAKE naming what it does. It answers as an MCP」
// 「[Fable 5.1] server on stdin and stdout.」
func TestMain(m *testing.M) {
	if scenario := os.Getenv("MCP_FAKE"); scenario != "" {
		os.Exit(fakeServer(scenario))
	}
	grace = 200 * time.Millisecond
	os.Exit(m.Run())
}

type rpc struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
}

const opened = `{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"fake","version":"0"}}`

func fakeServer(scenario string) int {
	if scenario == "dies" {
		fmt.Fprintln(os.Stderr, "fake server: dying as told")
		return 3
	}
	if scenario == "deaf" {
		signal.Ignore(syscall.SIGTERM)
	}
	in := bufio.NewScanner(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	say := func(r rpc, what string) {
		fmt.Fprintln(out, `{"jsonrpc":"2.0","id":`+string(r.ID)+`,`+what+`}`)
		out.Flush()
	}
	for in.Scan() {
		var r rpc
		if err := json.Unmarshal(in.Bytes(), &r); err != nil {
			fmt.Fprintln(os.Stderr, "fake server: not JSON:", in.Text())
			return 4
		}
		switch {
		case r.Method == "initialize":
			say(r, `"result":`+opened)
		case r.Method == "tools/call":
			// 「[Fable 5.1] What of the node's environment reached the server.」
			_, leaked := os.LookupEnv("MCPCLIENT_OF_THE_NODE")
			say(r, fmt.Sprintf(`"result":{"content":[{"type":"text","text":"leaked=%t"}]}`, leaked))
		case len(r.ID) > 0:
			// 「[Fable 5.1] Anything else asked of it, server/discover first, is a method it does not have.」
			say(r, `"error":{"code":-32601,"message":"method not found"}`)
		}
	}
	// 「[Fable 5.1] Its input closed. A server that ends there has ended; these two run on.」
	if scenario == "stays" || scenario == "deaf" {
		time.Sleep(time.Minute)
	}
	return 0
}

// 「[Fable 5.1] standIn is the SDK's own server, the one the node serves MCP with, connected in this process.」
func standIn(t *testing.T, options *mcp.ServerOptions, add func(*mcp.Server)) *Conn {
	t.Helper()
	ctx := context.Background()
	clientReads, serverWrites := io.Pipe()
	serverReads, clientWrites := io.Pipe()
	server := mcp.NewServer(&mcp.Implementation{Name: "stand-in", Version: "1.2.3"}, options)
	add(server)
	if _, err := server.Connect(ctx, &mcp.IOTransport{Reader: serverReads, Writer: serverWrites}, nil); err != nil {
		t.Fatalf("the stand-in did not connect: %v", err)
	}
	conn, err := open(ctx, &mcp.IOTransport{Reader: clientReads, Writer: clientWrites}, "stand-in", Client{Name: "qntx-test", Version: "0"})
	if err != nil {
		t.Fatalf("the session did not open: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Logf("the session closed with: %v", err)
		}
	})
	return conn
}

var takesAnything = map[string]any{"type": "object"}

func said(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// 「[Fable 5.1] textOf is the text a result holds, when it holds one piece of text.」
func textOf(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("the result holds %d pieces, and one was answered", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("the result holds %T, and text was answered", result.Content[0])
	}
	return text.Text
}

func TestEveryPageOfToolsIsListed(t *testing.T) {
	names := []string{"a", "b", "c", "d", "e"}
	conn := standIn(t, &mcp.ServerOptions{PageSize: 2}, func(s *mcp.Server) {
		for _, name := range names {
			s.AddTool(&mcp.Tool{Name: name, Description: "does " + name, InputSchema: takesAnything}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return said("ok"), nil
			})
		}
	})
	tools, err := conn.Tools(context.Background())
	if err != nil {
		t.Fatalf("the tools were not listed: %v", err)
	}
	var listed []string
	for _, tool := range tools {
		listed = append(listed, tool.Name)
		if tool.Description != "does "+tool.Name {
			t.Errorf("%s was listed as %q", tool.Name, tool.Description)
		}
	}
	if strings.Join(listed, ",") != strings.Join(names, ",") {
		t.Errorf("listed %v, and the server has %v", listed, names)
	}
}

func TestAToolAnswersWhatItWasCalledWith(t *testing.T) {
	conn := standIn(t, nil, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{Name: "echo", InputSchema: takesAnything}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return said(string(req.Params.Arguments)), nil
		})
	})
	result, err := conn.Call(context.Background(), "echo", map[string]any{"note": "ADR/ADR-038-MCP.md"})
	if err != nil {
		t.Fatalf("echo was not answered: %v", err)
	}
	if result.IsError {
		t.Fatalf("echo said it failed: %s", textOf(t, result))
	}
	var called map[string]string
	if err := json.Unmarshal([]byte(textOf(t, result)), &called); err != nil || called["note"] != "ADR/ADR-038-MCP.md" {
		t.Errorf("echo was called with %s (%v)", textOf(t, result), err)
	}
}

func TestAToolsOwnFailureIsAResult(t *testing.T) {
	conn := standIn(t, nil, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{Name: "read", InputSchema: takesAnything}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			failed := said("no such note")
			failed.IsError = true
			return failed, nil
		})
	})
	result, err := conn.Call(context.Background(), "read", nil)
	if err != nil {
		t.Fatalf("a tool that says it failed answered, and Call failed: %v", err)
	}
	if !result.IsError || textOf(t, result) != "no such note" {
		t.Errorf("the tool said it failed with no such note, and the result is %+v", result)
	}
}

func TestAToolTheServerDoesNotHaveIsAnError(t *testing.T) {
	conn := standIn(t, nil, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{Name: "echo", InputSchema: takesAnything}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return said("ok"), nil
		})
	})
	if _, err := conn.Call(context.Background(), "nothing", nil); err == nil {
		t.Error("a tool the server does not have was answered")
	}
}

// 「[Fable 5.1] Under -race a Go binary sleeps a second as it exits, which is longer than a server is waited on here.」
func fake(scenario string) Server {
	return Server{Binary: os.Args[0], Env: []string{"MCP_FAKE=" + scenario, "GORACE=atexit_sleep_ms=0"}}
}

func TestAServerIsRunAndEndsWhenItsInputCloses(t *testing.T) {
	t.Setenv("MCPCLIENT_OF_THE_NODE", "a secret")
	conn, err := Start(context.Background(), fake("serves"), Client{})
	if err != nil {
		t.Fatalf("the server did not start: %v", err)
	}
	result, err := conn.Call(context.Background(), "env", nil)
	if err != nil {
		t.Fatalf("env was not answered: %v", err)
	}
	if textOf(t, result) != "leaked=false" {
		t.Errorf("the server's environment is what Env names and nothing of the node's, and it answered %s", textOf(t, result))
	}
	if err := conn.Close(); err != nil {
		t.Errorf("a server that ends when its input closes was closed with: %v", err)
	}
}

func TestAServerThatRunsOnIsEndedAndCloseSaysSo(t *testing.T) {
	for _, scenario := range []string{"stays", "deaf"} {
		conn, err := Start(context.Background(), fake(scenario), Client{})
		if err != nil {
			t.Fatalf("%s: the server did not start: %v", scenario, err)
		}
		if err := conn.Close(); err == nil {
			t.Errorf("%s: a server that ran on after its input closed was closed with no error", scenario)
		}
	}
}

func TestAServerThatEndsBeforeItAnswersDoesNotStart(t *testing.T) {
	if _, err := Start(context.Background(), fake("dies"), Client{}); err == nil {
		t.Error("a server that died before answering started")
	}
}

func TestAServerNeedsItsBinary(t *testing.T) {
	if _, err := Start(context.Background(), Server{}, Client{}); err == nil {
		t.Error("a server with no binary started")
	}
	if _, err := Start(context.Background(), Server{Binary: "/nonexistent/mcp-server"}, Client{}); err == nil {
		t.Error("a binary that is not there started")
	}
}

// 「[Opus 5.5] bearing hands every request the one token it holds, and has nothing to say to a 401.」
type bearing struct{ token string }

func (b bearing) TokenSource(context.Context) (oauth2.TokenSource, error) {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: b.token, TokenType: "Bearer"}), nil
}

func (bearing) Authorize(context.Context, *http.Request, *http.Response) error {
	return errors.New("bearing holds one token, and it was refused")
}

// 「[Opus 5.5] remote is the SDK's server at a URL, answering only a request that bears token.」
func remote(t *testing.T, token string) string {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "remote", Version: "0"}, nil)
	server.AddTool(&mcp.Tool{Name: "echo", InputSchema: takesAnything}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return said(string(req.Params.Arguments)), nil
	})
	serving := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	at := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		serving.ServeHTTP(w, r)
	}))
	t.Cleanup(at.Close)
	return at.URL
}

func TestARemoteServerIsReachedWithWhatOAuthBears(t *testing.T) {
	at := remote(t, "the-token")
	conn, err := Dial(context.Background(), Remote{URL: at, OAuth: bearing{token: "the-token"}}, Client{})
	if err != nil {
		t.Fatalf("the session did not open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	result, err := conn.Call(context.Background(), "echo", map[string]any{"note": "remote"})
	if err != nil {
		t.Fatalf("echo was not answered: %v", err)
	}
	if textOf(t, result) != `{"note":"remote"}` {
		t.Errorf("echo answered %s", textOf(t, result))
	}
	if _, err := Dial(context.Background(), Remote{URL: at, OAuth: bearing{token: "another"}}, Client{}); err == nil {
		t.Error("a token the server refuses opened a session")
	}
}

func TestARemoteServerNeedsItsURL(t *testing.T) {
	for _, at := range []string{"", "not a url", "/mcp"} {
		if _, err := Dial(context.Background(), Remote{URL: at}, Client{}); err == nil {
			t.Errorf("%q opened a session", at)
		}
	}
}
