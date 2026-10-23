package server

import (
	"io"
	"os"
	"path/filepath"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// ROOT SHAKE (ADR-054): the screenshot say hands an agent, by its file id.

// "its work is the prompt"

// screenshotParam is the screenshot a call hands the agent, by its file id.
func screenshotParam() *protocol.Param {
	return &protocol.Param{Name: "file_id", Says: "The id /api/files gave a picture: the screenshot of the element, or a picture attached in chat. The node puts it where the agent works and the prompt says to read it."}
}

// shakeDir is where screenshots are put under the agent's work, which is its
// working directory: Claude Code reads a file there without a permission rule.
const shakeDir = "shake"

// screenshotFor puts the screenshot a call names where the agent works,
// work/shake/<id><ext> under its home, and gives where it is. Empty is a call
// that names none.
func (s *QNTXServer) screenshotFor(agent *rootAgent, sent sigil.Sent) (string, *protocol.Refusal) {
	id := sent["file_id"]
	if id == "" {
		return "", nil
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

// "say being able to carry an image sounds usefull to me, and being able to attach an imagine whne in a mobile chat"

// shakePrompt is what the harness is handed when a picture is: what was said,
// then where the picture is, of the element when one is named, and to read it.
func shakePrompt(says, element, screenshot string) string {
	switch {
	case screenshot == "":
		return says
	case element == "":
		return says + "\n\nA picture is at " + screenshot + ". Read it."
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
