package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// QNTX as an A2A client: a message said to another agent over A2A, and what it
// answered. "Why wouldn't the agents talk to each other through A2A?"
//
// The agent is one a peer serves, asked of the peer as the person asking here,
// or one any card names, asked as no one.

// a2aAskTimeout is how long an agent is given to answer: a turn of an agent
// can run long, and SendMessage waits for its Task to end (§3.2.2).
const a2aAskTimeout = 30 * time.Minute

// a2aCardMost is the most a card read from its URL holds.
const a2aCardMost = 1 << 20

func (s *QNTXServer) a2aSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "a2a",
			Description: "QNTX as an A2A client: a message said to another agent over A2A, a peer's or any card's.",
			Tags:        []string{"agent", "a2a"},
			Oasf:        []string{"ai_ml_engineering/agent_orchestration/multi_agent_coordination"},
			Sigils: []*protocol.Sigil{{
				Name: "ask",
				Does: "Says something to another agent over A2A and gives what it answered: an agent a peer serves, asked of the peer as the person asking here, or the agent a card names, asked as no one. One message, which the agent answers with a message or a Task; the Task is waited for until it ends.",
				Takes: []*protocol.Param{
					{Name: "says", Required: true, Says: "What is said to it."},
					{Name: "peer", Says: "The node this one is the primary of that serves the agent, by its label in nodes.peers. With tenant, or without for its Node Agent."},
					{Name: "tenant", Says: "The agent at the peer: a namespace, or element:<element>."},
					{Name: "card", Says: "The URL of the agent's card, for an agent no peer serves."},
					{Name: "context", Says: "The context the message is in, as an earlier answer gave it: said again, the agent continues it (§3.4.1)."},
				},
				Answer: "protocol.A2AAsked",
				Http:   &protocol.Endpoint{Method: http.MethodPost, Path: "/api/a2a/ask"},
			}},
		},
		Answers: map[string]sigil.Answer{"ask": s.a2aAsk},
	}
}

func (s *QNTXServer) a2aAsk(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	says := strings.TrimSpace(sent["says"])
	if says == "" {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "says", Says: "says is what is said to the agent, and nothing was"}
	}
	peer, card := sent["peer"], sent["card"]
	if (peer == "") == (card == "") {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "peer", Says: "an agent is named by a peer or by a card, one of them"}
	}
	if card != "" && sent["tenant"] != "" {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "tenant", Says: "a card names its agent's tenant itself"}
	}
	ctx, cancel := context.WithTimeout(ctx, a2aAskTimeout)
	defer cancel()
	var (
		client  *a2aclient.Client
		agent   string
		refusal *protocol.Refusal
	)
	if peer != "" {
		client, agent, refusal = s.a2aPeersAgent(ctx, peer, sent["tenant"])
	} else {
		client, agent, refusal = a2aCardsAgent(ctx, card)
	}
	if refusal != nil {
		return nil, refusal
	}
	defer func() { _ = client.Destroy() }()

	message := a2asdk.NewMessage(a2asdk.MessageRoleUser, a2asdk.NewTextPart(says))
	message.ContextID = sent["context"]
	answered, err := client.SendMessage(ctx, &a2asdk.SendMessageRequest{Message: message})
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: agent + " did not answer: " + err.Error()}
	}
	return a2aAskedOf(agent, answered), nil
}

// a2aPeersAgent is a client of the agent peer serves at tenant, carrying the
// person asking here to the peer and nowhere else.
func (s *QNTXServer) a2aPeersAgent(ctx context.Context, label, tenant string) (*a2aclient.Client, string, *protocol.Refusal) {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Says: "a peer's agent is asked as the person asking here, and this asking came through no gate"}
	}
	peers := s.peers()
	at := slices.IndexFunc(peers, func(peer auth.Client) bool { return peer.Label == label })
	if at < 0 {
		return nil, "", &protocol.Refusal{Why: sigil.NotFound, Param: "peer", Says: "nodes.peers names no " + label}
	}
	peer := peers[at]
	agent := peer.Label + "'s Node Agent"
	if tenant != "" {
		if !peer.Serves(tenant) && !strings.HasPrefix(tenant, elementTenantPrefix) {
			return nil, "", &protocol.Refusal{Why: sigil.NotFound, Param: "tenant", Says: "nodes.peers says " + label + " does not serve " + tenant}
		}
		agent = "the agent of " + tenant + " at " + peer.Label
	}
	bearer, err := s.authHandler.BearerFor(admitted, peer)
	if err != nil {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	carried := &peerCarrier{bearer: bearer, origin: peer.Origin}
	client, err := a2aclient.NewFromEndpoints(ctx,
		[]*a2asdk.AgentInterface{{URL: strings.TrimSuffix(peer.Origin.JoinPath(a2aPrefix).String(), "/"), ProtocolBinding: a2asdk.TransportProtocolHTTPJSON, ProtocolVersion: a2asdk.Version, Tenant: tenant}},
		a2aclient.WithRESTTransport(&http.Client{Timeout: a2aAskTimeout, Transport: carried,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}))
	if err != nil {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Says: "no A2A client speaks to " + peer.Label + ": " + err.Error()}
	}
	return client, agent, nil
}

// a2aCardsAgent is a client of the agent the card at url names, asked as no
// one, over what the internet routes to alone: the card's URL and the
// interfaces it names are anyone's to write (§13.2).
func a2aCardsAgent(ctx context.Context, url string) (*a2aclient.Client, string, *protocol.Refusal) {
	public := publicClient(a2aAskTimeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", &protocol.Refusal{Why: sigil.Invalid, Param: "card", Says: "card " + url + " is no URL: " + err.Error()}
	}
	resp, err := public.Do(req)
	if err != nil {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Param: "card", Says: "the card at " + url + " was not read: " + err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, a2aCardMost))
	if err != nil {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Param: "card", Says: "the card at " + url + " was not read: " + err.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Param: "card", Says: url + " answered " + resp.Status + " for a card: " + bounded(string(body), saidDefectMost)}
	}
	card := &a2asdk.AgentCard{}
	if err := json.Unmarshal(body, card); err != nil {
		return nil, "", &protocol.Refusal{Why: sigil.Invalid, Param: "card", Says: url + " holds no agent card: " + err.Error()}
	}
	client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithRESTTransport(public), a2aclient.WithJSONRPCTransport(public))
	if err != nil {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Param: "card", Says: "no A2A client speaks to the agent " + url + " names: " + err.Error()}
	}
	agent := card.Name
	if agent == "" {
		agent = "the agent at " + url
	}
	return client, agent, nil
}

// a2aAskedOf is what the agent answered: a message, or a Task.
func a2aAskedOf(agent string, answered a2asdk.SendMessageResult) *protocol.A2AAsked {
	asked := &protocol.A2AAsked{Agent: agent}
	switch v := answered.(type) {
	case *a2asdk.Message:
		asked.Context, asked.Answer = v.ContextID, partsText(v.Parts)
	case *a2asdk.Task:
		asked.Task, asked.Context, asked.State = string(v.ID), v.ContextID, string(v.Status.State)
		var said []string
		for _, artifact := range v.Artifacts {
			if text := partsText(artifact.Parts); text != "" {
				said = append(said, text)
			}
		}
		asked.Answer = strings.Join(said, "\n")
		if v.Status.Message != nil && v.Status.State != a2asdk.TaskStateCompleted {
			asked.Why = partsText(v.Status.Message.Parts)
		}
	}
	return asked
}

// partsText is the text parts of parts, one after another.
func partsText(parts a2asdk.ContentParts) string {
	var texts []string
	for _, part := range parts {
		if text := part.Text(); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}
