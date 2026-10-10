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
	"strings"
	"sync"

	"github.com/teranos/QNTX/internal/agentenv"
	"github.com/teranos/errors"
)

// Signing in is Claude Code's own flow, run headless (ADR-048): `claude auth
// login` prints one URL bound to the process that printed it, the person signs
// in there, and the code they are shown is handed back on the process's
// stdin. The node carries the URL out and the code in, and holds nothing:
// what the sign-in leaves is in the agent's own config dir, as on any machine.

// Billing is whose account pays: the Claude subscription, or the Console.
const (
	BillingSubscription = "subscription"
	BillingConsole      = "console"
)

// Billings is every way `claude auth login` bills, as the sigil names them.
var Billings = []string{BillingSubscription, BillingConsole}

// SignIn is one agent's sign-in to Claude Code.
type SignIn struct {
	Binary string
	// Home is the agent's own directory; its config dir is Home/claude.
	Home    string
	Billing string
}

// SigningIn is a sign-in begun and not finished: the process waiting for the
// code, and the URL it printed.
type SigningIn struct {
	URL string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *bytes.Buffer
	done   chan error
	once   sync.Once
}

// environment is what the process starts with: what a process needs on the
// box, and this agent's own config dir.
func environment(home string) []string {
	return append(agentenv.Carried(), "CLAUDE_CONFIG_DIR="+filepath.Join(home, "claude"), "DISABLE_AUTOUPDATER=1")
}

// Begin starts the sign-in and reads until the URL is printed. The process
// stays, waiting for the code, until Finish or Abandon, or until ctx ends.
func (s SignIn) Begin(ctx context.Context) (*SigningIn, error) {
	config := filepath.Join(s.Home, "claude")
	if err := os.MkdirAll(config, 0o700); err != nil {
		return nil, errors.Wrapf(err, "could not create %s", config)
	}
	args := []string{"auth", "login"}
	switch s.Billing {
	case "", BillingSubscription:
		args = append(args, "--claudeai")
	case BillingConsole:
		args = append(args, "--console")
	default:
		return nil, errors.Newf("billing is one of %s, not %q", strings.Join(Billings, ", "), s.Billing)
	}
	cmd := exec.CommandContext(ctx, s.Binary, args...)
	cmd.Dir, cmd.Env = s.Home, environment(s.Home)
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.Wrap(err, "could not open what the code is handed on")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Wrap(err, "could not read what Claude Code prints")
	}
	if err := cmd.Start(); err != nil {
		return nil, errors.Wrapf(err, "could not start %s", s.Binary)
	}
	in := &SigningIn{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: stderr, done: make(chan error, 1)}
	// Claude Code prints the URL on a line of its own, then the prompt for the
	// code with no newline after it: read line by line, and stop at the URL.
	for in.URL == "" {
		line, err := in.stdout.ReadString('\n')
		if at := strings.Index(line, "https://"); at >= 0 {
			in.URL = strings.TrimSpace(line[at:])
		}
		if err != nil {
			break
		}
	}
	if in.URL == "" {
		said := in.said()
		if err := in.Abandon(); err != nil {
			return nil, errors.Wrapf(err, "Claude Code printed no sign-in URL, saying: %s", said)
		}
		return nil, errors.Newf("Claude Code printed no sign-in URL, saying: %s", said)
	}
	return in, nil
}

// Finish hands the code to the process and waits for it to end. The process
// ending well is the sign-in written where Claude Code keeps it.
func (in *SigningIn) Finish(code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("no code was handed over")
	}
	handed := code + "\n"
	written, err := io.WriteString(in.stdin, handed)
	if err != nil {
		return errors.Wrap(err, "the code was not handed to Claude Code")
	}
	if written != len(handed) {
		return errors.Newf("the code was handed to Claude Code in part: %d of %d bytes", written, len(handed))
	}
	if err := in.stdin.Close(); err != nil {
		return errors.Wrap(err, "the code was handed to Claude Code and its stdin did not close")
	}
	// What it prints after the code is read to the end so it can end, and
	// said with its refusal when it refuses.
	printed, err := io.ReadAll(in.stdout)
	if err != nil {
		return errors.Wrap(err, "what Claude Code printed after the code was cut short")
	}
	if err := in.cmd.Wait(); err != nil {
		return errors.Wrapf(err, "Claude Code refused the code, saying: %s", strings.TrimSpace(string(printed)+"\n"+in.said()))
	}
	return nil
}

// Abandon ends a sign-in nobody finished. The process being gone already, or
// ending because it was killed here, is the ending asked for and no error.
func (in *SigningIn) Abandon() error {
	var failed error
	in.once.Do(func() {
		if err := in.stdin.Close(); err != nil {
			failed = errors.Wrap(err, "the sign-in's stdin did not close")
		}
		if in.cmd.Process != nil {
			if err := in.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				failed = errors.Wrap(err, "the sign-in's process was not killed")
			}
		}
		var exit *exec.ExitError
		if err := in.cmd.Wait(); err != nil && !errors.As(err, &exit) {
			failed = errors.Wrap(err, "the sign-in's process was not waited for")
		}
	})
	return failed
}

// said is the tail of what Claude Code wrote to stderr.
func (in *SigningIn) said() string {
	said := strings.TrimSpace(in.stderr.String())
	if len(said) > stderrKept {
		said = said[len(said)-stderrKept:]
	}
	return said
}

// Status is what `claude auth status` says of an agent's config dir.
type Status struct {
	SignedIn    bool   `json:"loggedIn"`
	AuthMethod  string `json:"authMethod"`
	APIProvider string `json:"apiProvider"`
}

// StatusOf asks Claude Code whether the agent under home is signed in. Signed
// out, Claude Code says so and exits 1: that exit with its answer is the
// answer, and an exit 1 with no answer is still a failure.
func StatusOf(ctx context.Context, binary, home string) (Status, error) {
	cmd := exec.CommandContext(ctx, binary, "auth", "status", "--json")
	cmd.Dir, cmd.Env = home, environment(home)
	out, exitErr := cmd.Output()
	if exitErr != nil {
		var exit *exec.ExitError
		if !errors.As(exitErr, &exit) {
			return Status{}, errors.Wrap(exitErr, "claude auth status did not run")
		}
		if exit.ExitCode() != 1 {
			return Status{}, errors.Wrapf(exitErr, "claude auth status failed, saying: %s", strings.TrimSpace(string(out)+"\n"+string(exit.Stderr)))
		}
	}
	var status Status
	if err := json.Unmarshal(out, &status); err != nil {
		return Status{}, errors.Wrapf(err, "claude auth status did not answer JSON: %s", strings.TrimSpace(string(out)))
	}
	if exitErr != nil && status.SignedIn {
		return Status{}, errors.Wrapf(exitErr,"claude auth status exited 1 and said it is signed in: %s", strings.TrimSpace(string(out)))
	}
	return status, nil
}
