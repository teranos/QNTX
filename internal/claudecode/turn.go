package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/teranos/errors"
)

// "the ROOT agent should live separately in a sense"
//
// "when QNTX has restarted, it should just know somehow that the ROOT agent
// session is already there"

// The files a turn is kept in, under the agent's home. Claude Code prints the
// turn to the stream, and whichever node runs follows it from there.
const (
	turnFile   = "turn.json"
	streamFile = "turn.jsonl"
	stderrFile = "turn.stderr"
	envFile    = "turn.env"
)

// Turn is a turn begun: the session it is in, the process running it, and how
// many of the messages it printed were handed on.
type Turn struct {
	Session string `json:"session"`
	Resumes bool   `json:"resumes"`
	Effort  string `json:"effort"`
	PID     int    `json:"pid"`
	// HandedOn is how many messages were handed on: a node taking the turn up
	// hands on the rest, and nothing twice.
	HandedOn int `json:"handed_on"`

	home string
}

// Apart is how Claude Code is started apart from the node: a unit of its own
// under systemd, which stopping the node's service does not stop, or else a
// process of its own session.
type Apart struct {
	systemd bool
	// run is systemd-run, as the node's PATH finds it.
	run string
	// user is the user's own service manager, for a node not run as root.
	user bool
}

// ApartHere is how a turn is started apart from this node: in a unit of its
// own when the node is a systemd service, which systemd tells by INVOCATION_ID.
func ApartHere() (Apart, error) {
	service := slices.ContainsFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "INVOCATION_ID=") })
	if !service {
		return Apart{}, nil
	}
	run, err := exec.LookPath("systemd-run")
	if err != nil {
		return Apart{}, errors.Wrap(err, "the node is a systemd service and systemd-run is not on its PATH, so a turn would stop with the node")
	}
	me, err := user.Current()
	if err != nil {
		return Apart{}, errors.Wrap(err, "the user the node runs as is unknown, so which service manager starts a turn is too")
	}
	return Apart{systemd: true, run: run, user: me.Username != "root"}, nil
}

// Start begins it apart from whoever starts it, and keeps the turn where a
// node that starts later finds it.
func (s Said) Start(apart Apart) (*Turn, error) {
	args, env, err := s.prepare()
	if err != nil {
		return nil, err
	}
	turn := &Turn{Session: s.Session, Resumes: s.Resumes, Effort: s.Effort, home: s.Home}
	for _, name := range []string{streamFile, stderrFile} {
		if err := os.WriteFile(turn.path(name), nil, 0o600); err != nil {
			return nil, errors.Wrapf(err, "could not begin %s", turn.path(name))
		}
	}
	if apart.systemd {
		turn.PID, err = turn.startUnit(apart, s.Binary, args, env)
	} else {
		turn.PID, err = turn.startProcess(s.Binary, args, env)
	}
	if err != nil {
		return nil, err
	}
	if err := turn.keep(); err != nil {
		return nil, err
	}
	return turn, nil
}

// TurnLeftIn is the turn kept in home, when a node left one there.
func TurnLeftIn(home string) (*Turn, bool, error) {
	kept, err := os.ReadFile(filepath.Join(home, turnFile))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.Wrapf(err, "the turn left in %s did not read", home)
	}
	turn := &Turn{home: home}
	if err := json.Unmarshal(kept, turn); err != nil {
		return nil, false, errors.Wrapf(err, "the turn left in %s is not one: %s", home, kept)
	}
	return turn, true, nil
}

// Follow reads what the turn prints until it ends, handing on each message not
// handed on before. ctx ending stops the reading and not the turn, which stays
// kept for the next node to take up.
func (t *Turn) Follow(ctx context.Context, each func(Message)) (Answer, error) {
	stream, err := os.Open(t.path(streamFile))
	if err != nil {
		return Answer{}, errors.Wrap(err, "the turn's stream did not open")
	}
	defer func() { stream.Close() }()
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return Answer{}, errors.Wrap(err, "the turn's stream cannot be watched")
	}
	defer func() { watcher.Close() }()
	if err := watcher.Add(t.path(streamFile)); err != nil {
		return Answer{}, errors.Wrap(err, "the turn's stream cannot be watched")
	}
	gone := exited(t.PID)

	read := reading{turn: t, each: each}
	for {
		if err := read.on(stream); err != nil {
			return Answer{}, err
		}
		select {
		case <-watcher.Events:
		case watchErr := <-watcher.Errors:
			return Answer{}, errors.Wrap(watchErr, "the turn's stream stopped being watched")
		case exitErr := <-gone:
			if exitErr != nil {
				return Answer{}, errors.Wrapf(exitErr, "whether the turn's process %d ended is unknown", t.PID)
			}
			// What it printed before it ended is read to the end.
			if err := read.on(stream); err != nil {
				return Answer{}, err
			}
			if read.answered {
				return read.answer, nil
			}
			return Answer{}, errors.Newf("Claude Code ended with no result, saying: %s", t.said())
		case <-ctx.Done():
			return Answer{}, errors.Wrap(ctx.Err(), "the turn is left running for the node that starts next")
		}
	}
}

// End lets go of a turn that ended: nothing is left for a node to take up.
func (t *Turn) End() error {
	for _, name := range []string{turnFile, envFile, streamFile, stderrFile} {
		if err := os.Remove(t.path(name)); err != nil && !os.IsNotExist(err) {
			return errors.Wrapf(err, "the ended turn's %s was not removed", name)
		}
	}
	return nil
}

func (t *Turn) path(name string) string { return filepath.Join(t.home, name) }

// keep writes the turn down, as far as it has been handed on.
func (t *Turn) keep() error {
	body, err := json.Marshal(t)
	if err != nil {
		return errors.Wrap(err, "the turn did not marshal")
	}
	partial := t.path(turnFile + ".partial")
	if err := os.WriteFile(partial, body, 0o600); err != nil {
		return errors.Wrapf(err, "could not write %s", partial)
	}
	return errors.Wrapf(os.Rename(partial, t.path(turnFile)), "could not keep the turn at %s", t.path(turnFile))
}

// said is the tail of what Claude Code wrote to stderr.
func (t *Turn) said() string {
	raw, err := os.ReadFile(t.path(stderrFile))
	if err != nil {
		return "(its stderr did not read: " + err.Error() + ")"
	}
	said := strings.TrimSpace(string(raw))
	if len(said) > stderrKept {
		said = said[len(said)-stderrKept:]
	}
	return said
}

// startProcess starts it as a process of its own session, printing to the
// turn's files: the node's process group ending does not end it.
func (t *Turn) startProcess(binary string, args, env []string) (int, error) {
	stdout, err := os.OpenFile(t.path(streamFile), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, errors.Wrap(err, "the turn's stream did not open to be written")
	}
	defer func() { stdout.Close() }()
	stderr, err := os.OpenFile(t.path(stderrFile), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, errors.Wrap(err, "the turn's stderr did not open to be written")
	}
	defer func() { stderr.Close() }()

	cmd := exec.Command(binary, args...)
	cmd.Dir, cmd.Env = filepath.Join(t.home, "work"), env
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, errors.Wrapf(err, "could not start %s", binary)
	}
	// Reaped here while this node runs. A node that starts later is not its
	// parent, and reads its ending off the process itself.
	go func() { cmd.Wait() }()
	return cmd.Process.Pid, nil
}

// startUnit starts it as a transient unit of its own. Its environment is
// handed over in a file only the agent's user reads, never on a command line.
func (t *Turn) startUnit(apart Apart, binary string, args, env []string) (int, error) {
	if err := os.WriteFile(t.path(envFile), []byte(strings.Join(env, "\n")+"\n"), 0o600); err != nil {
		return 0, errors.Wrapf(err, "could not write %s", t.path(envFile))
	}
	unit := "qntx-turn-" + t.Session + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	var manager []string
	if apart.user {
		manager = []string{"--user"}
	}
	run := append(slices.Clone(manager), "--unit", unit, "--collect", "--quiet",
		"--working-directory", filepath.Join(t.home, "work"),
		"--property", "EnvironmentFile="+t.path(envFile),
		"--property", "StandardOutput=append:"+t.path(streamFile),
		"--property", "StandardError=append:"+t.path(stderrFile),
		"--", binary)
	started := exec.Command(apart.run, append(run, args...)...)
	started.Env = managerEnv(apart.user)
	if out, err := started.CombinedOutput(); err != nil {
		return 0, errors.Wrapf(err, "systemd-run did not start the turn, saying: %s", strings.TrimSpace(string(out)))
	}
	show := exec.Command("systemctl", append(slices.Clone(manager), "show", "--property", "MainPID", "--value", unit)...)
	show.Env = managerEnv(apart.user)
	out, err := show.Output()
	if err != nil {
		return 0, errors.Wrapf(err, "the turn's unit %s did not say its process", unit)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, errors.Wrapf(err, "the turn's unit %s said its process is %q", unit, out)
	}
	return pid, nil
}

// managerEnv is the node's environment, with where the user's service manager
// is reached when that is the one asked: a system service is not handed it.
func managerEnv(asUser bool) []string {
	env := os.Environ()
	if asUser {
		env = append(env, "XDG_RUNTIME_DIR=/run/user/"+strconv.Itoa(os.Getuid()))
	}
	return env
}

// reading is a turn's stream read so far: what of it has not ended a line yet,
// and the answer the lines read carry.
type reading struct {
	turn     *Turn
	each     func(Message)
	partial  []byte
	read     int
	answer   Answer
	answered bool
}

// on reads what the stream holds past what was read, a line at a time.
func (r *reading) on(stream io.Reader) error {
	more, err := io.ReadAll(stream)
	if err != nil {
		return errors.Wrap(err, "the turn's stream did not read")
	}
	r.partial = append(r.partial, more...)
	for {
		end := bytes.IndexByte(r.partial, '\n')
		if end < 0 {
			return nil
		}
		line := r.partial[:end]
		r.partial = r.partial[end+1:]
		m, isMessage := messageIn(line)
		if !isMessage {
			continue
		}
		r.read++
		r.answered = r.answer.take(m) || r.answered
		if r.read > r.turn.HandedOn {
			r.each(m)
			r.turn.HandedOn = r.read
			if err := r.turn.keep(); err != nil {
				return err
			}
		}
	}
}

// messageIn is the message a line of the stream is. A line that is not one is
// nothing the stream promised.
func messageIn(line []byte) (Message, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return Message{}, false
	}
	if _, typed := fields["type"]; !typed {
		return Message{}, false
	}
	var m Message
	if err := json.Unmarshal(line, &m); err != nil {
		return Message{}, false
	}
	return m, true
}
