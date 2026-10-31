package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/agentenv"
	errors "github.com/teranos/sacred-error"
)

// Said is one turn. An empty Session starts a thread; otherwise Codex resumes it.
// APIKey belongs to this agent. Existing login state lives in Home/codex.
// This foundation runs headlessly; it does not implement approval callbacks.
type Said struct {
	Binary, Home, Session, Says, Model, Sandbox, APIKey string
	System                                              string
	MCPURL, MCPBearer                                   string
	Ground                                              string
	// TrustHooks runs the hooks Home/codex/config.toml names without codex's
	// /hooks trust: they are the node's, written there by the node.
	TrustHooks bool
	// Env is what the agent's turn carries beyond its own home, as its git.
	Env []string
}

// Event preserves complete items for the future QNTX transcript adapter.
// Shape: codex-rs/exec/src/exec_events.rs at the Parity-pinned commit.
type Event struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Item     json.RawMessage `json:"item"`
	Usage    *Usage          `json:"usage"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
	Message string `json:"message"`
}

type Usage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

type Answer struct {
	Text, Session string
	Usage         Usage
	Took          time.Duration
}

// Run requires both a completed turn and a successful process exit.
// A partial answer never turns a failed or interrupted process into success.
func (s Said) Run(ctx context.Context, each func(Event)) (Answer, error) {
	if s.Binary == "" || s.Home == "" || s.Model == "" {
		return Answer{}, errors.New("Codex requires an explicit binary, agent home and model")
	}
	switch s.Sandbox {
	case "read-only", "workspace-write":
	default:
		return Answer{}, errors.Newf("Codex sandbox %q must be read-only or workspace-write", s.Sandbox)
	}
	home, err := filepath.Abs(s.Home)
	if err != nil {
		return Answer{}, errors.Wrapf(err, "could not resolve agent home %s", s.Home)
	}
	config, work := filepath.Join(home, "codex"), filepath.Join(home, "work")
	for _, dir := range []string{config, work} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return Answer{}, errors.Wrapf(err, "could not create %s", dir)
		}
	}
	args := []string{"exec", "--json", "--model", s.Model, "--sandbox", s.Sandbox, "--skip-git-repo-check"}
	if s.System != "" {
		value, err := json.Marshal(s.System)
		if err != nil {
			return Answer{}, errors.Wrap(err, "Codex instructions did not encode")
		}
		args = append(args, "-c", "developer_instructions="+string(value))
	}
	if s.MCPURL != "" {
		value, err := json.Marshal(s.MCPURL)
		if err != nil {
			return Answer{}, errors.Wrap(err, "Codex MCP URL did not encode")
		}
		args = append(args, "-c", "mcp_servers.qntx.url="+string(value), "-c", `mcp_servers.qntx.bearer_token_env_var="QNTX_CODEX_MCP_BEARER"`)
	}
	if s.Ground != "" {
		command := "'" + strings.ReplaceAll(s.Ground, "'", "'\"'\"'") + "'"
		args = append(args, "-c", "features.hooks=true")
		for _, event := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse", "PreCompact", "PostCompact", "Stop", "SessionEnd", "SubagentStart", "SubagentStop"} {
			args = append(args, "-c", "hooks."+event+"=[{hooks=[{type=\"command\",command="+strconv.Quote(command)+"}]}]")
		}
	}

	if s.TrustHooks {
		args = append(args, "--dangerously-bypass-hook-trust")
	}
	if s.Session != "" {
		args = append(args, "resume", "--", s.Session, "-")
	} else {
		args = append(args, "-")
	}
	cmd := exec.CommandContext(ctx, s.Binary, args...)
	cmd.Dir = work
	cmd.Env = append(append(agentenv.Carried(), s.Env...), "HOME="+home, "CODEX_HOME="+config)
	if s.APIKey != "" {
		cmd.Env = append(cmd.Env, "CODEX_API_KEY="+s.APIKey)
	}
	if s.MCPURL != "" {
		cmd.Env = append(cmd.Env, "QNTX_CODEX_MCP_BEARER="+s.MCPBearer)
	}
	if s.Ground != "" {
		cmd.Env = append(cmd.Env, "QNTX_TOKEN="+s.MCPBearer, "PATH="+filepath.Dir(s.Ground)+":"+os.Getenv("PATH"))
	}
	cmd.Stdin = strings.NewReader(s.Says)
	cmd.WaitDelay = time.Second
	var stderr tailBuffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Answer{}, errors.Wrapf(err, "could not read %s stdout", s.Binary)
	}
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return Answer{}, errors.Wrapf(err, "could not start %s", s.Binary)
	}
	// A child retaining stdout must not keep cancellation blocked in Scan.
	streamDone := make(chan struct{})
	defer close(streamDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = stdout.Close()
		case <-streamDone:
		}
	}()
	answer := Answer{}
	completed := false
	var streamErr error
	lines := bufio.NewScanner(stdout)
	lines.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for lines.Scan() {
		line := lines.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			streamErr = errors.Wrap(err, "invalid Codex JSON event")
			break
		}
		if e.Type == "" {
			streamErr = errors.New("Codex JSON event has no type")
			break
		}
		if each != nil {
			each(e)
		}
		switch e.Type {
		case "thread.started":
			if e.ThreadID == "" || (s.Session != "" && e.ThreadID != s.Session) {
				streamErr = errors.Newf("Codex thread %q does not match requested session %q", e.ThreadID, s.Session)
			} else {
				answer.Session = e.ThreadID
			}
		case "item.completed":
			var item struct{ Type, Text string }
			if err := json.Unmarshal(e.Item, &item); err != nil {
				streamErr = errors.Wrap(err, "invalid completed Codex item")
			} else if item.Type == "agent_message" {
				answer.Text = item.Text
			}
		case "turn.completed":
			if e.Usage == nil {
				streamErr = errors.New("completed Codex turn has no usage")
			} else {
				answer.Usage = *e.Usage
				completed = true
			}
		case "turn.failed":
			message := "no error message"
			if e.Error != nil {
				message = e.Error.Message
			}
			streamErr = errors.Newf("Codex turn failed: %s", message)
		case "error":
			streamErr = errors.Newf("Codex stream failed: %s", e.Message)
		}
		if streamErr != nil {
			break
		}
	}
	if err := lines.Err(); err != nil && streamErr == nil {
		streamErr = errors.Wrap(err, "Codex stream was cut short")
	}
	if streamErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	answer.Took = time.Since(started)
	if ctx.Err() != nil {
		return answer, errors.Wrapf(ctx.Err(), "Codex %s stopped", s.Binary)
	}
	if streamErr != nil {
		return answer, streamErr
	}
	if waitErr != nil {
		return answer, errors.Wrapf(waitErr, "Codex %s failed: %s", s.Binary, stderr.String())
	}
	if !completed || answer.Session == "" {
		return answer, errors.Newf("Codex %s ended without a completed turn and thread id: %s", s.Binary, stderr.String())
	}
	return answer, nil
}

// Keep diagnostics bounded while the process is still running.
type tailBuffer struct{ data []byte }

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	const limit = 4096
	if len(p) >= limit {
		b.data = append(b.data[:0], p[len(p)-limit:]...)
	} else {
		if excess := len(b.data) + len(p) - limit; excess > 0 {
			b.data = b.data[excess:]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *tailBuffer) String() string { return strings.TrimSpace(string(b.data)) }
