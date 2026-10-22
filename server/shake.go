package server

import (
	"io"
	"os"
	"path/filepath"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// ROOT SHAKE (ADR-054): what say takes to speak to an element, and to hand
// the agent the screenshot of it.

// "its work is the prompt"

// elementParam is the element a call names: its own session with the agent.
func elementParam() *protocol.Param {
	return &protocol.Param{Name: "element", Says: "The id of the element on screen, a canvas element id or a tray element id such as agent-element. Named, the session is the element's own with the agent, and continues; not named, it is the agent's one session."}
}

// screenshotParam is the screenshot a call hands the agent, by its file id.
func screenshotParam() *protocol.Param {
	return &protocol.Param{Name: "file_id", Says: "The id /api/files gave a PNG screenshot of the element. The node puts it where the agent works and the prompt says to read it. Takes element."}
}

// shakeDir is where screenshots are put under the agent's work, which is its
// working directory: Claude Code reads a file there without a permission rule.
const shakeDir = "shake"

// elementNamed is the element a call names, or none. An element is kept under
// the agent's home by its id, so an id that is a path is not an element.
func elementNamed(sent sigil.Sent) (string, *protocol.Refusal) {
	element := sent["element"]
	if element == "" {
		return "", nil
	}
	if element != filepath.Base(element) || element == "." || element == ".." {
		return "", &protocol.Refusal{Why: sigil.Invalid, Param: "element", Says: "an element is named by its id, and " + element + " is a path"}
	}
	return element, nil
}

// screenshotFor puts the screenshot a call names where the agent works,
// work/shake/<id><ext> under its home, and gives where it is. Empty is a call
// that names none.
func (s *QNTXServer) screenshotFor(agent *rootAgent, element string, sent sigil.Sent) (string, *protocol.Refusal) {
	id := sent["file_id"]
	if id == "" {
		return "", nil
	}
	if element == "" {
		return "", &protocol.Refusal{Why: sigil.Missing, Param: "element", Says: "a screenshot is of an element, and none was named"}
	}
	if id != filepath.Base(id) {
		return "", &protocol.Refusal{Why: sigil.Invalid, Param: "file_id", Says: "a file is named by the id /api/files gave it, and " + id + " is a path"}
	}
	stored, found, err := s.storedFile(id)
	if err != nil {
		return "", &protocol.Refusal{Why: sigil.Invalid, Param: "file_id", Says: err.Error()}
	}
	if !found {
		return "", &protocol.Refusal{Why: sigil.NotFound, Param: "file_id", Says: "this node holds no file " + id + " to hand " + agent.called}
	}
	at := filepath.Join(agent.home, "work", shakeDir, filepath.Base(stored))
	if err := copyFile(stored, at); err != nil {
		return "", &protocol.Refusal{Why: sigil.Failed, Says: "the screenshot was not put where " + agent.called + " reads it: " + err.Error()}
	}
	return at, nil
}

// shakePrompt is what the harness is handed when a screenshot is: what was said,
// then where the screenshot of the element is, and to read it.
func shakePrompt(says, element, screenshot string) string {
	if screenshot == "" {
		return says
	}
	return says + "\n\nA screenshot of element " + element + " is at " + screenshot + ". Read it."
}

// copyFile copies from to to, making to's directory.
func copyFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return errors.Wrapf(err, "could not create %s", filepath.Dir(to))
	}
	in, err := os.Open(from)
	if err != nil {
		return errors.Wrapf(err, "could not open %s", from)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.Wrapf(err, "could not create %s", to)
	}
	if _, err := io.Copy(out, in); err != nil {
		return errors.WithSecondaryError(errors.Wrapf(err, "could not copy %s to %s", from, to), out.Close())
	}
	return errors.Wrapf(out.Close(), "%s did not close", to)
}
