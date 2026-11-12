package server

import (
	"context"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/teranos/QNTX/internal/claudecode"
	harnesswire "github.com/teranos/QNTX/plugin/harness"
	"github.com/teranos/QNTX/plugin/harness/claudeharness"
)

// "Harness is a plugin that speaks grpc A2A"
//
// Claude Code as a plugin's harness (ADR-059): a plugin named claude that
// declares harness_provider runs every Claude Code turn of this node, as the pi
// plugin runs Pi's. While none runs, the node runs the Claude Code it holds, as
// before.

// runClaude runs said in Claude Code: in the claude plugin when one runs, and
// in the Claude Code the node holds when none does. Each record the plugin
// prints reaches each as Claude Code's own line; the one that says the turn's
// process runs reaches said.Started, as it does when the node runs the turn.
func (s *QNTXServer) runClaude(ctx context.Context, said claudecode.Said, each func(claudecode.Message)) (claudecode.Answer, error) {
	backend, runs := s.harnessBackend(agentLineClaude)
	if !runs {
		return said.Run(ctx, each)
	}
	run := claudeharness.Run{
		Home: said.Home, Session: said.Session, Resumes: said.Resumes,
		Model: said.Model, Effort: said.Effort, Token: said.Token, System: said.System,
		MCP: said.MCP, Mode: said.Mode, Allow: said.Allow, Env: said.Env, Apart: said.Apart,
	}
	var answer claudeharness.Answer
	ended, err := saidInBackend(ctx, backend, said.Says, run, func(event a2asdk.Event) error {
		var record claudeharness.Record
		carries, err := harnesswire.PrintedIn(event, &record)
		if !carries || err != nil {
			return err
		}
		if stream, started := claudeharness.IsStarted(record); started {
			if said.Started != nil {
				said.Started(stream)
			}
			return nil
		}
		each(record)
		return nil
	})
	if err != nil {
		return claudecode.Answer{}, err
	}
	if _, err := harnesswire.AnswerIn(ended, &answer); err != nil {
		return claudecode.Answer{}, err
	}
	return answer, nil
}
