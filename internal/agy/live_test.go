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

// The pinned agy, run for real over ACP as the node at Google, with its hooks installed as a turn
// installs them and a ground_hook that keeps each event it was run for. This spends what the project
// spends. Opt in with the binary, the node's external_account file and its project:
//
//	QNTX_AGY_BINARY=/absolute/path/to/agy QNTX_AGY_CREDENTIALS=/absolute/external-account.json \
//	QNTX_AGY_PROJECT=the-project go test -tags libacp ./internal/agy -run TestLivePinnedAgyOverACP -v
func TestLivePinnedAgyOverACP(t *testing.T) {
	binary, credentials, project := os.Getenv("QNTX_AGY_BINARY"), os.Getenv("QNTX_AGY_CREDENTIALS"), os.Getenv("QNTX_AGY_PROJECT")
	if binary == "" || credentials == "" || project == "" {
		t.Skip("QNTX_AGY_BINARY, QNTX_AGY_CREDENTIALS and QNTX_AGY_PROJECT name no agy to run")
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
	said := Said{Binary: binary, Home: home, Says: "Say only: pong", Model: "gemini-3.8-flash", Google: &Google{Credentials: credentials, Project: project, Location: "global"}}
	answer, err := said.Run(ctx, func(u acp.Update) { updates = append(updates, u.Type+" "+u.Text) })
	t.Logf("session %s, %d updates: %q", answer.Session, len(updates), updates)
	if raw, readErr := os.ReadFile(hooked); readErr == nil {
		t.Logf("agy ran the agent's ground for: %s", strings.Fields(string(raw)))
	} else {
		t.Logf("agy ran no hook: %v", readErr)
	}
	if err != nil || !strings.Contains(strings.ToLower(answer.Text), "pong") {
		t.Fatalf("answer %q, err %v", answer.Text, err)
	}
}
