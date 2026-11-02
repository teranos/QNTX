//go:build libacp

package agy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/internal/acp"
	"github.com/teranos/QNTX/server/comet"
)

// The pinned agy, run for real over ACP, with its hooks installed as a turn installs them and a
// ground_hook that keeps each event it was run for. Opt in with the binary, and a Gemini key when there
// is one: without QNTX_AGY_KEY the turn runs on a key Gemini refuses, and the refusal must come back.
//
//	QNTX_AGY_BINARY=/absolute/path/to/agy go test -tags libacp ./internal/agy -run TestLivePinnedAgyOverACP -v
func TestLivePinnedAgyOverACP(t *testing.T) {
	binary := os.Getenv("QNTX_AGY_BINARY")
	if binary == "" {
		t.Skip("QNTX_AGY_BINARY names no agy")
	}
	key, valid := os.Getenv("QNTX_AGY_KEY"), true
	if key == "" {
		key, valid = "not-a-gemini-key", false
	}
	home := t.TempDir()
	hooked := filepath.Join(home, "hooked")
	hook := comet.AgentHook(home)
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' \"$2\" >> '"+hooked+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := comet.AgentAgyHooks(home); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var updates []string
	said := Said{Binary: binary, Home: home, Says: "Say only: pong", Model: "gemini-3.8-flash", APIKey: key}
	answer, err := said.Run(ctx, func(u acp.Update) { updates = append(updates, u.Type+" "+u.Text) })
	t.Logf("session %s, %d updates: %q", answer.Session, len(updates), updates)
	if raw, readErr := os.ReadFile(hooked); readErr == nil {
		t.Logf("agy ran the agent's ground for: %s", strings.Fields(string(raw)))
	} else {
		t.Logf("agy ran no hook: %v", readErr)
	}
	if answer.Session == "" {
		t.Fatalf("agy started no session: %v", err)
	}
	if valid {
		if err != nil || !strings.Contains(strings.ToLower(answer.Text), "pong") {
			t.Fatalf("answer %q, err %v", answer.Text, err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), "API key not valid") {
		t.Fatalf("a key Gemini refuses came back as %v", err)
	}
	t.Logf("refused as it should be: %v", err)

	// Another agy, the next turn, resumes the session the first one answered in.
	said.Session = answer.Session
	again, err := said.Run(ctx, nil)
	if again.Session != answer.Session || err == nil || !strings.Contains(err.Error(), "API key not valid") {
		t.Fatalf("the next turn resumed %q of %q: %v", again.Session, answer.Session, err)
	}
	t.Logf("the next turn resumed %s, and was refused as the first", again.Session)
}
