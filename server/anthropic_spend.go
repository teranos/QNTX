package server

// What ROOT's agents spend at Anthropic, and in which order (QNTX-Web stories/29-root-orders-what-its-agents-spend.feature).
// "we use session first until exhaustion"
// "this applies to all agents belonging to ROOT."

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// What ROOT's agents spend: the Claude subscription, or the node's OIDC at the Console.
const (
	anthropicSession = "session"
	anthropicConsole = "console"
)

var anthropicSources = []string{anthropicSession, anthropicConsole}

// What a turn ends with when what it spent on is exhausted, as Claude Code says it
// (code.claude.com/docs/en/errors, Usage limits).
var exhaustedSays = map[string][]string{
	anthropicSession: {"hit your session limit", "hit your weekly limit", "hit your Opus limit", "hit your Sonnet limit"},
	anthropicConsole: {"Credit balance is too low"},
}

// identityTokenFile is where a ROOT agent's run reads the node's identity token, in its home.
const identityTokenFile = "anthropic-identity-token"

// identityTokens is each ROOT agent home whose identity token is being renewed.
type identityTokens = sync.Map

// inUse is what ROOT's agents last ran on.
type inUse struct{ v atomic.Value }

func (u *inUse) Load() string {
	if s, ok := u.v.Load().(string); ok {
		return s
	}
	return ""
}

func (u *inUse) Store(source string) { u.v.Store(source) }

func (s *QNTXServer) anthropicOrder(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "what ROOT's agents spend is ordered through the gate, and this came through none"}
	}
	kept, _, err := s.anthropicLine()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	kept.first, kept.then = sent["first"], sent["then"] == "true"
	if refused := s.keepAnthropicLine(admitted.Identity, kept); refused != nil {
		return nil, refused
	}
	s.logger.Infow("what ROOT's agents spend at Anthropic was ordered", "first", kept.first, "then", kept.then, "by", admitted.Identity)
	return s.anthropicAm(ctx, sent)
}

// sessionWindows is each window of the Claude subscription as ug last read it,
// in the namespace the caller stands in (am_ground.go). Asked outside a request, none.
func (s *QNTXServer) sessionWindows(ctx context.Context) ([]*protocol.AnthropicWindow, error) {
	caller := sigil.Caller(ctx)
	if caller == nil {
		return nil, nil
	}
	store, err := s.storeFor(caller)
	if err != nil {
		return nil, errors.Wrap(err, "the store ug's readings are in did not open")
	}
	since := time.Now().Add(-ugReadingsWindow)
	found, err := store.GetAttestations(ats.AttestationFilter{
		Predicates: []string{ugReadingPredicate}, Actors: []string{ugActor},
		TimeStart: types.BoundAt(since), TimeEnd: types.Unbounded(), Limit: ugReadingsLimit,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "reading the %s rows %s posted since %s", ugReadingPredicate, ugActor, since.UTC().Format(time.RFC3339))
	}
	latest := map[string]*types.As{}
	for _, as := range found {
		if len(as.Subjects) == 0 || len(as.Predicates) == 0 || as.Predicates[0] != ugReadingPredicate {
			continue
		}
		if was, seen := latest[as.Subjects[0]]; !seen || as.Timestamp.After(was.Timestamp) {
			latest[as.Subjects[0]] = as
		}
	}
	windows := []*protocol.AnthropicWindow{}
	for _, name := range []string{"five_hour", "seven_day"} {
		as, seen := latest[name]
		if !seen {
			continue
		}
		used, _ := as.Attributes["used_percentage"].(float64)
		resets, _ := as.Attributes["resets_at"].(float64)
		windows = append(windows, &protocol.AnthropicWindow{Window: name, UsedPercentage: used, ResetsAt: int64(resets), At: as.Timestamp.UTC().Format(time.RFC3339)})
	}
	return windows, nil
}

// spend runs a ROOT agent's turn on what ROOT ordered: the first until it is
// exhausted, then the other when ROOT ticked it. Any other agent's turn runs as said.
func (s *QNTXServer) spend(agent *rootAgent, said claudecode.Said, each func(claudecode.Message)) (claudecode.Answer, error) {
	if agent.namespace != auth.NamespaceSystem {
		return said.Run(s.ctx, each)
	}
	kept, _, err := s.anthropicLine()
	if err != nil {
		return claudecode.Answer{}, err
	}
	order := []string{kept.first}
	if kept.then {
		order = append(order, otherSource(kept.first))
	}
	var answer claudecode.Answer
	for n, source := range order {
		on := said
		if source == anthropicConsole {
			if on, err = s.onConsole(agent, said, kept); err != nil {
				return claudecode.Answer{}, err
			}
		}
		// The second runs on what the first left in the session.
		if n > 0 {
			on.Resumes = claudecode.Holds(agent.home, said.Session)
		}
		s.anthropicInUse.Store(source)
		answer, err = on.Run(s.ctx, each)
		if err != nil {
			s.logger.Errorw("a ROOT agent's turn did not run", "on", source, "session", said.Session, "error", err)
			return answer, err
		}
		s.logger.Infow("a ROOT agent's turn ran", "on", source, "is_error", answer.IsError, "said", answer.Text, "cost_usd", answer.CostUSD, "session", said.Session)
		if !exhausted(source, answer) {
			return answer, err
		}
		s.logger.Infow("what ROOT's agents spend was exhausted", "source", source, "said", answer.Text, "session", said.Session)
	}
	return answer, nil
}

// onConsole is said run on the node's OIDC: Claude Code trades the node's identity
// token at the rule ROOT gave, in place of the subscription (code.claude.com/docs/en/iam,
// Anthropic profiles and federation credentials).
func (s *QNTXServer) onConsole(agent *rootAgent, said claudecode.Said, kept anthropicKept) (claudecode.Said, error) {
	rule, organization, account := kept.ids["federation_rule_id"], kept.ids["organization_id"], kept.ids["service_account_id"]
	if rule == "" || organization == "" || account == "" {
		return said, errors.Newf("ROOT's agents spend on the Console, and the node has no rule at Anthropic yet: organization %q, service account %q, rule %q", organization, account, rule)
	}
	i, err := s.theIssuer()
	if err != nil {
		return said, err
	}
	file := filepath.Join(agent.home, identityTokenFile)
	if err := writeIdentityToken(i, s.nodeDID.DID, file); err != nil {
		return said, err
	}
	if _, renewing := s.identityTokensRenewed.LoadOrStore(agent.home, true); !renewing {
		sacred.Go("anthropic.identity-token", func() { s.keepIdentityTokensFresh(agent) })
	}
	// The plan token outranks federation in Claude Code: on the Console it is not handed over.
	said.Token = ""
	said.Env = append(append([]string{}, said.Env...),
		"ANTHROPIC_FEDERATION_RULE_ID="+rule, "ANTHROPIC_ORGANIZATION_ID="+organization,
		"ANTHROPIC_SERVICE_ACCOUNT_ID="+account, "ANTHROPIC_IDENTITY_TOKEN_FILE="+file)
	if workspace := kept.ids["workspace_id"]; workspace != "" {
		said.Env = append(said.Env, "ANTHROPIC_WORKSPACE_ID="+workspace)
	}
	return said, nil
}

// writeIdentityToken writes a fresh identity token for subject where Claude Code reads it.
func writeIdentityToken(i *issuer, subject, file string) error {
	token, err := i.identityToken(subject, auth.NamespaceSystem)
	if err != nil {
		return err
	}
	return errors.Wrapf(os.WriteFile(file, []byte(token), 0o600), "the identity token was not written to %s", file)
}

// keepIdentityTokensFresh rewrites each ROOT agent's identity token before the last one
// expires: Claude Code reads the file again at every exchange (platform.claude.com/docs/en/manage-claude/wif-reference).
func (s *QNTXServer) keepIdentityTokensFresh(agent *rootAgent) {
	file := filepath.Join(agent.home, identityTokenFile)
	tick := time.NewTicker(identityTokenLifetime / 2)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
			if _, err := os.Stat(file); err != nil {
				continue
			}
			i, err := s.theIssuer()
			if err == nil {
				err = writeIdentityToken(i, s.nodeDID.DID, file)
			}
			if err != nil {
				s.logger.Errorw("the identity token a ROOT agent spends on the Console with was not renewed", "file", file, "error", err)
			}
		}
	}
}

func otherSource(source string) string {
	if source == anthropicConsole {
		return anthropicSession
	}
	return anthropicConsole
}

// exhausted is a turn that ended because what it spent on is exhausted.
func exhausted(source string, answer claudecode.Answer) bool {
	if !answer.IsError {
		return false
	}
	for _, says := range exhaustedSays[source] {
		if strings.Contains(answer.Text, says) {
			return true
		}
	}
	return false
}
