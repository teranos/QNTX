// 「[Opus 5.5] Package acptest stands in for an ACP agent in a test: a shell script that answers what a client asks」
// 「[Opus 5.5] by its method, and keeps each run's arguments, environment and every line it was sent.」
package acptest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 「[Opus 5.5] Script is what the stand-in answers. Updates are SessionUpdate objects, told before the prompt's」
// 「[Opus 5.5] answer; Log is written to the file --log-file names, when it names one.」
type Script struct {
	Session string
	Updates []string
	Stop    string
	Log     string
}

// 「[Opus 5.5] StandIn writes the stand-in. Run n keeps its args, env and input under ran/n.」
func StandIn(t testing.TB, s Script) (binary, ran string) {
	t.Helper()
	dir := t.TempDir()
	ran = filepath.Join(dir, "ran")
	if err := os.MkdirAll(ran, 0o755); err != nil {
		t.Fatal(err)
	}
	if s.Session == "" {
		s.Session = "session-1"
	}
	if s.Stop == "" {
		s.Stop = "end_turn"
	}
	var told strings.Builder
	for _, u := range s.Updates {
		told.WriteString(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"` + s.Session + `","update":` + u + "}}\n")
	}
	files := map[string]string{"updates": told.String(), "log": s.Log}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	binary = filepath.Join(dir, "agent")
	script := `#!/bin/sh
ran='` + ran + `'
n=$(ls "$ran" | wc -l | tr -d ' ')
mkdir "$ran/$n"
log=
prev=
for arg in "$@"; do
  printf '%s\n' "$arg" >> "$ran/$n/args"
  [ "$prev" = --log-file ] && log=$arg
  prev=$arg
done
env > "$ran/$n/env"
[ -n "$log" ] && cat '` + filepath.Join(dir, "log") + `' > "$log"
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$ran/$n/input"
  id=$(printf '%s' "$line" | sed -n 's/^{"jsonrpc":"2.0","id":\([0-9]*\),.*/\1/p')
  [ -z "$id" ] && continue
  case "$line" in
    *'"method":"initialize"'*) r='{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"sessionCapabilities":{"resume":{}}},"agentInfo":{"name":"stand-in","version":"1"}}' ;;
    *'"method":"session/new"'*) r='{"sessionId":"` + s.Session + `"}' ;;
    *'"method":"session/prompt"'*) cat '` + filepath.Join(dir, "updates") + `'; r='{"stopReason":"` + s.Stop + `"}' ;;
    *) r='{}' ;;
  esac
  printf '{"jsonrpc":"2.0","id":%s,"result":%s}\n' "$id" "$r"
done
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return binary, ran
}

// 「[Opus 5.5] Read is one file a run kept, by its lines.」
func Read(t testing.TB, ran, run, what string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ran, run, what))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}
