package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/teranos/QNTX/internal/agentenv"
	"github.com/teranos/QNTX/internal/sacred"
	errors "github.com/teranos/sacred-error"
)

// Said is one thing said to a session of Claude Code, and how it is run for it.
// The loop is Claude Code's (ADR-048): this starts the process and reads what
// it prints.
type Said struct {
	// Binary is the pinned Claude Code, as Ensure left it.
	Binary string
	// Home is the agent's own directory: its config under claude, its work
	// under work. Nothing of whoever runs the node is read from or written to.
	Home string
	// Session is the session's UUID, and Resumes whether it exists already.
	Session string
	Resumes bool
	Says    string
	Model   string
	Effort  string
	// Token is the Claude plan token, handed over in the environment.
	Token string
	// System is what this agent additionally is, appended to what Claude Code is.
	System string
	MCP    []MCPServer
	// Mode is the permission mode the session runs in, as Claude Code names
	// them. Whoever says it names it: nothing here picks one.
	Mode string
	// Allow is the tools the session may use without being asked, each by
	// Claude Code's own name for it.
	Allow []string
	// Env is what the session's environment holds beyond what this sets, each
	// NAME=value. What the session runs inherits it.
	Env []string
	// Apart is whether the process runs apart from the node's: in a systemd
	// scope of its own where systemd keeps the node, and in a process group of
	// its own elsewhere, so the node stopping is not the turn stopping.
	Apart bool
	// Started is told the stream the turn prints to, once its process runs:
	// the moment the turn is being done, and where it can be read on from.
	Started func(stream string)
}

// MCPServer is one MCP server the session reaches over HTTP, and the bearer it
// presents there.
type MCPServer struct {
	Name   string
	URL    string
	Bearer string
}

// Message is one line of the stream Claude Code prints: the session starting,
// what the model said or reached for, what a tool answered, and the result.
type Message struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	Timestamp string `json:"timestamp"`
	// On the system message that starts a run.
	Model   string `json:"model"`
	Version string `json:"claude_code_version"`
	// On an assistant or user message.
	Message *struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	// On a stream_event: the API's own event, whole.
	Event json.RawMessage `json:"event"`
	// On the result.
	Result  string  `json:"result"`
	IsError bool    `json:"is_error"`
	CostUSD float64 `json:"total_cost_usd"`
	TookMS  int64   `json:"duration_ms"`
	Denials []struct {
		Tool string `json:"tool_name"`
	} `json:"permission_denials"`
}

// Block is one part of what a message carries: text, a tool reached for, or
// what a tool answered.
type Block struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	// On a thinking block.
	Thinking string `json:"thinking"`
	// On a tool_result: the call it answers, and what the tool said, a string
	// or blocks of text.
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

// Said is a tool_result's content as text: the string it is, or its text
// blocks joined.
func (b Block) Said() string {
	var text string
	if json.Unmarshal(b.Content, &text) == nil {
		return text
	}
	var blocks []Block
	if json.Unmarshal(b.Content, &blocks) != nil {
		return string(b.Content)
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Blocks is the parts of a message. One whose content is plain text has none.
func (m Message) Blocks() []Block {
	if m.Message == nil {
		return nil
	}
	var blocks []Block
	if err := json.Unmarshal(m.Message.Content, &blocks); err != nil {
		return nil
	}
	return blocks
}

// Answer is how a run ended, as Claude Code's own result says it.
type Answer struct {
	Text string
	// IsError is Claude Code reporting that the turn failed, in Text's words,
	// and Subtype which way.
	IsError bool
	Subtype string
	Session string
	Model   string
	Version string
	CostUSD float64
	Took    time.Duration
	// Denied is each tool the session reached for and was not allowed, once.
	Denied []string
}

// stderrKept is how much of what Claude Code wrote to stderr an error carries.
const stderrKept = 4096

// Run says it and reads the stream to its result, handing each message to
// each as it arrives. A run that prints no result is an error carrying what
// Claude Code wrote to stderr.
func (s Said) Run(ctx context.Context, each func(Message)) (Answer, error) {
	if s.Mode == "" {
		return Answer{}, errors.New("no permission mode was named for the session, and none is assumed")
	}
	if s.Started == nil {
		return Answer{}, errors.New("nothing is told where the turn prints once it runs, and nobody is assumed")
	}
	config, work := filepath.Join(s.Home, "claude"), filepath.Join(s.Home, "work")
	for _, dir := range []string{config, work} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Answer{}, errors.Wrapf(err, "could not create %s", dir)
		}
	}

	// Partial messages are the stream_event lines: the words as the model says
	// them, for whoever tails the stream while the turn runs (Tail).
	args := []string{"-p", s.Says, "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--model", s.Model, "--effort", s.Effort, "--permission-mode", s.Mode}
	if len(s.Allow) > 0 {
		args = append(args, "--allowedTools", strings.Join(s.Allow, ","))
	}
	if s.Resumes {
		args = append(args, "--resume", s.Session)
	} else {
		args = append(args, "--session-id", s.Session)
	}
	if s.System != "" {
		args = append(args, "--append-system-prompt", s.System)
	}
	env := append(agentenv.Carried(), "CLAUDE_CONFIG_DIR="+config, "DISABLE_AUTOUPDATER=1")
	env = append(env, s.Env...)
	if s.Token != "" {
		env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+s.Token)
	}
	if len(s.MCP) > 0 {
		path, bearers, err := s.writeMCP()
		if err != nil {
			return Answer{}, err
		}
		args = append(args, "--mcp-config", path, "--strict-mcp-config")
		env = append(env, bearers...)
	}

	turns := turnsDir(s.Home)
	if err := os.MkdirAll(turns, 0o700); err != nil {
		return Answer{}, errors.Wrapf(err, "could not create %s", turns)
	}
	stream := newStream(s.Home, s.Session)
	out, err := os.OpenFile(stream, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Answer{}, errors.Wrapf(err, "could not create the stream %s", stream)
	}
	said, err := os.OpenFile(stream+".err", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Answer{}, errors.WithSecondaryError(errors.Wrapf(err, "could not create %s.err", stream), out.Close())
	}
	// The process holds the stream and its stderr from its start on; the node
	// reads them by name, and closes what it opened once it is handed over.
	handedOver := func() error {
		if err := out.Close(); err != nil {
			return errors.WithSecondaryError(errors.Wrapf(err, "the stream %s did not close", stream), said.Close())
		}
		return errors.Wrapf(said.Close(), "%s.err did not close", stream)
	}

	binary, argv := s.Binary, args
	if s.Apart {
		scope, bus, systemd, err := systemdScope("qntx-turn-" + strings.TrimSuffix(filepath.Base(stream), streamExt))
		if err != nil {
			return Answer{}, err
		}
		if systemd {
			binary, argv = "systemd-run", append(append(scope, s.Binary), args...)
			env = append(env, bus...)
		}
	}
	cmd := exec.Command(binary, argv...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = work, env, out, said
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: s.Apart}
	if err := cmd.Start(); err != nil {
		return Answer{}, errors.WithSecondaryError(errors.Wrapf(err, "could not start %s", binary), handedOver())
	}
	if err := handedOver(); err != nil {
		return Answer{}, err
	}
	if err := os.WriteFile(stream+".pid", []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		return Answer{}, errors.Wrapf(err, "the pid of %s was not kept", stream)
	}
	s.Started(stream)
	var exited atomic.Bool
	waited := make(chan error, 1)
	sacred.Go("waiting on a Claude Code turn's process", func() {
		waited <- cmd.Wait()
		exited.Store(true)
	})
	answer, err := follow(ctx, stream, func() bool { return !exited.Load() }, each)
	// A process of the node's own, not apart, stops when the node does, the
	// way a person stops it, so the session is left as written. One that
	// already printed its result has answered, and its answer is returned.
	if ctxErr := ctx.Err(); ctxErr != nil && err != nil && !s.Apart {
		signalled := cmd.Process.Signal(os.Interrupt)
		return Answer{}, errors.WithSecondaryError(errors.WithSecondaryError(err, signalled), <-waited)
	}
	return answer, err
}

// Holds reports whether Claude Code under home keeps the session: it writes
// each one it ran to projects/<its working directory>/<session>.jsonl.
func Holds(home, session string) bool {
	kept, err := filepath.Glob(filepath.Join(home, "claude", "projects", "*", session+".jsonl"))
	return err == nil && len(kept) > 0
}

// writeMCP writes the servers the session reaches into its home, each bearer
// named by the variable it is read from, and gives those variables set.
func (s Said) writeMCP() (string, []string, error) {
	servers := map[string]any{}
	var bearers []string
	for _, server := range s.MCP {
		entry := map[string]any{"type": "http", "url": server.URL}
		if server.Bearer != "" {
			name := "QNTX_MCP_BEARER_" + strings.ToUpper(server.Name)
			entry["headers"] = map[string]string{"Authorization": "Bearer ${" + name + "}"}
			bearers = append(bearers, name+"="+server.Bearer)
		}
		servers[server.Name] = entry
	}
	body, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		return "", nil, errors.Wrap(err, "the MCP servers did not marshal")
	}
	path := filepath.Join(s.Home, "mcp.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", nil, errors.Wrapf(err, "could not write %s", path)
	}
	return path, bearers, nil
}

// systemdScope is how systemd-run puts a process in a scope of its own named unit, what it needs to
// reach that systemd, and whether systemd keeps this machine's processes: a node whose processes
// systemd keeps stops with every one in its cgroup.
func systemdScope(unit string) ([]string, []string, bool, error) {
	booted, err := os.Stat("/run/systemd/system")
	if os.IsNotExist(err) {
		return []string{}, []string{}, false, nil
	}
	if err != nil {
		return []string{}, []string{}, false, errors.Wrap(err, "whether systemd keeps this machine's processes did not read")
	}
	me, err := user.Current()
	if err != nil {
		return []string{}, []string{}, false, errors.Wrap(err, "who runs the node did not read")
	}
	scope := []string{"--scope", "--quiet", "--collect", "--unit", unit}
	if me.Username == "root" {
		return scope, []string{}, booted.IsDir(), nil
	}
	bus, err := userBus(me.Username, filepath.Join(userRuntimeRoot, me.Uid))
	if err != nil {
		return []string{}, []string{}, false, err
	}
	return append([]string{"--user"}, scope...), bus, booted.IsDir(), nil
}

// userRuntimeRoot is where systemd keeps each user's runtime directory, by uid.
const userRuntimeRoot = "/run/user"

// userBus is how systemd-run --user reaches the user's own systemd from their runtime directory.
// A system service hands a node neither variable: on k, 2026-10-19, every turn of the Node Agent
// ended at systemd-run, which found no bus.
func userBus(name, runtime string) ([]string, error) {
	socket := filepath.Join(runtime, "bus")
	if _, err := os.Stat(socket); err != nil {
		return []string{}, errors.Wrapf(err, "a turn runs apart in %s's own systemd, whose bus %s is not there: it runs only while %s is logged in or lingers (loginctl enable-linger %s)", name, socket, name, name)
	}
	return []string{"XDG_RUNTIME_DIR=" + runtime, "DBUS_SESSION_BUS_ADDRESS=unix:path=" + socket}, nil
}
