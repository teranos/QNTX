package agy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/agentenv"
	errors "github.com/teranos/sacred-error"
)

// Said is one turn. An empty Session starts a conversation; otherwise agy resumes it.
// APIKey belongs to this agent: agy's Google sign-in lives in a browser and the
// OS keyring, which are the node's, so a turn without its own key is refused.
// This foundation runs headlessly; it does not implement permission callbacks.
type Said struct {
	Binary, Home, Session, Says, Model, APIKey string
	// Env is what the agent's turn carries beyond its own home, as its git.
	Env []string
}

// Event preserves agy's stream for the future QNTX transcript adapter.
// Shape: agy 1.4.4's --output-format stream-json, as it emits it and as
// antigravity.google/docs/cli/headless documents it.
type Event struct {
	Event string `json:"event"`
	// ConversationID is init's own, beside its payload.
	ConversationID string          `json:"conversation_id"`
	Init           json.RawMessage `json:"init"`
	StepUpdate     *Step           `json:"step_update"`
	Result         *Result         `json:"result"`
}

type Step struct {
	ConversationID  string          `json:"conversation_id"`
	StepIndex       int             `json:"step_index"`
	State           string          `json:"state"`
	StepType        string          `json:"step_type"`
	ToolName        string          `json:"tool_name"`
	TextDelta       string          `json:"text_delta"`
	DurationSeconds float64         `json:"duration_seconds"`
	Usage           *Usage          `json:"usage"`
	ToolInfo        json.RawMessage `json:"tool_info"`
	SubagentInfo    json.RawMessage `json:"subagent_info"`
}

type Result struct {
	ConversationID  string  `json:"conversation_id"`
	Status          string  `json:"status"`
	Response        string  `json:"response"`
	Error           string  `json:"error"`
	DurationSeconds float64 `json:"duration_seconds"`
	NumTurns        int     `json:"num_turns"`
	Usage           Usage   `json:"usage"`
	FinishedReason  string  `json:"finished_reason"`
}

type Usage struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ThinkingTokens  int64 `json:"thinking_tokens"`
	CacheReadTokens int64 `json:"cache_read_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

type Answer struct {
	Text, Session string
	Usage         Usage
	Took          time.Duration
}

// Run requires a SUCCESS result for the requested conversation and a successful
// process exit. A partial answer never turns a failed process into success.
func (s Said) Run(ctx context.Context, each func(Event)) (Answer, error) {
	if s.Binary == "" || s.Home == "" || s.Model == "" || s.APIKey == "" {
		return Answer{}, errors.New("agy requires an explicit binary, agent home, model and the agent's own API key")
	}
	home, err := filepath.Abs(s.Home)
	if err != nil {
		return Answer{}, errors.Wrapf(err, "could not resolve agent home %s", s.Home)
	}
	work := filepath.Join(home, "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		return Answer{}, errors.Wrapf(err, "could not create %s", work)
	}
	// GEMINI_API_KEY alone has no effect: agy reads it only under the gemini provider.
	if err := geminiProvider(filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")); err != nil {
		return Answer{}, err
	}
	prompt, err := json.Marshal(map[string]any{"event": "user", "message": map[string]string{"content": s.Says}})
	if err != nil {
		return Answer{}, errors.Wrap(err, "could not encode the prompt")
	}
	// The prompt goes in on stdin; the slash commands agy would read in it are text.
	args := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--model", s.Model, "--disable-slash-commands", "--sandbox"}
	if s.Session != "" {
		args = append(args, "--conversation", s.Session)
	}
	cmd := exec.CommandContext(ctx, s.Binary, args...)
	cmd.Dir = work
	cmd.Env = append(append(agentenv.Carried(), s.Env...), "HOME="+home, updates, "GEMINI_API_KEY="+s.APIKey)
	// One message, then EOF: agy ends the session once the turn completes.
	cmd.Stdin = bytes.NewReader(append(prompt, '\n'))
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
	// agy starts another conversation, with a warning, for one it does not have.
	conversation := func(id string) {
		switch {
		case id == "" || (s.Session != "" && id != s.Session):
			streamErr = errors.Newf("agy conversation %q does not match requested session %q", id, s.Session)
		case answer.Session == "":
			answer.Session = id
		case id != answer.Session:
			streamErr = errors.Newf("agy conversation %q changed to %q mid-turn", answer.Session, id)
		}
	}
	lines := bufio.NewScanner(stdout)
	lines.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for lines.Scan() {
		line := lines.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			streamErr = errors.Wrap(err, "invalid agy JSON event")
			break
		}
		if e.Event == "" {
			streamErr = errors.New("agy JSON event has no event")
			break
		}
		if each != nil {
			each(e)
		}
		switch e.Event {
		case "init":
			conversation(e.ConversationID)
		case "step_update":
			if e.StepUpdate == nil {
				streamErr = errors.New("agy step_update has no step")
			} else {
				conversation(e.StepUpdate.ConversationID)
			}
		case "result":
			if e.Result == nil {
				streamErr = errors.New("agy result has no result")
				break
			}
			conversation(e.Result.ConversationID)
			if streamErr == nil && e.Result.Status != "SUCCESS" {
				streamErr = errors.Newf("agy turn ended %s: %s", e.Result.Status, e.Result.Error)
			}
			if streamErr == nil {
				answer.Text = e.Result.Response
				answer.Usage = e.Result.Usage
				completed = true
			}
		}
		if streamErr != nil {
			break
		}
	}
	if err := lines.Err(); err != nil && streamErr == nil {
		streamErr = errors.Wrap(err, "agy stream was cut short")
	}
	if streamErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	answer.Took = time.Since(started)
	if ctx.Err() != nil {
		return answer, errors.Wrapf(ctx.Err(), "agy %s stopped", s.Binary)
	}
	// agy writes its AGY_ERROR line to stderr; it is the error's detail.
	if streamErr != nil {
		if detail := stderr.String(); detail != "" {
			return answer, errors.Wrapf(streamErr, "%s", detail)
		}
		return answer, streamErr
	}
	if waitErr != nil {
		return answer, errors.Wrapf(waitErr, "agy %s failed: %s", s.Binary, stderr.String())
	}
	if !completed || answer.Session == "" {
		return answer, errors.Newf("agy %s ended without a completed turn and conversation id: %s", s.Binary, stderr.String())
	}
	return answer, nil
}

// geminiProvider sets modelProvider to gemini in agy's settings, keeping the rest.
func geminiProvider(path string) error {
	settings := map[string]any{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &settings); err != nil {
			return errors.Wrapf(err, "%s is not agy settings", path)
		}
		if settings["modelProvider"] == "gemini" {
			return nil
		}
	case !os.IsNotExist(err):
		return errors.Wrapf(err, "could not read %s", path)
	}
	settings["modelProvider"] = "gemini"
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return errors.Wrapf(err, "could not encode %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.Wrapf(err, "could not create %s", filepath.Dir(path))
	}
	f, err := os.CreateTemp(filepath.Dir(path), "settings-*")
	if err != nil {
		return errors.Wrapf(err, "could not stage %s", path)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(out); err != nil {
		return errors.Wrapf(err, "could not write %s", f.Name())
	}
	if err := f.Close(); err != nil {
		return errors.Wrapf(err, "could not close %s", f.Name())
	}
	return errors.Wrapf(os.Rename(f.Name(), path), "could not publish %s", path)
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
