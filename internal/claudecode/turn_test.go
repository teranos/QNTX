package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// halting is a stand-in that prints the stream's first line, then the rest
// once go-ahead exists: a turn still running when whoever followed it stops.
func halting(t *testing.T, first, rest string) (binary, goAhead string) {
	t.Helper()
	dir := t.TempDir()
	goAhead = filepath.Join(dir, "go-ahead")
	for name, body := range map[string]string{"first": first, "rest": rest} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := "#!/bin/sh\n" +
		"cat '" + filepath.Join(dir, "first") + "'\n" +
		"while [ ! -f '" + goAhead + "' ]; do sleep 0.05; done\n" +
		"cat '" + filepath.Join(dir, "rest") + "'\n"
	binary = filepath.Join(dir, "claude")
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, goAhead
}

// "when QNTX has restarted, it should just know somehow that the ROOT agent
// session is already there"
func TestATurnOutlivesTheNodeFollowingItAndIsTakenUp(t *testing.T) {
	first := `{"type":"system","subtype":"init","session_id":"s-1","model":"claude-opus-5-5","claude_code_version":"2.1.289"}
`
	rest := `{"type":"assistant","session_id":"s-1","message":{"content":[{"type":"text","text":"Still here."}]}}
{"type":"result","subtype":"success","session_id":"s-1","is_error":false,"result":"Still here."}
`
	binary, goAhead := halting(t, first, rest)
	home := t.TempDir()
	said := Said{Binary: binary, Home: home, Session: "s-1", Says: "are you there?", Model: "m", Effort: "low", Mode: "dontAsk"}

	turn, err := said.Start(Apart{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// The node following it stops once it has handed on the first message.
	stopping, stop := context.WithCancel(context.Background())
	var seenBefore []string
	if _, err := turn.Follow(stopping, func(m Message) {
		seenBefore = append(seenBefore, m.Type)
		stop()
	}); err == nil {
		t.Fatal("a follower that stopped was answered")
	}
	if !slices.Equal(seenBefore, []string{"system"}) {
		t.Fatalf("before stopping it handed on %v", seenBefore)
	}

	// The next node finds the turn, still running, and follows the rest.
	left, found, err := TurnLeftIn(home)
	if err != nil || !found {
		t.Fatalf("the turn was not left to be taken up: %v, %v", found, err)
	}
	if left.Session != "s-1" || left.HandedOn != 1 {
		t.Errorf("the turn left is %+v", left)
	}
	if err := os.WriteFile(goAhead, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var seenAfter []string
	answer, err := left.Follow(context.Background(), func(m Message) { seenAfter = append(seenAfter, m.Type) })
	if err != nil {
		t.Fatalf("the turn taken up did not answer: %v", err)
	}
	if !slices.Equal(seenAfter, []string{"assistant", "result"}) {
		t.Errorf("taken up, it handed on %v: what was handed on before was handed on again, or the rest was not", seenAfter)
	}
	if answer.Text != "Still here." || answer.Version != "2.1.289" {
		t.Errorf("the answer is %+v", answer)
	}

	if err := left.End(); err != nil {
		t.Fatalf("End: %v", err)
	}
	if _, found, err := TurnLeftIn(home); err != nil || found {
		t.Errorf("an ended turn is still left to be taken up: %v, %v", found, err)
	}
}
