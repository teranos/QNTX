//go:build libacp

package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	errors "github.com/teranos/sacred-error"
)

// 「[Opus 5.5] The fake agent: this test binary, run again with ACP_FAKE naming what it does. It answers as an ACP」
// 「[Opus 5.5] agent on stdin and stdout, and fails the turn when a client message is not the one it expects.」
func TestMain(m *testing.M) {
	if scenario := os.Getenv("ACP_FAKE"); scenario != "" {
		os.Exit(fakeAgent(scenario))
	}
	os.Exit(m.Run())
}

type rpc struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

func fakeAgent(scenario string) int {
	in := bufio.NewScanner(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	say := func(s string) { fmt.Fprintln(out, s); out.Flush() }
	next := func() rpc {
		if !in.Scan() {
			fmt.Fprintln(os.Stderr, "fake agent: the client closed its side")
			os.Exit(3)
		}
		var r rpc
		if err := json.Unmarshal(in.Bytes(), &r); err != nil {
			fmt.Fprintln(os.Stderr, "fake agent: not JSON:", in.Text())
			os.Exit(3)
		}
		return r
	}
	expect := func(method string) rpc {
		r := next()
		if r.Method != method {
			fmt.Fprintf(os.Stderr, "fake agent: expected %s, got %s\n", method, r.Method)
			os.Exit(4)
		}
		return r
	}
	answer := func(r rpc, result string) { say(`{"jsonrpc":"2.0","id":` + string(r.ID) + `,"result":` + result + `}`) }
	update := func(u string) {
		say(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s-1","update":` + u + `}}`)
	}

	init := expect("initialize")
	var params struct {
		ProtocolVersion int                   `json:"protocolVersion"`
		ClientInfo      struct{ Name string } `json:"clientInfo"`
	}
	json.Unmarshal(init.Params, &params)
	if params.ProtocolVersion != 1 || params.ClientInfo.Name != "qntx-test" {
		fmt.Fprintf(os.Stderr, "fake agent: initialize said %s\n", init.Params)
		return 4
	}
	answer(init, `{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"sessionCapabilities":{"resume":{}}},"agentInfo":{"name":"fake","version":"9"},"authMethods":[{"id":"key","name":"Key"}]}`)

	if scenario == "hangs" {
		time.Sleep(time.Hour)
	}
	if scenario == "dies" {
		fmt.Fprintln(os.Stderr, "fake agent: AGY_ERROR the model is gone")
		return 5
	}
	if scenario == "auth" {
		r := expect("authenticate")
		if !strings.Contains(string(r.Params), `"methodId":"key"`) {
			fmt.Fprintf(os.Stderr, "fake agent: authenticate said %s\n", r.Params)
			return 4
		}
		answer(r, `{}`)
	}
	if scenario == "resume" {
		r := expect("session/resume")
		if !strings.Contains(string(r.Params), `"sessionId":"s-1"`) || !strings.Contains(string(r.Params), `"mcpServers":[]`) {
			fmt.Fprintf(os.Stderr, "fake agent: resume said %s\n", r.Params)
			return 4
		}
		answer(r, `{}`)
	} else {
		r := expect("session/new")
		answer(r, `{"sessionId":"s-1"}`)
	}
	set := expect("session/set_config_option")
	if !strings.Contains(string(set.Params), `"configId":"model"`) || !strings.Contains(string(set.Params), `"value":"m-1"`) {
		fmt.Fprintf(os.Stderr, "fake agent: set_config_option said %s\n", set.Params)
		return 4
	}
	answer(set, `{"configOptions":[]}`)

	prompt := expect("session/prompt")
	if !strings.Contains(string(prompt.Params), `"text":"say pong"`) {
		fmt.Fprintf(os.Stderr, "fake agent: prompt said %s\n", prompt.Params)
		return 4
	}
	if scenario == "refuses" {
		say(`{"jsonrpc":"2.0","id":` + string(prompt.ID) + `,"error":{"code":-32603,"message":"the model refused","data":{"why":"quota"}}}`)
		expect("session/cancel")
		return 0
	}
	update(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"say pong"}}`)
	update(`{"sessionUpdate":"tool_call","toolCallId":"c1","title":"ls","kind":"execute","status":"pending","rawInput":{"command":"ls"}}`)
	// 「[Opus 5.5] The client may ask nothing of the agent's that it does not offer, and answers what it asks.」
	say(`{"jsonrpc":"2.0","id":"r1","method":"fs/read_text_file","params":{"sessionId":"s-1","path":"/etc/passwd"}}`)
	refused := next()
	if string(refused.ID) != `"r1"` || !strings.Contains(in.Text(), `"code":-32601`) {
		fmt.Fprintf(os.Stderr, "fake agent: fs/read_text_file was answered %s\n", in.Text())
		return 4
	}
	say(`{"jsonrpc":"2.0","id":"p1","method":"session/request_permission","params":{"sessionId":"s-1","toolCall":{"toolCallId":"c1","title":"ls"},"options":[{"optionId":"yes","name":"Allow","kind":"allow_once"},{"optionId":"no","name":"Reject","kind":"reject_once"}]}}`)
	outcome := next()
	if string(outcome.ID) != `"p1"` || !strings.Contains(string(outcome.Result), `"optionId":"no"`) {
		fmt.Fprintf(os.Stderr, "fake agent: permission was answered %s\n", in.Text())
		return 4
	}
	update(`{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"failed"}`)
	update(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"po"}}`)
	update(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ng"}}`)
	update(`{"sessionUpdate":"usage_update","used":120,"size":1000}`)
	answer(prompt, `{"stopReason":"end_turn"}`)
	return 0
}

func fakeRun(t *testing.T, scenario string, turn Turn) (Answer, []Update, error) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	turn.Cwd, turn.Says, turn.Client, turn.Version = t.TempDir(), "say pong", "qntx-test", "1"
	turn.Config = [][2]string{{"model", "m-1"}}
	var seen []Update
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	answer, err := Run(ctx, Agent{Binary: self, Env: append(os.Environ(), "ACP_FAKE="+scenario)}, turn, func(u Update) { seen = append(seen, u) })
	return answer, seen, err
}

func TestRunStartsASessionAndSaysOneThing(t *testing.T) {
	answer, seen, err := fakeRun(t, "new", Turn{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer.Session != "s-1" || answer.Text != "pong" || answer.Stop != "end_turn" || answer.Agent != "fake" || answer.AgentVersion != "9" {
		t.Errorf("answer = %+v", answer)
	}
	if answer.Used != 120 || answer.Size != 1000 {
		t.Errorf("usage = %d of %d", answer.Used, answer.Size)
	}
	var kinds []UpdateKind
	for _, u := range seen {
		kinds = append(kinds, u.Kind)
	}
	want := []UpdateKind{Started, UserMessage, ToolCall, ToolCallUpdate, AgentMessage, AgentMessage, Usage}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("updates = %v, want %v", kinds, want)
	}
	if seen[0].Session != "s-1" || seen[0].Resumed {
		t.Errorf("started = %+v", seen[0])
	}
	call := seen[2]
	if call.ToolCallID != "c1" || call.Title != "ls" || call.ToolKind != "execute" || call.RawInput != `{"command":"ls"}` {
		t.Errorf("tool call = %+v", call)
	}
}

func TestRunSignsInFirst(t *testing.T) {
	if _, _, err := fakeRun(t, "auth", Turn{Auth: "key"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunContinuesASession(t *testing.T) {
	answer, seen, err := fakeRun(t, "resume", Turn{Session: "s-1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer.Session != "s-1" || len(seen) == 0 || !seen[0].Resumed {
		t.Errorf("answer %+v, started %+v", answer, seen)
	}
}

func TestRunAsksThePermissionPolicy(t *testing.T) {
	var asked Asked
	_, _, err := fakeRun(t, "new", Turn{Permission: func(a Asked) string { asked = a; return "no" }})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if asked.ToolCallID != "c1" || len(asked.Options) != 2 || asked.Options[0].Kind != "allow_once" {
		t.Errorf("asked = %+v", asked)
	}
}

func TestRunCarriesTheAgentsError(t *testing.T) {
	_, _, err := fakeRun(t, "refuses", Turn{})
	var rpcErr *Error
	if err == nil || !errors.As(err, &rpcErr) {
		t.Fatalf("err = %v, want an *Error", err)
	}
	if rpcErr.Method != "session/prompt" || rpcErr.Code != -32603 || rpcErr.Message != "the model refused" || rpcErr.Data != `{"why":"quota"}` {
		t.Errorf("error = %+v", rpcErr)
	}
}

func TestRunCarriesWhatTheAgentSaidAsItDied(t *testing.T) {
	_, _, err := fakeRun(t, "dies", Turn{})
	if err == nil || !strings.Contains(err.Error(), "AGY_ERROR the model is gone") {
		t.Fatalf("err = %v, want the agent's stderr", err)
	}
}

func TestRunRefusesATurnWithoutCwd(t *testing.T) {
	if _, err := Run(context.Background(), Agent{Binary: "x"}, Turn{Says: "x", Cwd: "relative"}, nil); err == nil {
		t.Fatal("a relative cwd was run")
	}
}

// 「[Opus 5.5] A turn left is stopped, the agent with it, however the agent holds on.」
func TestRunStopsWhenLeft(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := t.TempDir() + "/holds"
	// 「[Opus 5.5] A child keeping the agent's stdout must not keep the turn waiting.」
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30 &\nexec sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, agent := range map[string]Agent{
		"hung":             {Binary: self, Env: append(os.Environ(), "ACP_FAKE=hangs")},
		"inherited stdout": {Binary: script},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := Run(ctx, agent, Turn{Cwd: t.TempDir(), Says: "x", Client: "qntx-test"}, nil)
			if err == nil || time.Since(start) > 3*time.Second {
				t.Fatalf("err = %v after %s", err, time.Since(start))
			}
		})
	}
}

// 「[Opus 5.5] What the agent writes on stderr is kept to its last 4 KiB while it runs.」
func TestStderrStaysBounded(t *testing.T) {
	var b tail
	for range 100 {
		_, _ = b.Write([]byte(strings.Repeat("a", 1000)))
	}
	_, _ = b.Write([]byte("last"))
	if len(b.data) != 4096 || !strings.HasSuffix(b.String(), "last") {
		t.Fatalf("kept %d bytes", len(b.data))
	}
}
