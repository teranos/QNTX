package server

import (
	"slices"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

// A turn a restart cuts off is resumed. A deploy restarts the node, and the
// node's restarting kills the turn the ROOT agent is in: "Every deploy kills me
// mid-sentence". The session survives it; the turn is picked up once the node
// is up again, in the same session, and only once.

// nodeStopped is the error a turn ends in when the node stopping ended it.
const nodeStopped = "node_stopped"

// picksUpKey is what a turn the node resumed says on what was said in it: the
// turn it picks up, by the id of what was said there.
const picksUpKey = "picks_up"

const (
	// resumeAfter is how long the node is up before it picks up a cut-off
	// turn: a node that restarts again within it is looping, and picks up none.
	resumeAfter = 2 * time.Minute
	// resumeWithin is how recent a cut-off turn is to be picked up.
	resumeWithin = time.Hour
)

// cutOff is the last turn in rows, and whether it was cut off: said, and
// either never ended or ended by the node stopping.
func cutOff(rows []*types.As) (*types.As, bool) {
	inOrder := slices.Clone(rows)
	slices.SortStableFunc(inOrder, func(a, b *types.As) int { return a.Timestamp.Compare(b.Timestamp) })
	var said, ended *types.As
	var wasSaid, wasEnded bool
	for _, row := range inOrder {
		switch {
		case slices.Contains(row.Predicates, "UserPromptSubmit"):
			said, wasSaid, wasEnded = row, true, false
		case wasSaid && (slices.Contains(row.Predicates, "Stop") || slices.Contains(row.Predicates, "StopFailure")):
			ended, wasEnded = row, true
		}
	}
	if !wasSaid {
		return said, false
	}
	if !wasEnded {
		return said, true
	}
	stopped, isText := ended.Attributes["error"].(string)
	return said, isText && stopped == nodeStopped && slices.Contains(ended.Predicates, "StopFailure")
}

// resumeSays is what the agent is told when the node picks up turn.
func resumeSays(turn *types.As) (string, error) {
	prompt, isText := turn.Attributes["prompt"].(string)
	if !isText {
		return "", errors.Newf("what was said in turn %s is not text", turn.ID)
	}
	return "The node stopped while you were answering this, said to you at " + turn.Timestamp.UTC().Format(time.RFC3339) + ":\n\n" +
		prompt + "\n\n" +
		"The node is up again. Pick up where you were cut off: check what you had already done before doing it again.", nil
}

// refusedAs is refused as an error when it is one of the kinds a sigil
// refuses with, and no error when nothing refused.
func refusedAs(refused *protocol.Refusal) error {
	switch refused.GetWhy() {
	case sigil.Missing, sigil.NotOneOf, sigil.Invalid, sigil.NotFound, sigil.NotAllowed, sigil.Failed:
		return errors.New(refusalSays(refused))
	}
	return nil
}

// resumeCutOff picks up, once, the turn the node's last stopping cut off in
// each harness the ROOT agent runs in, after the node has been up resumeAfter.
// A harness that does not pick up leaves the ones after it for ROOT.
func (s *QNTXServer) resumeCutOff(agent *rootAgent) {
	select {
	case <-time.After(resumeAfter):
	case <-s.ctx.Done():
		return
	}
	for _, h := range s.harnesses() {
		if !h.named() {
			continue
		}
		if err := s.resumeIn(h, agent); err != nil {
			s.logger.Errorw("A turn the node's stopping cut off was not picked up, and the harnesses after it were not tried",
				"agent", agent.did, "harness", h.called, "error", err)
			return
		}
	}
}

// resumeIn picks up agent's cut-off turn in h, when there is one.
func (s *QNTXServer) resumeIn(h *harness, agent *rootAgent) error {
	in := agent.in(h)
	session, kept, err := agent.sessionIn(in.file)
	if err != nil {
		return err
	}
	if !kept {
		return nil
	}
	read, err := s.held.Read(agent.namespace)
	if err != nil {
		return errors.Wrapf(err, "no %s to read session %s from", agent.namespace, session)
	}
	since := time.Now().Add(-resumeWithin)
	rows, err := read.GetAttestations(ats.AttestationFilter{
		Contexts:   []string{"session:" + session},
		Predicates: []string{"UserPromptSubmit", "Stop", "StopFailure"},
		TimeStart:  &since,
		Limit:      ats.EveryRow,
	})
	if err != nil {
		return errors.Wrapf(err, "session %s did not read", session)
	}
	turn, wasCutOff := cutOff(rows)
	if !wasCutOff {
		return nil
	}
	if picked, already := turn.Attributes[picksUpKey]; already {
		s.logger.Warnw("A turn the node picked up was cut off again, and is left for ROOT",
			"agent", agent.did, "harness", h.called, "session", session, "turn", turn.ID, "picked_up", picked)
		return nil
	}
	says, err := resumeSays(turn)
	if err != nil {
		return err
	}
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return errors.Wrapf(err, "nowhere to write session %s", session)
	}
	t, refused := h.part(s.ctx, sigil.Sent{}, agent, h.spec())
	if err := refusedAs(refused); err != nil {
		return errors.Wrapf(err, "%s was not ready to pick up turn %s", h.called, turn.ID)
	}
	t.in, t.called = in, h.called
	s.logger.Infow("The node picks up a turn its stopping cut off", "agent", agent.did, "harness", h.called, "session", session, "turn", turn.ID)
	answer, refused := s.turnIn(s.ctx, s.nodeDID.DID, store, agent, says, t, map[string]any{picksUpKey: turn.ID})
	if err := refusedAs(refused); err != nil {
		return errors.Wrapf(err, "turn %s was not picked up", turn.ID)
	}
	s.logger.Infow("The node picked up a turn its stopping cut off", "agent", agent.did, "harness", h.called, "session", session, "turn", turn.ID, "answer", answer)
	return nil
}
