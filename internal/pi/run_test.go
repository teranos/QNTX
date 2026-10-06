package pi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// standIn is a program that stands in for Pi: it writes down how it was run,
// then prints the stream it was given and exits as told.
func standIn(t *testing.T, stream string, exit int, stderr string) (binary, ran string) {
	t.Helper()
	dir := t.TempDir()
	ran = filepath.Join(dir, "ran")
	if err := os.MkdirAll(ran, 0o755); err != nil {
		t.Fatal(err)
	}
	canned := filepath.Join(dir, "stream")
	if err := os.WriteFile(canned, []byte(stream), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do printf '%s\\n' \"$arg\"; done > '" + ran + "/args'\n" +
		"env > '" + ran + "/env'\n" +
		"pwd > '" + ran + "/cwd'\n" +
		"cat '" + canned + "'\n"
	if stderr != "" {
		script += "printf '%s\\n' '" + stderr + "' >&2\n"
	}
	script += "exit " + string(rune('0'+exit)) + "\n"
	binary = filepath.Join(dir, "pi")
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, ran
}

func linesOf(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s did not read: %v", path, err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

func after(args []string, name string) string {
	at := slices.Index(args, name)
	if at < 0 || at+1 >= len(args) {
		return ""
	}
	return args[at+1]
}

// A run that reached for one tool and answered, in the shapes Pi's JSON mode
// documents (docs/json.md, docs/message-types.md).
const answered = `{"type":"session","version":3,"id":"s-1","timestamp":"2026-10-05T20:00:00.000Z","cwd":"/work"}
{"type":"agent_start"}
{"type":"turn_start"}
{"type":"message_end","message":{"role":"user","content":"how long has the box been up?","timestamp":1791230400000}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"uptime"}}],"provider":"qntx","model":"anthropic/claude-sonnet-4.6","usage":{"input":10,"output":5,"totalTokens":15,"cost":{"total":0.01}},"stopReason":"toolUse","timestamp":1791230401000}}
{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"uptime"}}
{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"up 3 days"}]},"isError":false}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Up 3 days."}],"provider":"qntx","model":"anthropic/claude-sonnet-4.6","usage":{"input":20,"output":4,"totalTokens":24,"cost":{"total":0.02}},"stopReason":"stop","timestamp":1791230402000}}
{"type":"agent_end","messages":[],"willRetry":false}
{"type":"agent_settled"}
`

func said(binary, home string) Said {
	return Said{
		Binary: binary, Home: home, Session: "s-1", Says: "how long has the box been up?",
		Model: "qntx/anthropic/claude-sonnet-4.6", Thinking: "low", System: "You are the ROOT agent.",
		MCP:      []MCPServer{{Name: "qntx", URL: "http://127.0.0.1:8770/mcp", Bearer: "the-agent-token"}},
		Provider: &Provider{Name: "qntx", BaseURL: "http://127.0.0.1:8770/api/openrouter-qntx/v1", Key: "the-agent-token", Models: []string{"anthropic/claude-sonnet-4.6"}},
		Env:      []string{"GIT_CONFIG_GLOBAL=/agent/gitconfig"},
	}
}

// What is said is said to Pi in JSON mode, in the agent's one session, and
// what it answered is read off its stream.
func TestSaidRunsPiAndReadsItsAnswer(t *testing.T) {
	binary, ran := standIn(t, answered, 0, "")
	home := t.TempDir()

	var seen []string
	answer, err := said(binary, home).Run(context.Background(), func(e Event) { seen = append(seen, e.Type) })
	if err != nil {
		t.Fatal(err)
	}
	if answer.Text != "Up 3 days." || answer.IsError || answer.Model != "anthropic/claude-sonnet-4.6" || answer.Session != "s-1" {
		t.Fatalf("answer = %+v", answer)
	}
	if answer.CostUSD < 0.0299 || answer.CostUSD > 0.0301 {
		t.Fatalf("cost = %v, the two assistant messages' totals summed", answer.CostUSD)
	}
	if !slices.Contains(seen, "tool_execution_start") || !slices.Contains(seen, "agent_settled") {
		t.Fatalf("events handed on: %v", seen)
	}

	args := linesOf(t, filepath.Join(ran, "args"))
	for flag, want := range map[string]string{
		"--mode": "json", "--session-id": "s-1", "--model": "qntx/anthropic/claude-sonnet-4.6",
		"--thinking": "low", "--append-system-prompt": "You are the ROOT agent.",
		"--session-dir": filepath.Join(home, "pi", "sessions"),
	} {
		if got := after(args, flag); got != want {
			t.Errorf("%s = %q, want %q", flag, got, want)
		}
	}
	if args[len(args)-1] != "how long has the box been up?" || args[len(args)-2] != "--" {
		t.Errorf("what is said is not the last argument after --: %v", args)
	}

	env := linesOf(t, filepath.Join(ran, "env"))
	for _, want := range []string{
		"PI_CODING_AGENT_DIR=" + filepath.Join(home, "pi"), "PI_SKIP_VERSION_CHECK=1", "PI_TELEMETRY=0",
		"QNTX_MCP_BEARER_QNTX=the-agent-token", "QNTX_PI_PROVIDER_KEY=the-agent-token", "GIT_CONFIG_GLOBAL=/agent/gitconfig",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("the environment lacks %s", want)
		}
	}
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if cwd := linesOf(t, filepath.Join(ran, "cwd")); cwd[0] != filepath.Join(resolved, "work") {
		t.Errorf("ran in %s", cwd[0])
	}
}

// Pi reaches the node's MCP and its provider by what the node writes into its
// directory, and neither file carries a secret.
func TestSaidWritesPisMCPAndProviderWithoutTheirSecrets(t *testing.T) {
	binary, _ := standIn(t, answered, 0, "")
	home := t.TempDir()
	if _, err := said(binary, home).Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	var mcp struct {
		Servers map[string]struct {
			URL      string            `json:"url"`
			Headers  map[string]string `json:"headers"`
			Exposure string            `json:"exposure"`
		} `json:"mcpServers"`
	}
	raw, err := os.ReadFile(filepath.Join(home, "pi", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &mcp); err != nil {
		t.Fatal(err)
	}
	qntx := mcp.Servers["qntx"]
	if qntx.URL != "http://127.0.0.1:8770/mcp" || qntx.Headers["Authorization"] != "Bearer ${QNTX_MCP_BEARER_QNTX}" || qntx.Exposure != "direct" {
		t.Fatalf("mcp.json = %s", raw)
	}

	var models struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			API     string `json:"api"`
			APIKey  string `json:"apiKey"`
			Models  []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	raw, err = os.ReadFile(filepath.Join(home, "pi", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &models); err != nil {
		t.Fatal(err)
	}
	provider := models.Providers["qntx"]
	if provider.BaseURL != "http://127.0.0.1:8770/api/openrouter-qntx/v1" || provider.API != "openai-completions" ||
		provider.APIKey != "${QNTX_PI_PROVIDER_KEY}" || len(provider.Models) != 1 || provider.Models[0].ID != "anthropic/claude-sonnet-4.6" {
		t.Fatalf("models.json = %s", raw)
	}
	for _, name := range []string{"mcp.json", "models.json"} {
		body, _ := os.ReadFile(filepath.Join(home, "pi", name))
		if strings.Contains(string(body), "the-agent-token") {
			t.Errorf("%s carries the token itself", name)
		}
	}
}

// An answer the provider failed is an error turn, in the words it failed in.
func TestATurnTheProviderFailedIsAnErrorInItsWords(t *testing.T) {
	failed := `{"type":"session","version":3,"id":"s-1","timestamp":"2026-10-05T20:00:00.000Z","cwd":"/work"}
{"type":"message_end","message":{"role":"assistant","content":[],"provider":"qntx","model":"m","usage":{"cost":{"total":0}},"stopReason":"error","errorMessage":"402 insufficient credits","timestamp":1791230402000}}
{"type":"agent_settled"}
`
	binary, _ := standIn(t, failed, 0, "")
	answer, err := said(binary, t.TempDir()).Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !answer.IsError || answer.Text != "402 insufficient credits" || answer.Stop != "error" {
		t.Fatalf("answer = %+v", answer)
	}
}

// A run that printed no answer is an error carrying what Pi wrote to stderr.
func TestARunWithNoAnswerSaysWhatPiSaid(t *testing.T) {
	binary, _ := standIn(t, "", 1, "No API key found for qntx")
	_, err := said(binary, t.TempDir()).Run(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "No API key found for qntx") {
		t.Fatalf("err = %v", err)
	}
}
