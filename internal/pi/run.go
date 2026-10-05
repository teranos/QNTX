// Package pi runs Pi (earendil-works/pi) for one thing said to a session, and
// reads what its JSON mode prints. The loop is Pi's (ADR-048): this starts the
// process and reads its stream.
package pi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/teranos/errors"
)

// Said is one thing said to a session of Pi, and how it is run for it.
type Said struct {
	// Binary is the pinned Pi.
	Binary string
	// Home is the agent's own directory: Pi's config and sessions under pi,
	// its work under work.
	Home    string
	Session string
	Says    string
	// Model is Pi's provider/id.
	Model    string
	Thinking string
	// System is what this agent additionally is, appended to what Pi is.
	System   string
	MCP      []MCPServer
	Provider *Provider
	// Env is what the session's environment holds beyond what this sets, each
	// NAME=value.
	Env []string
}

// MCPServer is one MCP server the session reaches over HTTP, and the bearer it
// presents there.
type MCPServer struct {
	Name   string
	URL    string
	Bearer string
}

// Provider is an OpenAI-compatible endpoint Pi's model calls go to, and the
// key it presents there.
type Provider struct {
	Name    string
	BaseURL string
	Key     string
	Models  []string
}

// providerKeyVar is where the provider's key is handed over: models.json names
// the variable, and the file carries no secret.
const providerKeyVar = "QNTX_PI_PROVIDER_KEY"

// Event is one record of Pi's JSON stream: the session header, a message
// completed, a tool started or ended, and the run ending.
type Event struct {
	Type string `json:"type"`
	// On the session header.
	ID string `json:"id"`
	// On message_start and message_end.
	Message *Message `json:"message"`
	// On tool_execution_*.
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Args       json.RawMessage `json:"args"`
	IsError    bool            `json:"isError"`
}

// Message is a message as Pi completes it (docs/message-types.md).
type Message struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	Provider     string          `json:"provider"`
	Model        string          `json:"model"`
	StopReason   string          `json:"stopReason"`
	ErrorMessage string          `json:"errorMessage"`
	Timestamp    int64           `json:"timestamp"`
	Usage        *struct {
		Cost struct {
			Total float64 `json:"total"`
		} `json:"cost"`
	} `json:"usage"`
}

// Text is what a message says: its text blocks, joined.
func (m Message) Text() string {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		var plain string
		if json.Unmarshal(m.Content, &plain) == nil {
			return plain
		}
		return ""
	}
	var said []string
	for _, block := range blocks {
		if block.Type == "text" {
			said = append(said, block.Text)
		}
	}
	return strings.Join(said, "\n")
}

// Answer is how a run ended, as Pi's last assistant message says it.
type Answer struct {
	Text string
	// IsError is the provider or Pi ending the turn in error, in Text's words,
	// and Stop the reason it stopped.
	IsError bool
	Stop    string
	Session string
	Model   string
	CostUSD float64
	Took    time.Duration
}

// stderrKept is how much of what Pi wrote to stderr an error carries.
const stderrKept = 4096

// Run says it and reads the stream to its end, handing each record to each as
// it arrives. A run that completed no answer is an error carrying what Pi
// wrote to stderr.
func (s Said) Run(ctx context.Context, each func(Event)) (Answer, error) {
	agentDir, work := filepath.Join(s.Home, "pi"), filepath.Join(s.Home, "work")
	for _, dir := range []string{agentDir, work} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Answer{}, errors.Wrapf(err, "could not create %s", dir)
		}
	}
	env := append(os.Environ(), "PI_CODING_AGENT_DIR="+agentDir, "PI_SKIP_VERSION_CHECK=1", "PI_TELEMETRY=0")
	env = append(env, s.Env...)
	bearers, err := s.writeMCP(agentDir)
	if err != nil {
		return Answer{}, err
	}
	env = append(env, bearers...)
	if s.Provider != nil {
		if err := s.writeProvider(agentDir); err != nil {
			return Answer{}, err
		}
		env = append(env, providerKeyVar+"="+s.Provider.Key)
	}

	args := []string{"--mode", "json", "--session-id", s.Session,
		"--session-dir", filepath.Join(agentDir, "sessions"), "--model", s.Model}
	if s.Thinking != "" {
		args = append(args, "--thinking", s.Thinking)
	}
	if s.System != "" {
		args = append(args, "--append-system-prompt", s.System)
	}
	args = append(args, "--", s.Says)

	started := time.Now()
	cmd := exec.CommandContext(ctx, s.Binary, args...)
	cmd.Dir, cmd.Env = work, env
	// Stopped the way a person stops it, so the session is left as written.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Answer{}, errors.Wrap(err, "could not read what Pi prints")
	}
	if err := cmd.Start(); err != nil {
		return Answer{}, errors.Wrapf(err, "could not start %s", s.Binary)
	}

	answer := Answer{Session: s.Session}
	answered := false
	// Records end at LF and nowhere else: Pi's strings may hold Unicode line
	// separators (docs/json.md).
	lines := bufio.NewReader(stdout)
	for {
		line, readErr := lines.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var e Event
			// A line that is not a record is nothing the stream promised.
			if json.Unmarshal(line, &e) == nil && e.Type != "" {
				if e.Type == "session" && e.ID != "" {
					answer.Session = e.ID
				}
				if e.Type == "message_end" && e.Message != nil && e.Message.Role == "assistant" {
					m := e.Message
					if m.Usage != nil {
						answer.CostUSD += m.Usage.Cost.Total
					}
					answer.Model, answer.Stop = m.Model, m.StopReason
					if m.StopReason != "toolUse" {
						answered = true
						answer.IsError = m.StopReason == "error" || m.StopReason == "aborted"
						answer.Text = m.Text()
						if answer.IsError && m.ErrorMessage != "" {
							answer.Text = m.ErrorMessage
						}
					}
				}
				if each != nil {
					each(e)
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return Answer{}, errors.Wrap(readErr, "the stream Pi prints was cut short")
			}
			break
		}
	}
	waitErr := cmd.Wait()
	answer.Took = time.Since(started)
	if answered {
		return answer, nil
	}
	said := strings.TrimSpace(stderr.String())
	if len(said) > stderrKept {
		said = said[len(said)-stderrKept:]
	}
	if waitErr != nil {
		return Answer{}, errors.Wrapf(waitErr, "Pi ended with no answer, saying: %s", said)
	}
	return Answer{}, errors.Newf("Pi ended with no answer, saying: %s", said)
}

// writeMCP writes the servers the session reaches into Pi's directory, exposed
// direct as Claude Code declares them, each bearer named by the variable it
// is read from. It gives those variables set.
func (s Said) writeMCP(agentDir string) ([]string, error) {
	servers := map[string]any{}
	var bearers []string
	for _, server := range s.MCP {
		entry := map[string]any{"url": server.URL, "exposure": "direct"}
		if server.Bearer != "" {
			name := "QNTX_MCP_BEARER_" + strings.ToUpper(server.Name)
			entry["headers"] = map[string]string{"Authorization": "Bearer ${" + name + "}"}
			bearers = append(bearers, name+"="+server.Bearer)
		}
		servers[server.Name] = entry
	}
	return bearers, writeJSON(filepath.Join(agentDir, "mcp.json"), map[string]any{"mcpServers": servers})
}

// writeProvider writes the provider Pi's model calls go to into its
// models.json, its key named by the variable it is read from.
func (s Said) writeProvider(agentDir string) error {
	models := make([]map[string]string, 0, len(s.Provider.Models))
	for _, id := range s.Provider.Models {
		models = append(models, map[string]string{"id": id})
	}
	return writeJSON(filepath.Join(agentDir, "models.json"), map[string]any{"providers": map[string]any{
		s.Provider.Name: map[string]any{
			"baseUrl": s.Provider.BaseURL,
			"api":     "openai-completions",
			"apiKey":  "${" + providerKeyVar + "}",
			"models":  models,
		},
	}})
}

func writeJSON(path string, v any) error {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errors.Wrapf(err, "%s did not marshal", path)
	}
	return errors.Wrapf(os.WriteFile(path, body, 0o600), "could not write %s", path)
}
