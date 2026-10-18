package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	errors "github.com/teranos/sacred-error"
)

// A turn's stream is a file under the agent's home, not a pipe to the node: the
// node hosts the process, it starts it, resumes it and stops it (ADR-048), and
// a node that stops is not the turn stopping. What Claude Code printed waits in
// the file for whichever node process reads it next.

// streamExt is what a turn's stream is named with, beside its .pid, .read and
// .err.
const streamExt = ".stream"

// followEvery is how often a stream that has nothing new is read again.
const followEvery = 200 * time.Millisecond

// Turn is a turn whose stream is still to be read to its end.
type Turn struct {
	// Session is the session it runs in.
	Session string
	// Stream is the file Claude Code prints to.
	Stream string
	// Started is when the node started it.
	Started time.Time
}

// turnsDir is where a home keeps the streams of its turns.
func turnsDir(home string) string { return filepath.Join(home, "turns") }

// newStream names the stream of a turn in session starting now.
func newStream(home, session string) string {
	return filepath.Join(turnsDir(home), session+"."+strconv.FormatInt(time.Now().UnixNano(), 10)+streamExt)
}

// Pending is every turn under home whose stream was not read to its end, oldest
// first: a node that stopped while they ran left them.
func Pending(home string) ([]Turn, error) {
	streams, err := filepath.Glob(filepath.Join(turnsDir(home), "*"+streamExt))
	if err != nil {
		return nil, errors.Wrapf(err, "the turns under %s did not list", home)
	}
	slices.Sort(streams)
	turns := make([]Turn, 0, len(streams))
	for _, stream := range streams {
		name := strings.TrimSuffix(filepath.Base(stream), streamExt)
		session, started, cut := strings.Cut(name, ".")
		if !cut {
			return nil, errors.Newf("%s names no session and start", stream)
		}
		nanos, err := strconv.ParseInt(started, 10, 64)
		if err != nil {
			return nil, errors.Wrapf(err, "%s names no start", stream)
		}
		turns = append(turns, Turn{Session: session, Stream: stream, Started: time.Unix(0, nanos)})
	}
	return turns, nil
}

// Follow reads the turn's stream from where it was last read to its result,
// handing each message to each. A process that is gone with no result is an
// error carrying what it wrote to stderr. A ctx done stops the reading and not
// the process: what it prints waits for the next Follow.
func (t Turn) Follow(ctx context.Context, each func(Message)) (Answer, error) {
	return follow(ctx, t.Stream, func() bool { return pidRuns(t.Stream) }, each)
}

// pidRuns is whether the process the stream names is still there.
func pidRuns(stream string) bool {
	held, err := os.ReadFile(stream + ".pid")
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(held)))
	if err != nil {
		return false
	}
	err = syscall.Kill(pid, syscall.Signal(0))
	return err == nil
}

// follow reads stream from its .read mark until a result, or until runs says
// the process is gone and nothing more is in it.
func follow(ctx context.Context, stream string, runs func() bool, each func(Message)) (answered Answer, err error) {
	file, err := os.Open(stream)
	if err != nil {
		return Answer{}, errors.Wrapf(err, "the stream %s did not open", stream)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.WithSecondaryError(errors.Wrapf(closeErr, "the stream %s did not close", stream), err)
		}
	}()
	read, err := readMark(stream)
	if err != nil {
		return Answer{}, err
	}
	at, err := file.Seek(read, io.SeekStart)
	if err != nil {
		return Answer{}, errors.Wrapf(err, "the stream %s did not seek to %d, and is at %d", stream, read, at)
	}

	var answer Answer
	resulted, gone := false, false
	lines := bufio.NewReader(file)
	var partial []byte
	for !resulted {
		line, readErr := lines.ReadBytes('\n')
		partial = append(partial, line...)
		if readErr == nil {
			var m Message
			// A line that is not a message is nothing the stream promised.
			if unmarshalErr := json.Unmarshal(partial, &m); unmarshalErr == nil {
				resulted = resulted || answerFrom(&answer, m)
				each(m)
			}
			read += int64(len(partial))
			partial = []byte{}
			if err := os.WriteFile(stream+".read", []byte(strconv.FormatInt(read, 10)), 0o600); err != nil {
				return Answer{}, errors.Wrapf(err, "how far %s was read was not kept", stream)
			}
			continue
		}
		if !errors.Is(readErr, io.EOF) {
			return Answer{}, errors.Wrapf(readErr, "the stream %s did not read", stream)
		}
		if gone {
			return Answer{}, endedUnanswered(stream)
		}
		// Gone is read once more: what it printed before it went is in the file.
		gone = !runs()
		select {
		case <-ctx.Done():
			return Answer{}, errors.Wrapf(ctx.Err(), "the stream %s was left to be read on", stream)
		case <-time.After(followEvery):
		}
	}
	return answer, ended(stream)
}

// answerFrom takes into answer what m says of how the run went, and whether m
// is its result.
func answerFrom(answer *Answer, m Message) bool {
	switch m.Type {
	case "system":
		if m.Subtype == "init" {
			answer.Model, answer.Version = m.Model, m.Version
		}
	case "result":
		answer.Text, answer.IsError, answer.Subtype = m.Result, m.IsError, m.Subtype
		answer.Session, answer.CostUSD = m.SessionID, m.CostUSD
		answer.Took = time.Duration(m.TookMS) * time.Millisecond
		for _, denial := range m.Denials {
			if !slices.Contains(answer.Denied, denial.Tool) {
				answer.Denied = append(answer.Denied, denial.Tool)
			}
		}
		return true
	}
	return false
}

// readMark is how far stream was read, and the start when it was not read yet.
func readMark(stream string) (int64, error) {
	held, err := os.ReadFile(stream + ".read")
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, errors.Wrapf(err, "how far %s was read did not read", stream)
	}
	read, err := strconv.ParseInt(strings.TrimSpace(string(held)), 10, 64)
	return read, errors.Wrapf(err, "how far %s was read is not a number", stream)
}

// endedUnanswered is a process gone with no result, in its stderr's words.
func endedUnanswered(stream string) error {
	said, err := os.ReadFile(stream + ".err")
	if err != nil {
		return errors.Wrapf(err, "Claude Code ended with no result, and what it said on stderr did not read")
	}
	kept := strings.TrimSpace(string(said))
	if len(kept) > stderrKept {
		kept = kept[len(kept)-stderrKept:]
	}
	return errors.WithSecondaryError(errors.Newf("Claude Code ended with no result, saying: %s", kept), ended(stream))
}

// ended removes a stream read to its end, and what was kept beside it.
func ended(stream string) error {
	for _, path := range []string{stream, stream + ".read", stream + ".pid", stream + ".err"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return errors.Wrapf(err, "%s was read to its end and was not removed", path)
		}
	}
	return nil
}
