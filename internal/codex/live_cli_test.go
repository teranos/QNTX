package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The actual pinned CLI, with local model responses and a recording Ground
// stand-in. It uses no real model credential or external inference service.
func TestPinnedCLIWithLocalModelAndHooks(t *testing.T) {
	binary := os.Getenv("QNTX_CODEX_BINARY")
	if binary == "" {
		t.Skip("set QNTX_CODEX_BINARY to the verified pinned executable")
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("model request: %s %s", r.Method, r.URL.Path)
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		events := []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "resp_test"}},
			{"type": "response.output_item.done", "item": map[string]any{"id": "msg_test", "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "local answer"}}}},
			{"type": "response.completed", "response": map[string]any{"id": "resp_test", "status": "completed", "output": []any{}, "usage": map[string]any{"input_tokens": 2, "output_tokens": 3, "input_tokens_details": map[string]any{"cached_tokens": 0}}}},
		}
		for _, event := range events {
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
	}))
	defer model.Close()
	home, err := os.MkdirTemp(".", "codex-live-test-")
	if err != nil {
		t.Fatal(err)
	}
	home, err = filepath.Abs(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.RemoveAll(home)
	})
	config := filepath.Join(home, "codex")
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf("model_provider = \"localtest\"\n[model_providers.localtest]\nname = \"localtest\"\nbase_url = %q\nwire_api = \"responses\"\n", model.URL)
	if err := os.WriteFile(filepath.Join(config, "config.toml"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("home: %s", home)
	ground := filepath.Join(home, "ground")
	if err := os.WriteFile(ground, []byte("#!/bin/sh\ncat >> \"$HOME/hooks-seen\"\nprintf '\\n' >> \"$HOME/hooks-seen\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	s := Said{Binary: binary, Home: home, Model: "local-test-model", Sandbox: "read-only", Says: "answer", Ground: ground, TrustHooks: true, System: "Reply briefly."}
	first, err := s.Run(ctx, func(e Event) { t.Logf("event: %+v", e) })
	if err != nil {
		t.Fatal(err)
	}
	if first.Text != "local answer" || first.Session == "" {
		t.Fatalf("first: %+v", first)
	}
	s.Session = first.Session
	second, err := s.Run(ctx, func(e Event) { t.Logf("event: %+v", e) })
	if err != nil {
		t.Fatal(err)
	}
	if second.Session != first.Session || second.Text != "local answer" {
		t.Fatalf("second: %+v", second)
	}
	hooks, err := os.ReadFile(filepath.Join(home, "hooks-seen"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hooks), "SessionStart") || !strings.Contains(string(hooks), "UserPromptSubmit") || !strings.Contains(string(hooks), "Stop") {
		t.Fatalf("hooks not executed: %s", hooks)
	}
}
