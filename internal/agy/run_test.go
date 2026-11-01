package agy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// As agy 1.4.4 streams a turn under --output-format stream-json.
const success = `{"event":"init","conversation_id":"conv-1","init":{"cwd":"/w","tools":["run_command"],"permission_mode":"request-review"}}
{"event":"step_update","step_update":{"conversation_id":"conv-1","step_index":0,"state":"DONE","step_type":"user_input"}}
{"event":"step_update","step_update":{"conversation_id":"conv-1","step_index":1,"state":"DONE","step_type":"agent_response","text_delta":"done"}}
{"event":"result","result":{"conversation_id":"conv-1","status":"SUCCESS","response":"done","duration_seconds":1.5,"num_turns":1,"usage":{"input_tokens":12,"output_tokens":3,"thinking_tokens":2,"cache_read_tokens":0,"total_tokens":17}}}
`

func fake(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "agy")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunStartsAndResumesWithOwnEnvironment(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "node-secret")
	t.Setenv("GOOGLE_API_KEY", "node-secret")
	t.Setenv("AGY_ADC_AUTH", "true")
	t.Setenv("QNTX_TOKEN", "node-secret")
	for _, session := range []string{"", "conv-1"} {
		t.Run("session="+session, func(t *testing.T) {
			home := t.TempDir()
			binary := fake(t, `printf '%s\n' "$@" > args
cat > prompt
printf '%s\n' "$HOME" "$GEMINI_API_KEY" "$AGY_CLI_DISABLE_AUTO_UPDATE" "${GOOGLE_API_KEY-unset}" "${AGY_ADC_AUTH-unset}" "${QNTX_TOKEN-unset}" > env
cat <<'JSON'
`+success+"JSON\n")
			s := Said{Binary: binary, Home: home, Session: session, Says: "/model\nhello", Model: "test-model", APIKey: "agent-key"}
			count := 0
			answer, err := s.Run(context.Background(), func(Event) { count++ })
			if err != nil {
				t.Fatal(err)
			}
			if answer.Text != "done" || answer.Session != "conv-1" || answer.Usage.OutputTokens != 3 || count != 4 {
				t.Fatalf("answer=%+v count=%d", answer, count)
			}
			read := func(name string) string {
				b, err := os.ReadFile(filepath.Join(home, "work", name))
				if err != nil {
					t.Fatal(err)
				}
				return string(b)
			}
			var prompt struct {
				Event   string
				Message struct{ Content string }
			}
			if err := json.Unmarshal([]byte(read("prompt")), &prompt); err != nil || prompt.Event != "user" || prompt.Message.Content != s.Says {
				t.Fatalf("prompt: %q %v", read("prompt"), err)
			}
			args := read("args")
			for _, want := range []string{"--input-format\nstream-json\n", "--output-format\nstream-json\n", "--model\ntest-model\n", "--disable-slash-commands\n", "--sandbox\n"} {
				if !strings.Contains(args, want) {
					t.Fatalf("args lack %q: %s", want, args)
				}
			}
			if strings.Contains(args, "-p\n") || strings.Contains(args, "--print") || strings.Contains(args, "hello") {
				t.Fatalf("prompt on the command line: %s", args)
			}
			if strings.Contains(args, "--conversation\nconv-1\n") != (session != "") {
				t.Fatalf("resume args: %s", args)
			}
			if got := read("env"); got != home+"\nagent-key\ntrue\nunset\nunset\nunset\n" {
				t.Fatalf("environment: %q", got)
			}
		})
	}
}

func TestRunSetsGeminiProviderKeepingSettings(t *testing.T) {
	home := t.TempDir()
	settings := filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"modelProvider":"other","historySize":7}`), 0600); err != nil {
		t.Fatal(err)
	}
	binary := fake(t, "cat >/dev/null\ncat <<'JSON'\n"+success+"JSON\n")
	if _, err := (Said{Binary: binary, Home: home, Model: "m", APIKey: "k"}).Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["modelProvider"] != "gemini" || got["historySize"] != float64(7) {
		t.Fatalf("settings: %s", b)
	}
}

func TestRunRefusesFailureAsSuccess(t *testing.T) {
	failed := `{"event":"init","conversation_id":"conv-1","init":{}}
{"event":"result","result":{"conversation_id":"conv-1","status":"ERROR","response":"","error":"API key not valid"}}`
	for _, tc := range []struct{ name, session, stream, exit string }{
		{"missing result", "", `{"event":"init","conversation_id":"conv-1","init":{}}`, "0"},
		{"malformed", "", "not-json", "0"},
		{"no event", "", `{"init":{}}`, "0"},
		{"failed", "", failed, "3"},
		{"failed with zero exit", "", failed, "0"},
		{"exit after completion", "", success, "3"},
		{"no conversation", "", `{"event":"result","result":{"status":"SUCCESS","response":"x"}}`, "0"},
		// agy warns of a conversation it does not have and starts another.
		{"other conversation", "conv-0", success, "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binary := fake(t, "cat >/dev/null\ncat <<'JSON'\n"+tc.stream+"\nJSON\nexit "+tc.exit+"\n")
			_, err := (Said{Binary: binary, Home: t.TempDir(), Session: tc.session, Model: "test", APIKey: "k"}).Run(context.Background(), nil)
			if err == nil {
				t.Fatal("reported success")
			}
		})
	}
}

func TestRunCarriesAgyErrorFromStderr(t *testing.T) {
	binary := fake(t, `cat >/dev/null
echo 'AGY_ERROR: {"short_error":"API key not valid","error_code":400}' >&2
cat <<'JSON'
{"event":"init","conversation_id":"conv-1","init":{}}
{"event":"result","result":{"conversation_id":"conv-1","status":"ERROR","error":"API key not valid"}}
JSON
exit 3
`)
	_, err := (Said{Binary: binary, Home: t.TempDir(), Model: "test", APIKey: "k"}).Run(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "API key not valid") || !strings.Contains(err.Error(), "AGY_ERROR") {
		t.Fatalf("error: %v", err)
	}
}

func TestRunCancellationReapsProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	binary := fake(t, "exec sleep 30\n")
	start := time.Now()
	_, err := (Said{Binary: binary, Home: t.TempDir(), Model: "test", APIKey: "k"}).Run(ctx, nil)
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("cancel: %v after %s", err, time.Since(start))
	}
}

func TestRunCancellationWithInheritedStdout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	binary := fake(t, "sleep 4\n")
	start := time.Now()
	_, err := (Said{Binary: binary, Home: t.TempDir(), Model: "test", APIKey: "k"}).Run(ctx, nil)
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("inherited pipe blocked cancellation: %v after %s", err, time.Since(start))
	}
}

// Without its own key agy reaches for a Google sign-in, in a browser or the
// node's keyring: neither is the agent's.
func TestRunRequiresExplicitConfiguration(t *testing.T) {
	for _, s := range []Said{{}, {Binary: "unused", Home: t.TempDir(), APIKey: "k"}, {Binary: "unused", Home: t.TempDir(), Model: "test"}} {
		if _, err := s.Run(context.Background(), nil); err == nil {
			t.Fatal("accepted incomplete configuration")
		}
	}
}

func TestDiagnosticsStayBounded(t *testing.T) {
	var b tailBuffer
	for range 100 {
		_, _ = b.Write([]byte(strings.Repeat("a", 1000)))
	}
	_, _ = b.Write([]byte("last"))
	if len(b.data) != 4096 || !strings.HasSuffix(b.String(), "last") {
		t.Fatalf("retained %d diagnostic bytes", len(b.data))
	}
}
