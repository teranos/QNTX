package server

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/internal/admission"
	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/proto"
)

const (
	namespaceAnthropicSubject   = "ANTHROPIC"
	namespaceAnthropicPredicate = "federation"
	namespaceAnthropicSession   = "anthropic-session"
)

func (s *QNTXServer) namespaceAnthropicSignum() sigil.Signum {
	namespace := &protocol.Param{Name: "namespace", Required: true, Says: "The namespace whose Anthropic federation this is."}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "namespace_anthropic",
			Description: "A namespace's own Anthropic workload identity federation: its issuer, organization, rule and service account. No API key.",
			Tags:        []string{"anthropic", "oidc", "namespaces"},
			Sigils: []*protocol.Sigil{
				{Name: "am", Does: "The namespace's OIDC issuer and Anthropic federation registration.", Takes: []*protocol.Param{namespace}, Answer: "protocol.NodeAnthropic", Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/agents/{namespace}/anthropic"}},
				{Name: "set", Does: "Keeps Anthropic's organization, federation rule, service account and optional workspace IDs in this namespace, then asks Anthropic to accept its identity.", Takes: []*protocol.Param{namespace, {Name: "given", Required: true, Says: "The organization UUID and Anthropic IDs copied from workload identity federation."}}, Answer: "protocol.NodeAnthropic", Http: &protocol.Endpoint{Method: http.MethodPost, Path: "/api/agents/{namespace}/anthropic"}},
				{Name: "issuer", Does: "The namespace's distinct OIDC issuer URL and subject to register with Anthropic.", Takes: []*protocol.Param{namespace}, Answer: "protocol.NodeAnthropic", Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/agents/{namespace}/issuer"}},
			},
		},
		Answers: map[string]sigil.Answer{"am": s.namespaceAnthropicAm, "set": s.namespaceAnthropicSet, "issuer": s.namespaceAnthropicIssuer},
	}
}

func namespaceAnthropicHarness() *harness {
	return &harness{name: agentLineAnthropic, called: "Anthropic Managed Agents", file: namespaceAnthropicSession}
}

func (s *QNTXServer) namespaceAnthropicIssuer(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	asked, refused := agentNamespaceOf(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	issuer, err := s.namespaceIssuer(asked.namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	subject, err := s.namespaceAnthropicSubject(asked.namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return &protocol.NodeAnthropic{Issuer: issuer.url, Subject: subject}, nil
}

func (s *QNTXServer) namespaceAnthropicAm(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	asked, refused := agentNamespaceOf(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	return s.namespaceAnthropicAmOf(ctx, asked.namespace)
}

func (s *QNTXServer) namespaceAnthropicAmOf(ctx context.Context, namespace string) (*protocol.NodeAnthropic, *protocol.Refusal) {
	issuer, err := s.namespaceIssuer(namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	subject, err := s.namespaceAnthropicSubject(namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	kept, held, err := s.namespaceAnthropicLine(namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	am := &protocol.NodeAnthropic{Issuer: issuer.url, Subject: subject}
	if !held {
		am.Why = "the namespace has no complete Anthropic workload identity federation registration"
		return am, nil
	}
	am.OrganizationId = kept.ids["organization_id"]
	am.IssuerId = kept.ids["issuer_id"]
	am.FederationRuleId = kept.ids["federation_rule_id"]
	am.ServiceAccountId = kept.ids["service_account_id"]
	am.WorkspaceId = kept.ids["workspace_id"]
	am.SetBy, am.At = kept.setBy, kept.at.Format(time.RFC3339)
	if am.OrganizationId == "" || am.ServiceAccountId == "" || am.FederationRuleId == "" {
		am.Why = "the namespace has no complete Anthropic workload identity federation registration"
		return am, nil
	}
	am.Asked = true
	client, err := s.namespaceAnthropicClient(namespace, kept)
	if err != nil {
		am.Why = err.Error()
		return am, nil
	}
	asking, cancel := context.WithTimeout(ctx, anthropicAsking)
	defer cancel()
	if _, err := client.Models.List(asking, anthropic.ModelListParams{}); err != nil {
		am.Why = errors.Wrapf(err, "Anthropic did not accept namespace %s's identity at rule %s", namespace, am.FederationRuleId).Error()
		return am, nil
	}
	am.Accepted = true
	return am, nil
}

func (s *QNTXServer) namespaceAnthropicSet(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	asked, refused := namespaceAnthropicAsking(ctx, sent, s)
	if refused != nil {
		return nil, refused
	}
	found := anthropicIDsIn(sent["given"])
	if len(found) == 0 {
		return nil, &protocol.Refusal{Why: sigil.NotOneOf, Param: "given", Says: "no Anthropic organization, issuer, service account, rule or workspace ID was found"}
	}
	kept, _, err := s.namespaceAnthropicLine(asked.namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	for name, id := range found {
		kept.ids[name] = id
	}
	if err := s.keepNamespaceAnthropicLine(asked.admitted, asked.namespace, kept); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	s.logger.Infow("a namespace's Anthropic federation IDs were given", "namespace", asked.namespace, "given", found, "by", asked.admitted.Identity)
	return s.namespaceAnthropicAmOf(ctx, asked.namespace)
}

func namespaceAnthropicAsking(ctx context.Context, sent sigil.Sent, s *QNTXServer) (anAsking, *protocol.Refusal) {
	asked, refused := agentNamespaceOf(ctx, sent)
	return then(asked, refused, func(asked anAsking) (anAsking, *protocol.Refusal) {
		if _, isToken := asked.admitted.Asker().(admission.TokenAsked); isToken {
			return anAsking{}, &protocol.Refusal{Why: sigil.NotAllowed, Says: "Anthropic federation for " + asked.namespace + " is handled by a person, and a token asked"}
		}
		if client, err := named.TextOf("the connector's client", asked.admitted.ClientDID); err == nil {
			return anAsking{}, &protocol.Refusal{Why: sigil.NotAllowed, Says: "Anthropic federation for " + asked.namespace + " is handled by a person, and the connector " + client.String() + " asked"}
		}
		return asked, s.mayHandle(asked.admitted, asked.namespace, "Anthropic federation for "+asked.namespace+" is handled")
	})
}

func (s *QNTXServer) namespaceAnthropicLine(namespace string) (anthropicKept, bool, error) {
	reading, err := s.held.Read(namespace)
	if err != nil {
		return anthropicKept{}, false, errors.Wrapf(err, "failed to read the Anthropic federation line of %s", namespace)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{namespaceAnthropicSubject}, Limit: ats.EveryRow})
	if err != nil {
		return anthropicKept{}, false, errors.Wrapf(err, "failed to read the Anthropic federation line of %s", namespace)
	}
	var newest *types.As
	for _, row := range found {
		if !slices.Contains(row.Predicates, namespaceAnthropicPredicate) {
			continue
		}
		if newest == nil || row.Timestamp.After(newest.Timestamp) {
			newest = row
		}
	}
	if newest == nil {
		return anthropicKept{ids: map[string]string{}}, false, nil
	}
	kept := anthropicKept{ids: map[string]string{}, at: newest.Timestamp}
	for _, name := range anthropicIDs {
		if _, exists := newest.Attributes[name]; !exists {
			continue
		}
		if kept.ids[name], err = oneText(newest.Attributes, name, namespaceAnthropicSubject, newest.ID, namespace); err != nil {
			return anthropicKept{}, false, err
		}
	}
	setters, err := named.ItemsOf(namespaceAnthropicSubject+" line "+newest.ID+", who gave it,", newest.Actors)
	if err != nil {
		return anthropicKept{}, false, err
	}
	kept.setBy = setters.First()
	return kept, true, nil
}

func (s *QNTXServer) keepNamespaceAnthropicLine(admitted auth.Admission, namespace string, kept anthropicKept) error {
	store, err := s.held.Write(admitted, namespace)
	if err != nil {
		return errors.Wrapf(err, "no store to keep the Anthropic federation line of %s in", namespace)
	}
	attributes := map[string]any{}
	for name, id := range kept.ids {
		if id != "" {
			attributes[name] = id
		}
	}
	id, err := identity.GenerateASUIDWithRetry("AS", namespaceAnthropicSubject, namespaceAnthropicPredicate, namespace, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the Anthropic federation line of %s", namespace)
	}
	now := time.Now()
	return errors.Wrapf(store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{namespaceAnthropicSubject}, Predicates: []string{namespaceAnthropicPredicate}, Contexts: []string{namespace},
		Actors: []string{admitted.Identity}, Timestamp: now, CreatedAt: now, Source: "anthropic", Attributes: attributes,
	}), "the Anthropic federation line of %s was not kept", namespace)
}

func (s *QNTXServer) namespaceAnthropicSubject(namespace string) (string, error) {
	if s.nodeDID == nil {
		return "", errors.New("this node has no key of its own to derive a namespace subject from")
	}
	_, did, err := access.DeriveToken(s.nodeDID.PrivateKey, "namespace:"+slug.Of(namespace))
	return did, err
}

func (s *QNTXServer) namespaceAnthropicClient(namespace string, kept anthropicKept) (anthropic.Client, error) {
	if kept.ids["organization_id"] == "" || kept.ids["federation_rule_id"] == "" || kept.ids["service_account_id"] == "" {
		return anthropic.Client{}, errors.Newf("namespace %s has no complete Anthropic workload identity federation registration", namespace)
	}
	issuer, err := s.namespaceIssuer(namespace)
	if err != nil {
		return anthropic.Client{}, err
	}
	subject, err := s.namespaceAnthropicSubject(namespace)
	if err != nil {
		return anthropic.Client{}, err
	}
	return s.federatedAnthropicClient(kept, issuer, subject, namespace)
}

func (s *QNTXServer) namespaceAnthropicAgent(ctx context.Context, sent sigil.Sent) (anAgentHeld, *protocol.Refusal) {
	asked, refused := agentNamespaceOf(ctx, sent)
	if refused != nil {
		return anAgentHeld{}, refused
	}
	row, setBy, held, err := s.newestAgentLine(asked.namespace, agentLineAnthropic)
	if err != nil {
		return anAgentHeld{}, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if !held {
		return anAgentHeld{}, &protocol.Refusal{Why: sigil.NotFound, Says: asked.namespace + " opted into no Anthropic agent: set one with agents set, harness anthropic"}
	}
	model, err := oneText(row.Attributes, "model", agentLineSubject, row.ID, asked.namespace)
	if err != nil || strings.TrimSpace(model) == "" {
		if err == nil {
			err = errors.New("the Anthropic agent has no model")
		}
		return anAgentHeld{}, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	agent, refused := s.holdNamespaceAgent(asked.namespace, model, setBy)
	return anAgentHeld{agent: agent, line: agentLine{spec: agentSpec{Model: model}, setBy: setBy, at: row.Timestamp}}, refused
}

func (s *QNTXServer) setAnthropicAgent(ctx context.Context, sent sigil.Sent, admitted auth.Admission, namespace, model string) (any, *protocol.Refusal) {
	if err := s.setAgentLine(admitted, namespace, agentLineAnthropic, map[string]any{"model": model}); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	agent, refused := s.holdNamespaceAgent(namespace, model, admitted.Identity)
	if refused != nil {
		return nil, refused
	}
	return s.namespaceAnthropicAgentAmOf(ctx, agent, agentLine{spec: agentSpec{Model: model}, setBy: admitted.Identity})
}

func (s *QNTXServer) namespaceAnthropicAgentAm(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	held, refused := s.namespaceAnthropicAgent(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	return s.namespaceAnthropicAgentAmOf(ctx, held.agent, held.line)
}

func (s *QNTXServer) namespaceAnthropicAgentAmOf(ctx context.Context, agent *rootAgent, line agentLine) (*protocol.NamespaceAgentAm, *protocol.Refusal) {
	am := &protocol.ClaudeAm{Did: agent.did, Model: line.spec.Model, AuthMethod: "Anthropic workload identity federation"}
	var err error
	var kept bool
	am.Session, kept, err = agent.sessionIn(agent.in(namespaceAnthropicHarness()).file)
	if err != nil {
		am.NotReady = err.Error()
	} else if !kept {
		am.Session = ""
	}
	if going := agent.in(namespaceAnthropicHarness()).answering.Load(); going != nil {
		am.Answering, am.Session = true, going.session
	}
	status, refused := s.namespaceAnthropicAmOf(ctx, agent.namespace)
	if refused != nil {
		am.NotReady = refused.Says
	} else {
		am.SignedIn, am.NotReady = status.Accepted, status.Why
	}
	return &protocol.NamespaceAgentAm{Namespace: agent.namespace, SetBy: line.setBy, Agent: am}, nil
}

func (s *QNTXServer) namespaceAnthropicSay(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	held, refused := s.namespaceAnthropicAgent(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	agent, line := held.agent, held.line
	config, configured, err := s.namespaceAnthropicLine(agent.namespace)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if !configured || config.ids["organization_id"] == "" || config.ids["service_account_id"] == "" || config.ids["federation_rule_id"] == "" {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the namespace has no complete Anthropic workload identity federation registration"}
	}
	client, err := s.namespaceAnthropicClient(agent.namespace, config)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	h := namespaceAnthropicHarness()
	h.part = func(context.Context, sigil.Sent, *rootAgent, agentSpec) (aTurn, *protocol.Refusal) {
		return aTurn{
			withoutClaude: true, keepBeforeRun: true,
			ready: func(context.Context, string, bool) (bool, *protocol.Refusal) { return false, nil },
			run: func(t turnRun) (proto.Message, error) {
				answer, err := s.runManagedWithClient(agent, claudecode.Said{
					Model: line.spec.Model, Session: t.session, Says: t.says, Started: t.started,
				}, t.shot, config, client, func(message claudecode.Message) {
					t.write(t.writes.rowsOf(message, t.now()))
				})
				if err != nil {
					return nil, err
				}
				return &protocol.ClaudeSaid{
					Answer: answer.Text, IsError: answer.IsError, Subtype: answer.Subtype,
					Session: t.session, Model: line.spec.Model, CostUsd: answer.CostUSD,
					TookMs: float64(answer.Took.Milliseconds()), Unwritten: t.unwritten(),
				}, nil
			},
		}, nil
	}
	answer, refused := s.sayTo(ctx, h, agent, line.spec, sent)
	if refused != nil {
		return nil, refused
	}
	said, ok := answer.(*protocol.ClaudeSaid)
	if !ok {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the namespace's Anthropic agent did not answer as Claude"}
	}
	return &protocol.NamespaceAgentSaid{Claude: said}, nil
}
