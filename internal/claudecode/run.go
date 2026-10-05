package claudecode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/teranos/errors"
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
	config, work := filepath.Join(s.Home, "claude"), filepath.Join(s.Home, "work")
	for _, dir := range []string{config, work} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Answer{}, errors.Wrapf(err, "could not create %s", dir)
		}
	}

	args := []string{"-p", s.Says, "--output-format", "stream-json", "--verbose",
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
	env := append(os.Environ(), "CLAUDE_CONFIG_DIR="+config, "DISABLE_AUTOUPDATER=1")
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

	cmd := exec.CommandContext(ctx, s.Binary, args...)
	cmd.Dir, cmd.Env = work, env
	// Stopped the way a person stops it, so the session is left as written.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Answer{}, errors.Wrap(err, "could not read what Claude Code prints")
	}
	if err := cmd.Start(); err != nil {
		return Answer{}, errors.Wrapf(err, "could not start %s", s.Binary)
	}

	var answer Answer
	answered := false
	lines := bufio.NewReader(stdout)
	for {
		line, readErr := lines.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var m Message
			// A line that is not a message is nothing the stream promised.
			if json.Unmarshal(line, &m) == nil && m.Type != "" {
				if m.Type == "system" && m.Subtype == "init" {
					answer.Model, answer.Version = m.Model, m.Version
				}
				if m.Type == "result" {
					answered = true
					answer.Text, answer.IsError, answer.Subtype = m.Result, m.IsError, m.Subtype
					answer.Session, answer.CostUSD = m.SessionID, m.CostUSD
					answer.Took = time.Duration(m.TookMS) * time.Millisecond
					for _, denial := range m.Denials {
						if !slices.Contains(answer.Denied, denial.Tool) {
							answer.Denied = append(answer.Denied, denial.Tool)
						}
					}
				}
				if each != nil {
					each(m)
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return Answer{}, errors.Wrap(readErr, "the stream Claude Code prints was cut short")
			}
			break
		}
	}
	waitErr := cmd.Wait()
	if answered {
		return answer, nil
	}
	said := strings.TrimSpace(stderr.String())
	if len(said) > stderrKept {
		said = said[len(said)-stderrKept:]
	}
	if waitErr != nil {
		return Answer{}, errors.Wrapf(waitErr, "Claude Code ended with no result, saying: %s", said)
	}
	return Answer{}, errors.Newf("Claude Code ended with no result, saying: %s", said)
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
