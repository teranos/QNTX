package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// standIn is a program that stands in for Claude Code: it writes down how it
// was run, then prints the stream it was given and exits as told.
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
	binary = filepath.Join(dir, "claude")
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

const answered = `{"type":"system","subtype":"init","session_id":"s-1","model":"claude-opus-5-5","claude_code_version":"2.1.289"}
{"type":"assistant","session_id":"s-1","timestamp":"2026-10-04T20:21:34.077Z","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"uname -s","description":"Print kernel name"}}]}}
{"type":"rate_limit_event","session_id":"s-1"}
{"type":"user","session_id":"s-1","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"Darwin","is_error":false}]}}
{"type":"assistant","session_id":"s-1","timestamp":"2026-10-04T20:21:36.083Z","message":{"content":[{"type":"text","text":"It printed Darwin."}]}}
{"type":"result","subtype":"success","session_id":"s-1","is_error":false,"result":"It printed Darwin.","total_cost_usd":0.05,"duration_ms":2592,"num_turns":2}
`

func TestSaidStartsASessionAndAnswers(t *testing.T) {
	binary, ran := standIn(t, answered, 0, "")
	home := t.TempDir()
	said := Said{
		Binary: binary, Home: home, Session: "s-1", Says: "what does uname -s print?",
		Model: "claude-opus-5-5", Effort: "low", Mode: "dontAsk", Token: "the-plan-token", System: "You are the ROOT agent.",
	}

	var seen []string
	answer, err := said.Run(context.Background(), func(m Message) { seen = append(seen, m.Type) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer.Text != "It printed Darwin." || answer.IsError {
		t.Errorf("the answer is %+v", answer)
	}
	if answer.Session != "s-1" || answer.Model != "claude-opus-5-5" || answer.Version != "2.1.289" {
		t.Errorf("the answer names session %q, model %q, version %q", answer.Session, answer.Model, answer.Version)
	}
	if !slices.Equal(seen, []string{"system", "assistant", "rate_limit_event", "user", "assistant", "result"}) {
		t.Errorf("the stream was read as %v", seen)
	}

	args := linesOf(t, filepath.Join(ran, "args"))
	for _, pair := range [][2]string{
		{"-p", "what does uname -s print?"},
		{"--session-id", "s-1"},
		{"--model", "claude-opus-5-5"},
		{"--effort", "low"},
		{"--output-format", "stream-json"},
		{"--permission-mode", "dontAsk"},
		{"--append-system-prompt", "You are the ROOT agent."},
	} {
		at := slices.Index(args, pair[0])
		if at < 0 || at+1 >= len(args) || args[at+1] != pair[1] {
			t.Errorf("it was not run with %s %q: %v", pair[0], pair[1], args)
		}
	}
	if !slices.Contains(args, "--verbose") {
		t.Errorf("a stream is printed only with --verbose: %v", args)
	}
	if slices.Contains(args, "--resume") {
		t.Errorf("a session that starts here was resumed: %v", args)
	}
	if slices.Contains(args, "--allowedTools") {
		t.Errorf("a session that was allowed nothing was run with tools allowed: %v", args)
	}

	env := linesOf(t, filepath.Join(ran, "env"))
	for _, want := range []string{
		"CLAUDE_CONFIG_DIR=" + filepath.Join(home, "claude"),
		"CLAUDE_CODE_OAUTH_TOKEN=the-plan-token",
		"DISABLE_AUTOUPDATER=1",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("its environment lacks %s", want)
		}
	}
	cwd := linesOf(t, filepath.Join(ran, "cwd"))[0]
	if resolved, err := filepath.EvalSymlinks(filepath.Join(home, "work")); err != nil || cwd != resolved {
		t.Errorf("it ran in %s, want %s (%v)", cwd, resolved, err)
	}
}

// Nobody is there to be asked, so what a session may do is what it was allowed
// by name, and what it reached for beyond that is said with the answer.
func TestASessionMayDoWhatItWasAllowedAndTheRestIsSaid(t *testing.T) {
	stream := `{"type":"system","subtype":"init","session_id":"s-1","model":"m"}
{"type":"result","subtype":"success","session_id":"s-1","is_error":false,"result":"I could not write the file.","permission_denials":[{"tool_name":"Write","tool_use_id":"toolu_1","tool_input":{"file_path":"/etc/x"}},{"tool_name":"Write","tool_use_id":"toolu_2","tool_input":{}},{"tool_name":"WebFetch","tool_use_id":"toolu_3","tool_input":{}}]}
`
	binary, ran := standIn(t, stream, 0, "")
	said := Said{Binary: binary, Home: t.TempDir(), Session: "s-1", Says: "write it", Model: "m", Effort: "low", Mode: "dontAsk",
		Allow: []string{"Bash", "Read", "mcp__qntx"}}
	answer, err := said.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	args := linesOf(t, filepath.Join(ran, "args"))
	at := slices.Index(args, "--allowedTools")
	if at < 0 || args[at+1] != "Bash,Read,mcp__qntx" {
		t.Errorf("it was not allowed what it was given by name: %v", args)
	}
	if !slices.Equal(answer.Denied, []string{"Write", "WebFetch"}) {
		t.Errorf("what it was refused is %v, want each tool once", answer.Denied)
	}
}

// Which permission mode a session runs in is its caller's to name, and it is
// handed to Claude Code as named. One that names none is not run.
func TestTheModeNamedIsTheModeRunIn(t *testing.T) {
	binary, ran := standIn(t, answered, 0, "")
	said := Said{Binary: binary, Home: t.TempDir(), Session: "s-1", Says: "hello", Model: "m", Effort: "low", Mode: "plan"}
	if _, err := said.Run(context.Background(), nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	args := linesOf(t, filepath.Join(ran, "args"))
	at := slices.Index(args, "--permission-mode")
	if at < 0 || args[at+1] != "plan" {
		t.Errorf("it was not run in the mode named: %v", args)
	}

	said.Mode = ""
	if _, err := said.Run(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "permission mode") {
		t.Errorf("a session that names no permission mode was run: %v", err)
	}
}

func TestSaidResumesASessionThatExists(t *testing.T) {
	binary, ran := standIn(t, answered, 0, "")
	said := Said{Binary: binary, Home: t.TempDir(), Session: "s-1", Resumes: true, Says: "and again", Model: "m", Effort: "low", Mode: "dontAsk"}
	if _, err := said.Run(context.Background(), nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	args := linesOf(t, filepath.Join(ran, "args"))
	at := slices.Index(args, "--resume")
	if at < 0 || args[at+1] != "s-1" {
		t.Errorf("it was not resumed: %v", args)
	}
	if slices.Contains(args, "--session-id") {
		t.Errorf("a resumed session was given an id to start under: %v", args)
	}
}

func TestARunWithNoResultSaysWhatClaudeCodeSaid(t *testing.T) {
	binary, _ := standIn(t, "", 1, "No conversation found with session ID: s-1")
	said := Said{Binary: binary, Home: t.TempDir(), Session: "s-1", Resumes: true, Says: "hello", Model: "m", Effort: "low", Mode: "dontAsk"}
	_, err := said.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("a run that printed no result was taken as answered")
	}
	if !strings.Contains(err.Error(), "No conversation found with session ID: s-1") {
		t.Errorf("the error does not carry what Claude Code said: %v", err)
	}
}

func TestAnErrorResultIsAnAnswerThatSaysSo(t *testing.T) {
	stream := `{"type":"system","subtype":"init","session_id":"s-1","model":"m"}
{"type":"result","subtype":"error_during_execution","session_id":"s-1","is_error":true,"result":"API Error: 529 overloaded"}
`
	binary, _ := standIn(t, stream, 1, "")
	said := Said{Binary: binary, Home: t.TempDir(), Session: "s-1", Says: "hello", Model: "m", Effort: "low", Mode: "dontAsk"}
	answer, err := said.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("an error Claude Code reported as its result is an answer, not a failed run: %v", err)
	}
	if !answer.IsError || answer.Text != "API Error: 529 overloaded" || answer.Subtype != "error_during_execution" {
		t.Errorf("the answer is %+v", answer)
	}
}

func TestAnMCPServerIsNamedWithoutItsSecret(t *testing.T) {
	binary, ran := standIn(t, answered, 0, "")
	home := t.TempDir()
	said := Said{
		Binary: binary, Home: home, Session: "s-1", Says: "hello", Model: "m", Effort: "low", Mode: "dontAsk",
		MCP: []MCPServer{{Name: "qntx", URL: "http://127.0.0.1:8770/mcp", Bearer: "qntx_secret"}},
	}
	if _, err := said.Run(context.Background(), nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	args := linesOf(t, filepath.Join(ran, "args"))
	at := slices.Index(args, "--mcp-config")
	if at < 0 || !slices.Contains(args, "--strict-mcp-config") {
		t.Fatalf("it was not given its MCP servers and those alone: %v", args)
	}
	written, err := os.ReadFile(args[at+1])
	if err != nil {
		t.Fatalf("the MCP config did not read: %v", err)
	}
	if strings.Contains(string(written), "qntx_secret") {
		t.Errorf("the MCP config holds the bearer itself:\n%s", written)
	}
	if !strings.Contains(string(written), "http://127.0.0.1:8770/mcp") || !strings.Contains(string(written), "${QNTX_MCP_BEARER_QNTX}") {
		t.Errorf("the MCP config does not name the server and where its bearer is read from:\n%s", written)
	}
	if !slices.Contains(linesOf(t, filepath.Join(ran, "env")), "QNTX_MCP_BEARER_QNTX=qntx_secret") {
		t.Errorf("the bearer was not handed over in the environment")
	}
}

func TestALineLongerThanABufferIsStillRead(t *testing.T) {
	long := strings.Repeat("x", 300_000)
	stream := `{"type":"assistant","session_id":"s-1","message":{"content":[{"type":"text","text":"` + long + `"}]}}
{"type":"result","subtype":"success","session_id":"s-1","is_error":false,"result":"done"}
`
	binary, _ := standIn(t, stream, 0, "")
	said := Said{Binary: binary, Home: t.TempDir(), Session: "s-1", Says: "hello", Model: "m", Effort: "low", Mode: "dontAsk"}
	var texts []string
	answer, err := said.Run(context.Background(), func(m Message) {
		for _, block := range m.Blocks() {
			texts = append(texts, block.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if answer.Text != "done" || len(texts) != 1 || len(texts[0]) != len(long) {
		t.Errorf("the long line was not read whole: answer %q, %d texts", answer.Text, len(texts))
	}
}
