package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const success = `{"type":"thread.started","thread_id":"thread-1"}
{"type":"item.completed","item":{"id":"msg-1","type":"agent_message","text":"done"}}
{"type":"turn.completed","usage":{"input_tokens":12,"output_tokens":3}}
`

func fake(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunStartsAndResumesWithOwnEnvironment(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "node-secret")
	t.Setenv("CODEX_API_KEY", "node-secret")
	t.Setenv("QNTX_TOKEN", "node-secret")
	for _, session := range []string{"", "thread-1"} {
		t.Run("session="+session, func(t *testing.T) {
			home := t.TempDir()
			binary := fake(t, `printf '%s\n' "$@" > args
cat > prompt
printf '%s\n' "$CODEX_HOME" "$HOME" "${OPENAI_API_KEY-unset}" "$CODEX_API_KEY" "${QNTX_TOKEN-unset}" > env
cat <<'JSON'
`+success+"JSON\n")
			s := Said{Binary: binary, Home: home, Session: session, Says: "--literal\nhello", Model: "test-model", Sandbox: "read-only", APIKey: "agent-key"}
			count := 0
			answer, err := s.Run(context.Background(), func(Event) { count++ })
			if err != nil {
				t.Fatal(err)
			}
			if answer.Text != "done" || answer.Session != "thread-1" || answer.Usage.OutputTokens != 3 || count != 3 {
				t.Fatalf("answer=%+v count=%d", answer, count)
			}
			read := func(name string) string {
				b, err := os.ReadFile(filepath.Join(home, "work", name))
				if err != nil {
					t.Fatal(err)
				}
				return string(b)
			}
			if read("prompt") != s.Says {
				t.Fatal("prompt changed")
			}
			args := read("args")
			if !strings.Contains(args, "exec\n--json\n") || !strings.Contains(args, "--sandbox\nread-only\n") {
				t.Fatalf("args: %s", args)
			}
			if strings.Contains(args, "resume\n") != (session != "") {
				t.Fatalf("resume args: %s", args)
			}
			if got := read("env"); got != filepath.Join(home, "codex")+"\n"+home+"\nunset\nagent-key\nunset\n" {
				t.Fatalf("environment: %q", got)
			}
		})
	}
}

func TestRunRefusesFailureAsSuccess(t *testing.T) {
	for _, tc := range []struct{ name, stream, exit string }{
		{"missing completion", `{"type":"thread.started","thread_id":"t"}`, "0"},
		{"malformed", "not-json", "0"},
		{"failed", `{"type":"turn.failed","error":{"message":"provider failed"}}`, "0"},
		{"stream error", `{"type":"error","message":"disconnected"}`, "0"},
		{"exit after completion", success, "2"},
		{"no thread", `{"type":"turn.completed","usage":{}}`, "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binary := fake(t, "cat >/dev/null\ncat <<'JSON'\n"+tc.stream+"\nJSON\nexit "+tc.exit+"\n")
			_, err := (Said{Binary: binary, Home: t.TempDir(), Model: "test", Sandbox: "read-only"}).Run(context.Background(), nil)
			if err == nil {
				t.Fatal("reported success")
			}
		})
	}
}

func TestRunCancellationReapsProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	binary := fake(t, "exec sleep 30\n")
	start := time.Now()
	_, err := (Said{Binary: binary, Home: t.TempDir(), Model: "test", Sandbox: "read-only"}).Run(ctx, nil)
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("cancel: %v after %s", err, time.Since(start))
	}
}

func TestRunRequiresExplicitConfiguration(t *testing.T) {
	for _, s := range []Said{{}, {Binary: "unused", Home: t.TempDir(), Model: "test"}, {Binary: "unused", Home: t.TempDir(), Model: "test", Sandbox: "unknown"}} {
		if _, err := s.Run(context.Background(), nil); err == nil {
			t.Fatal("accepted incomplete configuration")
		}
	}
}

func TestRunCancellationWithInheritedStdout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	binary := fake(t, "sleep 4\n")
	start := time.Now()
	_, err := (Said{Binary: binary, Home: t.TempDir(), Model: "test", Sandbox: "read-only"}).Run(ctx, nil)
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("inherited pipe blocked cancellation: %v after %s", err, time.Since(start))
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
