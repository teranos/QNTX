package server

import (
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/internal/claudecode"
	errors "github.com/teranos/sacred-error"
)

// The node hosts the process, it starts it, resumes it and stops it (ADR-048).
// A turn the node's last process started runs on when that process stops, and
// this one reads it on, from its stream, to how it ends.

// followLeftTurns reads on each turn the node's last process left, oldest
// first, holding the session in so nothing new is said to it over a turn
// still going. One not read to its end leaves the ones after it. Once every one
// is read, a Task from before since that no turn is left to end is settled.
func (s *QNTXServer) followLeftTurns(agent *rootAgent, in *inHarness, effort string, since time.Time) {
	defer func() { <-in.turn }()
	turns, err := claudecode.Pending(agent.home)
	if err != nil {
		s.logger.Errorw("The turns the node's last process left were not listed", "agent", agent.did, "home", agent.home, "error", err)
		return
	}
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		s.logger.Errorw("The turns the node's last process left have nowhere to be written", "agent", agent.did, "error", err)
		return
	}
	for _, turn := range turns {
		if err := s.followLeftTurn(agent, in, store, effort, turn); err != nil {
			s.logger.Errorw("A turn the node's last process left was not read to its end, nor the ones after it",
				"agent", agent.did, "session", turn.Session, "stream", turn.Stream, "error", err)
			return
		}
	}
	if err := s.settleTasksNoTurnLeft(agent, since); err != nil {
		s.logger.Errorw("The tasks the node's last process left with no turn to end them were not settled", "agent", agent.did, "error", err)
	}
}

// followLeftTurn reads turn on to how it ends, writing what it did as the
// agent, and keeps the session it ran in.
func (s *QNTXServer) followLeftTurn(agent *rootAgent, in *inHarness, store ats.AttestationStore, effort string, turn claudecode.Turn) error {
	in.answering.Store(&turnInSession{session: turn.Session})
	defer in.answering.Store(nil)

	writes := sessionWriter{did: agent.did, session: turn.Session, resumed: true, effort: effort}
	write, unwritten := s.sessionRows(agent, store, turn.Session, "Claude Code")
	s.logger.Infow("The node reads on a turn its last process left", "agent", agent.did, "session", turn.Session, "started", turn.Started)
	answer, err := turn.Follow(s.ctx, func(m claudecode.Message) { write(writes.rowsOf(m, time.Now())) })
	if err != nil {
		if nodeErr := s.ctx.Err(); nodeErr != nil {
			return errors.WithSecondaryError(errors.Wrap(nodeErr, "the node stopped again while it read the turn, and the next node reads it on"), err)
		}
		write(writes.rowsOf(claudecode.Message{Type: "result", Subtype: "no_result", IsError: true, Result: err.Error()}, time.Now()))
	}
	if settleErr := s.settleLeftTask(agent, turn.Session, turn.Stream, answer, err); settleErr != nil {
		return errors.Wrapf(settleErr, "the turn in %s was read to its end, and the task it was is not settled", turn.Session)
	}

	continued, held, err := agent.sessionIn(in.file)
	if err != nil {
		return err
	}
	if !held {
		if err := agent.keepIn(in.file, turn.Session); err != nil {
			return errors.Wrapf(err, "the turn in %s was read to its end, and the session was not kept", turn.Session)
		}
	}
	s.logger.Infow("A turn the node's last process left was read to its end", "agent", agent.did, "session", turn.Session,
		"continues", continued, "answer", answer.Text, "is_error", answer.IsError, "unwritten", unwritten())
	return nil
}
