package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	errors "github.com/teranos/sacred-error"
)

// The fake server: this test binary, run again with MCP_FAKE naming what it does. It answers as an MCP
// server on stdin and stdout.
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
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
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
	answer := func(r rpc, result string) {
		fmt.Fprintln(out, `{"jsonrpc":"2.0","id":`+string(r.ID)+`,"result":`+result+`}`)
		out.Flush()
	}
	for in.Scan() {
		var r rpc
		if err := json.Unmarshal(in.Bytes(), &r); err != nil {
			fmt.Fprintln(os.Stderr, "fake server: not JSON:", in.Text())
			return 4
		}
		switch r.Method {
		case "initialize":
			answer(r, opened)
		case "tools/call":
			// What of the node's environment reached the server.
			_, leaked := os.LookupEnv("MCPCLIENT_OF_THE_NODE")
			answer(r, fmt.Sprintf(`{"content":[{"type":"text","text":"leaked=%t"}]}`, leaked))
		}
	}
	// Its input closed. A server that ends there has ended; these two run on.
	if scenario == "stays" || scenario == "deaf" {
		time.Sleep(time.Minute)
	}
	return 0
}

// standIn is the SDK's own server, the one the node serves MCP with, connected in this process.
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
	conn, err := Over(ctx, clientReads, clientWrites, Client{Name: "qntx-test", Version: "0"})
	if err != nil {
		t.Fatalf("the session did not open: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("the session did not close: %v", err)
		}
	})
	return conn
}

var takesAnything = map[string]any{"type": "object"}

func said(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func TestTheSessionOpensAndSaysWhoAnswered(t *testing.T) {
	conn := standIn(t, &mcp.ServerOptions{Instructions: "ask before writing"}, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{Name: "echo", InputSchema: takesAnything}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return said("ok"), nil
		})
	})
	peer := conn.Peer()
	if peer.Name != "stand-in" || peer.Version != "1.2.3" {
		t.Errorf("the server is %q %q, and it said stand-in 1.2.3", peer.Name, peer.Version)
	}
	if peer.Protocol != "2025-11-25" {
		t.Errorf("the session is spoken in %q, and 2025-11-25 was offered", peer.Protocol)
	}
	if peer.Instructions != "ask before writing" {
		t.Errorf("the instructions are %q", peer.Instructions)
	}
	if !peer.Offers("tools") {
		t.Errorf("a server with a tool offers tools, and this one offers %v", peer.Capabilities)
	}
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
		if tool.Description != "does "+tool.Name || len(tool.InputSchema) == 0 || len(tool.Raw) == 0 {
			t.Errorf("%s was listed without what the server said of it: %+v", tool.Name, tool)
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
	if result.IsError || len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("echo answered %s", result.Raw)
	}
	if result.Content[0].Text != `{"note":"ADR/ADR-038-MCP.md"}` {
		t.Errorf("echo was called with %s", result.Content[0].Text)
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
	if !result.IsError || len(result.Content) != 1 || result.Content[0].Text != "no such note" {
		t.Errorf("the tool said it failed with no such note, and the result is %s", result.Raw)
	}
}

func TestAToolTheServerDoesNotHaveIsTheServersError(t *testing.T) {
	conn := standIn(t, nil, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{Name: "echo", InputSchema: takesAnything}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return said("ok"), nil
		})
	})
	_, err := conn.Call(context.Background(), "nothing", nil)
	var answered *Error
	if !errors.As(err, &answered) {
		t.Fatalf("a tool the server does not have is its JSON-RPC error, and Call answered: %v", err)
	}
	if answered.Method != "tools/call" || answered.Code == 0 || answered.Message == "" {
		t.Errorf("the error does not carry what the server answered: %+v", answered)
	}
}

func TestTheServersPingIsAnswered(t *testing.T) {
	conn := standIn(t, nil, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{Name: "ping-back", InputSchema: takesAnything}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := req.Session.Ping(ctx, nil); err != nil {
				return said("the client did not answer: " + err.Error()), nil
			}
			return said("the client answered"), nil
		})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := conn.Call(ctx, "ping-back", nil)
	if err != nil {
		t.Fatalf("ping-back was not answered: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "the client answered" {
		t.Errorf("the server pinged the client mid-call, and %s", result.Raw)
	}
}

func TestACallNobodyWaitsOnIsCancelled(t *testing.T) {
	cancelled := make(chan struct{})
	conn := standIn(t, nil, func(s *mcp.Server) {
		s.AddTool(&mcp.Tool{Name: "slow", InputSchema: takesAnything}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := conn.Call(ctx, "slow", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a call past its deadline fails with the deadline, and Call answered: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the server was not told the call was cancelled")
	}
	// The session holds after one call was given up on.
	if _, err := conn.Tools(context.Background()); err != nil {
		t.Errorf("the session did not hold after a cancelled call: %v", err)
	}
}

// scripted is a server played by hand, for what the SDK's server never does.
type scripted struct {
	t   *testing.T
	in  *bufio.Scanner
	out io.WriteCloser
}

// next is the client's next message, or false when the client closed its side.
func (s *scripted) next() (rpc, bool) {
	if !s.in.Scan() {
		return rpc{}, false
	}
	var r rpc
	if err := json.Unmarshal(s.in.Bytes(), &r); err != nil {
		s.t.Errorf("the client wrote a line that is not JSON: %s", s.in.Text())
	}
	return r, true
}

func (s *scripted) expect(method string) rpc {
	r, ok := s.next()
	if !ok || r.Method != method {
		s.t.Errorf("the client was to send %s, and sent %q (still open: %t)", method, r.Method, ok)
	}
	return r
}

func (s *scripted) say(line string) {
	if _, err := io.WriteString(s.out, line+"\n"); err != nil {
		s.t.Errorf("the scripted server could not write: %v", err)
	}
}

func (s *scripted) answer(r rpc, result string) {
	s.say(`{"jsonrpc":"2.0","id":` + string(r.ID) + `,"result":` + result + `}`)
}

// script runs play as the server, and hands back the client's two ends and when play is over.
func script(t *testing.T, play func(*scripted)) (io.ReadCloser, io.WriteCloser, <-chan struct{}) {
	clientReads, serverWrites := io.Pipe()
	serverReads, clientWrites := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		play(&scripted{t: t, in: bufio.NewScanner(serverReads), out: serverWrites})
	}()
	return clientReads, clientWrites, done
}

func over(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the scripted server did not finish")
	}
}

func TestARevisionTheClientDoesNotSpeakEndsTheSession(t *testing.T) {
	from, to, done := script(t, func(s *scripted) {
		r := s.expect("initialize")
		var offered struct {
			ProtocolVersion string `json:"protocolVersion"`
			ClientInfo      struct {
				Name string `json:"name"`
			} `json:"clientInfo"`
		}
		if err := json.Unmarshal(r.Params, &offered); err != nil || offered.ProtocolVersion != "2025-11-25" || offered.ClientInfo.Name != "qntx" {
			s.t.Errorf("initialize offered %s", r.Params)
		}
		s.answer(r, `{"protocolVersion":"1999-01-01","capabilities":{},"serverInfo":{"name":"old","version":"0"}}`)
		if r, ok := s.next(); ok {
			s.t.Errorf("the client said %s in a session it does not speak", r.Method)
		}
	})
	_, err := Over(context.Background(), from, to, Client{})
	if err == nil || !strings.Contains(err.Error(), `old speaks MCP "1999-01-01"`) {
		t.Errorf("a revision the client does not speak opened a session: %v", err)
	}
	over(t, done)
}

func TestAServerWithNoToolsIsNotAskedForThem(t *testing.T) {
	from, to, done := script(t, func(s *scripted) {
		s.answer(s.expect("initialize"), `{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"bare","version":"0"}}`)
		s.expect("notifications/initialized")
		if r, ok := s.next(); ok {
			s.t.Errorf("the client asked %s of a server that offers no tools", r.Method)
		}
	})
	conn, err := Over(context.Background(), from, to, Client{})
	if err != nil {
		t.Fatalf("the session did not open: %v", err)
	}
	if conn.Peer().Protocol != "2025-06-18" {
		t.Errorf("the server answered in 2025-06-18, and the session is spoken in %q", conn.Peer().Protocol)
	}
	if _, err := conn.Tools(context.Background()); err == nil || !strings.Contains(err.Error(), "bare offers no tools") {
		t.Errorf("listing the tools of a server that offers none answered: %v", err)
	}
	if _, err := conn.Call(context.Background(), "echo", nil); err == nil || !strings.Contains(err.Error(), "bare offers no tools") {
		t.Errorf("calling a tool of a server that offers none answered: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("the session did not close: %v", err)
	}
	over(t, done)
}

func TestWhatTheServerMayNotAskIsRefused(t *testing.T) {
	from, to, done := script(t, func(s *scripted) {
		s.answer(s.expect("initialize"), opened)
		s.expect("notifications/initialized")
		s.say(`{"jsonrpc":"2.0","id":"s-1","method":"sampling/createMessage","params":{}}`)
		refused, _ := s.next()
		var with struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(refused.Error, &with); err != nil || string(refused.ID) != `"s-1"` || with.Code != -32601 {
			s.t.Errorf("sampling/createMessage was answered id %s error %s", refused.ID, refused.Error)
		}
	})
	conn, err := Over(context.Background(), from, to, Client{})
	if err != nil {
		t.Fatalf("the session did not open: %v", err)
	}
	over(t, done)
	if err := conn.Close(); err != nil {
		t.Errorf("the session did not close: %v", err)
	}
}

func TestTheServersEndEndsTheCall(t *testing.T) {
	from, to, done := script(t, func(s *scripted) {
		s.answer(s.expect("initialize"), opened)
		s.expect("notifications/initialized")
		s.expect("tools/call")
		if err := s.out.Close(); err != nil {
			s.t.Errorf("the scripted server could not close: %v", err)
		}
	})
	conn, err := Over(context.Background(), from, to, Client{})
	if err != nil {
		t.Fatalf("the session did not open: %v", err)
	}
	if _, err := conn.Call(context.Background(), "echo", nil); err == nil || !strings.Contains(err.Error(), "tools/call was not answered: fake closed its side") {
		t.Errorf("a call the server ended under answered: %v", err)
	}
	over(t, done)
	if err := conn.Close(); err != nil {
		t.Errorf("the session did not close: %v", err)
	}
}

func TestALineThatIsNoMessageEndsTheSession(t *testing.T) {
	from, to, done := script(t, func(s *scripted) {
		s.answer(s.expect("initialize"), opened)
		s.expect("notifications/initialized")
		s.expect("tools/list")
		s.say("listening on stdio")
	})
	conn, err := Over(context.Background(), from, to, Client{})
	if err != nil {
		t.Fatalf("the session did not open: %v", err)
	}
	if _, err := conn.Tools(context.Background()); err == nil || !strings.Contains(err.Error(), "fake wrote a line that is no JSON-RPC message: listening on stdio") {
		t.Errorf("a line that is no message answered: %v", err)
	}
	over(t, done)
	if err := conn.Close(); err != nil {
		t.Errorf("the session did not close: %v", err)
	}
}

// Under -race a Go binary sleeps a second as it exits, which is longer than a server is waited on here.
func fake(scenario string) Server {
	return Server{Binary: os.Args[0], Env: []string{"MCP_FAKE=" + scenario, "GORACE=atexit_sleep_ms=0"}}
}

func TestAServerIsRunAndEndsWhenItsInputCloses(t *testing.T) {
	t.Setenv("MCPCLIENT_OF_THE_NODE", "a secret")
	conn, err := Start(context.Background(), fake("serves"), Client{})
	if err != nil {
		t.Fatalf("the server did not start: %v", err)
	}
	if conn.Peer().Name != "fake" {
		t.Errorf("the server is %q", conn.Peer().Name)
	}
	result, err := conn.Call(context.Background(), "env", nil)
	if err != nil {
		t.Fatalf("env was not answered: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "leaked=false" {
		t.Errorf("the server's environment is what Env names and nothing of the node's, and it answered %s", result.Raw)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("a server that ends when its input closes was closed with: %v", err)
	}
	if _, err := conn.Call(context.Background(), "env", nil); err == nil {
		t.Error("a closed session answered a call")
	}
}

func TestAServerThatRunsOnIsEndedAndCloseSaysSo(t *testing.T) {
	for scenario, with := range map[string]string{"stays": "SIGTERM", "deaf": "SIGKILL"} {
		conn, err := Start(context.Background(), fake(scenario), Client{})
		if err != nil {
			t.Fatalf("%s: the server did not start: %v", scenario, err)
		}
		err = conn.Close()
		if err == nil || !strings.Contains(err.Error(), "was ended with "+with) {
			t.Errorf("%s: a server that ran on was closed with: %v", scenario, err)
		}
	}
}

func TestAServerThatEndsBeforeItAnswersSaysHow(t *testing.T) {
	_, err := Start(context.Background(), fake("dies"), Client{})
	if err == nil || !strings.Contains(err.Error(), "initialize was not answered") || !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("a server that died before answering started with: %v", err)
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
