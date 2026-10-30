package claudecode

import (
	"encoding/json"
	"os"
	"strconv"

	errors "github.com/teranos/sacred-error"
)

// A Mirror is a turn's stream printed by the node itself, for a turn that runs
// where no process of the node's prints one: an agent at Anthropic, whose
// events the node reads off the Console (server/anthropic_managed.go).
type Mirror struct {
	// Stream is the file, named as a turn's stream is, and read by Tail as one.
	Stream string
	file   *os.File
}

// OpenMirror opens the stream of a turn in session under home, held by this
// process, so a tail reads it while the turn runs.
func OpenMirror(home, session string) (*Mirror, error) {
	turns := turnsDir(home)
	if err := os.MkdirAll(turns, 0o700); err != nil {
		return nil, errors.Wrapf(err, "could not create %s", turns)
	}
	stream := newStream(home, session)
	file, err := os.OpenFile(stream, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, errors.Wrapf(err, "could not create the stream %s", stream)
	}
	// The node is the process printing it: a tail asks whether it still runs.
	if err := os.WriteFile(stream+".pid", []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return nil, errors.WithSecondaryError(errors.Wrapf(err, "the pid of %s was not kept", stream), file.Close())
	}
	return &Mirror{Stream: stream, file: file}, nil
}

// Print writes one message as a line of the stream.
func (m *Mirror) Print(message Message) error {
	line, err := json.Marshal(message)
	if err != nil {
		return errors.Wrap(err, "the message did not encode")
	}
	if _, err := m.file.Write(append(line, '\n')); err != nil {
		return errors.Wrapf(err, "the stream %s was not written", m.Stream)
	}
	return nil
}

// End closes the stream and removes it with what was kept beside it, as a
// stream read to its end is removed.
func (m *Mirror) End() error {
	return errors.WithSecondaryError(ended(m.Stream), m.file.Close())
}

// TextMessage is an assistant message of one text block, as a Claude Code
// stream prints one.
func TextMessage(text string) Message {
	content, _ := json.Marshal([]map[string]string{{"type": "text", "text": text}})
	return Message{Type: "assistant", Message: &struct {
		Content json.RawMessage `json:"content"`
	}{Content: content}}
}
