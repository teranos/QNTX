package server

// A namespace's agent in Claude Managed Agents, run in Anthropic's environment.
// "so we can have an agent, that isnt hosted on the same box as QNTX"
// "we will not be doing API key anymore"

// Its calls are made on access tokens traded for the node's identity token (oidc_issuer.go).

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/proto"
)

const (
	agentLineManaged = "managed"
	// managedSessionFile keeps the session as the node writes it down, and
	// managedSessionAtAnthropic the same session as Anthropic names it.
	managedSessionFile        = "managed-session"
	managedSessionAtAnthropic = "managed-session-anthropic"
)

// federation is where the agent trades the node's identity token: the
// organization, its rule, the service account the token acts as, the workspace.
type federation struct {
	organization, rule, serviceAccount, workspace string
}

// managedLine is a namespace's agent in Claude Managed Agents as its newest
// AGENT line says it: the model, the federation, and what Anthropic holds for it.
type managedLine struct {
	model, setBy string
	federation   federation
	agentID      string
	agentVersion int64
	environment  string
	vault        string
}

func (l managedLine) attributes() map[string]any {
	return map[string]any{
		"model": l.model, "organization_id": l.federation.organization, "federation_rule_id": l.federation.rule,
		"service_account_id": l.federation.serviceAccount, "workspace_id": l.federation.workspace,
		"agent_id": l.agentID, "agent_version": strconv.FormatInt(l.agentVersion, 10),
		"environment_id": l.environment, "vault_id": l.vault,
	}
}

// managedLineOf reads the newest AGENT line of the namespace in Claude Managed
// Agents. False is a namespace that opted into no agent there.
func (s *QNTXServer) managedLineOf(namespace string) (managedLine, bool, error) {
	newest, setBy, held, err := s.newestAgentLine(namespace, agentLineManaged)
	if err != nil || !held {
		return managedLine{}, held, err
	}
	line := managedLine{setBy: setBy}
	version := ""
	for name, into := range map[string]*string{
		"model": &line.model, "organization_id": &line.federation.organization, "federation_rule_id": &line.federation.rule,
		"service_account_id": &line.federation.serviceAccount, "workspace_id": &line.federation.workspace,
		"agent_id": &line.agentID, "agent_version": &version, "environment_id": &line.environment, "vault_id": &line.vault,
	} {
		if *into, err = oneText(newest.Attributes, name, agentLineSubject, newest.ID, namespace); err != nil {
			return managedLine{}, false, err
		}
	}
	if line.agentVersion, err = strconv.ParseInt(version, 10, 64); err != nil {
		return managedLine{}, false, errors.Wrapf(err, "%s line %s of %s: agent_version %q is no number", agentLineSubject, newest.ID, namespace, version)
	}
	return line, true, nil
}

// anthropicFor is a client of Anthropic's API acting as agent: every access
// token traded for an identity token the node signs for the agent's DID.
func (s *QNTXServer) anthropicFor(agent *rootAgent, f federation) (anthropic.Client, error) {
	i, err := s.theIssuer()
	if err != nil {
		return anthropic.Client{}, err
	}
	base := s.anthropicBaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	return anthropic.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(base),
		option.WithFederationTokenProvider(i.identityTokenOf(agent.did, agent.namespace), option.FederationOptions{
			FederationRuleID: f.rule, OrganizationID: f.organization, ServiceAccountID: f.serviceAccount, WorkspaceID: f.workspace,
		}),
	), nil
}

// managedSystem is what the agent is, said to it with everything said to it.
func managedSystem(agent *rootAgent) string {
	return "You are the agent of the namespace " + agent.namespace + " of a QNTX node, shared by everyone with reach on it. " +
		"You run in Anthropic's environment, not on the node's machine. " +
		"Your DID is " + agent.did + ". " +
		"The node's sigils are the tools of the MCP server named " + nodeAgentMCP + ", which you reach with your own token, and which keeps you in " + agent.namespace + "."
}

// setManagedAgent opts the namespace into its agent in Claude Managed Agents:
// Anthropic is asked, as the agent, for its environment, agent and vault.
func (s *QNTXServer) setManagedAgent(ctx context.Context, sent sigil.Sent, admitted auth.Admission, namespace, model string) (any, *protocol.Refusal) {
	f := federation{workspace: strings.TrimSpace(sent["workspace_id"])}
	for param, into := range map[string]*string{"organization_id": &f.organization, "federation_rule_id": &f.rule, "service_account_id": &f.serviceAccount} {
		held, err := named.TextOf(param, strings.TrimSpace(sent[param]))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: param, Says: err.Error() + ": an agent in Claude Managed Agents trades the node's identity token at its organization's federation rule"}
		}
		*into = held.String()
	}
	if s.authHandler == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node runs without auth, so it has no public origin for the agent of " + namespace + " to reach its MCP at"}
	}
	agent, refused := s.holdNamespaceAgent(namespace, model, admitted.Identity)
	if refused != nil {
		return nil, refused
	}
	client, err := s.anthropicFor(agent, f)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the agent of " + namespace + " has no way to reach Anthropic: " + err.Error()}
	}
	line, err := provisionManaged(ctx, client, agent, s.authHandler.PublicOrigin()+"/mcp", model)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "Anthropic did not set up the agent of " + namespace + ": " + err.Error()}
	}
	line.federation, line.setBy = f, admitted.Identity
	if err := s.setAgentLine(admitted, namespace, agentLineManaged, line.attributes()); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	s.logger.Infow("a namespace opted into an agent", "namespace", namespace, "harness", agentLineManaged, "model", model,
		"agent_id", line.agentID, "environment_id", line.environment, "vault_id", line.vault, "by", admitted.Identity)
	return &protocol.NamespaceAgentAm{Namespace: namespace, SetBy: admitted.Identity, Managed: managedAm(agent, line, agent.in(s.managedHarness(line)))}, nil
}

// provisionManaged asks Anthropic for the environment, the agent and the vault.
func provisionManaged(ctx context.Context, client anthropic.Client, agent *rootAgent, mcp, model string) (managedLine, error) {
	name := "qntx-" + slug.Of(agent.namespace)
	env, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
		Name: name,
		Config: anthropic.BetaEnvironmentNewParamsConfigUnion{OfCloud: &anthropic.BetaCloudConfigParams{
			Networking: anthropic.BetaCloudConfigParamsNetworkingUnion{OfLimited: &anthropic.BetaLimitedNetworkParams{
				AllowPackageManagers: anthropic.Bool(true), AllowMCPServers: anthropic.Bool(true),
			}},
		}},
	})
	if err != nil {
		return managedLine{}, errors.Wrapf(err, "no environment %s", name)
	}
	made, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
		Name:   name,
		Model:  anthropic.BetaManagedAgentsModelConfigParams{ID: model},
		System: anthropic.String(managedSystem(agent)),
		MCPServers: []anthropic.BetaManagedAgentsURLMCPServerParams{{
			Name: nodeAgentMCP, URL: mcp,
		}},
		Tools: []anthropic.BetaAgentNewParamsToolUnion{
			{OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{
				DefaultConfig: anthropic.BetaManagedAgentsAgentToolsetDefaultConfigParams{
					PermissionPolicy: anthropic.BetaManagedAgentsAgentToolsetDefaultConfigParamsPermissionPolicyUnion{OfAuto: &anthropic.BetaManagedAgentsAutoPolicyParam{}},
				},
				Configs: []anthropic.BetaManagedAgentsAgentToolConfigParamsUnion{
					{OfWebFetch: &anthropic.BetaManagedAgentsWebFetchToolConfigParams{Enabled: anthropic.Bool(false)}},
					{OfWebSearch: &anthropic.BetaManagedAgentsWebSearchToolConfigParams{Enabled: anthropic.Bool(false)}},
				},
			}},
			// The node's gate holds the agent to its namespace, so its tools are not asked about again.
			{OfMCPToolset: &anthropic.BetaManagedAgentsMCPToolsetParams{
				MCPServerName: nodeAgentMCP,
				DefaultConfig: anthropic.BetaManagedAgentsMCPToolsetDefaultConfigParams{
					PermissionPolicy: anthropic.BetaManagedAgentsMCPToolsetDefaultConfigParamsPermissionPolicyUnion{OfAlwaysAllow: &anthropic.BetaManagedAgentsAlwaysAllowPolicyParam{}},
				},
			}},
		},
	})
	if err != nil {
		return managedLine{}, errors.Wrapf(err, "no agent %s", name)
	}
	vault, err := client.Beta.Vaults.New(ctx, anthropic.BetaVaultNewParams{DisplayName: name, Metadata: map[string]string{"did": agent.did}})
	if err != nil {
		return managedLine{}, errors.Wrapf(err, "no vault %s", name)
	}
	if _, err := client.Beta.Vaults.Credentials.New(ctx, vault.ID, anthropic.BetaVaultCredentialNewParams{
		DisplayName: anthropic.String(nodeAgentMCP),
		Auth: anthropic.BetaVaultCredentialNewParamsAuthUnion{OfStaticBearer: &anthropic.BetaManagedAgentsStaticBearerCreateParams{
			Token: agent.token, MCPServerURL: mcp,
		}},
	}); err != nil {
		return managedLine{}, errors.Wrapf(err, "the token for %s was not put in vault %s", mcp, vault.ID)
	}
	return managedLine{model: model, agentID: made.ID, agentVersion: made.Version, environment: env.ID, vault: vault.ID}, nil
}

// aManagedAgentHeld is a namespace's agent in Claude Managed Agents as the node
// holds it, and the line that says what it is.
type aManagedAgentHeld struct {
	agent *rootAgent
	line  managedLine
}

// namespaceManagedAgent is the namespace's agent in Claude Managed Agents, when
// the caller acts there and the namespace opted into one there.
func (s *QNTXServer) namespaceManagedAgent(ctx context.Context, sent sigil.Sent) (aManagedAgentHeld, *protocol.Refusal) {
	asked, refused := agentNamespaceOf(ctx, sent)
	return then(asked, refused, func(asked anAsking) (aManagedAgentHeld, *protocol.Refusal) {
		line, set, err := s.managedLineOf(asked.namespace)
		if err != nil {
			return aManagedAgentHeld{}, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		if !set {
			return aManagedAgentHeld{}, &protocol.Refusal{Why: sigil.NotFound, Says: asked.namespace + " opted into no agent in Claude Managed Agents: set one with agents set, harness managed"}
		}
		agent, refused := s.holdNamespaceAgent(asked.namespace, line.model, line.setBy)
		return aManagedAgentHeld{agent: agent, line: line}, refused
	})
}

// managedHarness is Claude Managed Agents as a namespace's agent runs in it.
func (s *QNTXServer) managedHarness(line managedLine) *harness {
	return &harness{
		name: agentLineManaged, called: "Claude Managed Agents", file: managedSessionFile,
		part: func(_ context.Context, _ sigil.Sent, agent *rootAgent, _ agentSpec) (aTurn, *protocol.Refusal) {
			return s.managedPart(agent, line)
		},
	}
}

// managedAm is who the agent is in Claude Managed Agents.
func managedAm(agent *rootAgent, line managedLine, in *inHarness) *protocol.ManagedAm {
	am := &protocol.ManagedAm{
		Did: agent.did, Model: line.model, AgentId: line.agentID, AgentVersion: line.agentVersion,
		EnvironmentId: line.environment, VaultId: line.vault,
	}
	if session, kept, err := agent.sessionIn(managedSessionFile); err != nil {
		am.NotReady = err.Error()
	} else if kept {
		am.Session = session
	}
	if at, kept, err := agent.sessionIn(managedSessionAtAnthropic); err == nil && kept {
		am.ManagedSession = at
	}
	if going := in.answering.Load(); going != nil {
		am.Answering, am.Session = true, going.session
	}
	return am
}

// managedPart is Claude Managed Agents' part of one turn: the session
// Anthropic holds is opened once and continued.
func (s *QNTXServer) managedPart(agent *rootAgent, line managedLine) (aTurn, *protocol.Refusal) {
	client, err := s.anthropicFor(agent, line.federation)
	if err != nil {
		return aTurn{}, &protocol.Refusal{Why: sigil.Failed, Says: "the agent of " + agent.namespace + " has no way to reach Anthropic: " + err.Error()}
	}
	var atAnthropic string
	return aTurn{
		ready: func(ctx context.Context, _ string, _ bool) (bool, *protocol.Refusal) {
			held, kept, err := agent.sessionIn(managedSessionAtAnthropic)
			if err != nil {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
			}
			if kept {
				atAnthropic = held
				return true, nil
			}
			session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
				Agent: anthropic.BetaSessionNewParamsAgentUnion{OfBetaManagedAgentsAgents: &anthropic.BetaManagedAgentsAgentParams{
					ID: line.agentID, Version: anthropic.Int(line.agentVersion),
				}},
				EnvironmentID: line.environment,
				VaultIDs:      []string{line.vault},
				Title:         anthropic.String("the agent of " + agent.namespace),
			})
			if err != nil {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: "Anthropic opened no session for the agent of " + agent.namespace + ": " + err.Error()}
			}
			if err := agent.keepIn(managedSessionAtAnthropic, session.ID); err != nil {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: "Anthropic opened session " + session.ID + ", and it was not kept: " + err.Error()}
			}
			atAnthropic = session.ID
			return false, nil
		},
		run: func(t turnRun) (proto.Message, error) {
			return s.managedTurn(client, atAnthropic, line.model, t)
		},
	}, nil
}

// managedTurn says t.says in session and reads its stream until it is idle,
// writing a row for each tool it reaches for and for how the turn ended.
func (s *QNTXServer) managedTurn(client anthropic.Client, session, model string, t turnRun) (proto.Message, error) {
	ctx, began := s.ctx, time.Now()
	row := func(predicate string, attrs map[string]any) {
		as, err := t.writes.row(predicate, t.now(), attrs)
		t.write([]*types.As{as}, err)
	}
	failed := func(err error) (proto.Message, error) {
		row("StopFailure", map[string]any{"error": "error", "last_assistant_message": err.Error()})
		return nil, err
	}
	if !t.resumes {
		row("SessionStart", map[string]any{"source": "managed", "model": model})
	}
	// The stream is opened before anything is said, so nothing it says is missed.
	stream := client.Beta.Sessions.Events.StreamEvents(ctx, session, anthropic.BetaSessionEventStreamParams{})
	defer stream.Close()
	t.started(session)
	if _, err := client.Beta.Sessions.Events.Send(ctx, session, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{{OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
			Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{OfText: &anthropic.BetaManagedAgentsTextBlockParam{
				Text: t.says,
			}}},
		}}},
	}); err != nil {
		return failed(errors.Wrapf(err, "nothing was said in session %s", session))
	}
	var answer strings.Builder
	stop := ""
events:
	for stream.Next() {
		current := stream.Current()
		// Nobody watches a turn as it runs, so a tool call that asks is denied.
		if current.EvaluatedPermission == anthropic.BetaManagedAgentsAgentEvaluatedPermissionAsk {
			if _, err := client.Beta.Sessions.Events.Send(ctx, session, anthropic.BetaSessionEventSendParams{
				Events: []anthropic.BetaManagedAgentsEventParamsUnion{{OfUserToolConfirmation: &anthropic.BetaManagedAgentsUserToolConfirmationEventParams{
					ToolUseID: current.ID,
					Result: anthropic.BetaManagedAgentsUserToolConfirmationEventParamsResultDeny,
				}}},
			}); err != nil {
				return failed(errors.Wrapf(err, "tool call %s in session %s was not denied", current.ID, session))
			}
		}
		switch event := current.AsAny().(type) {
		case anthropic.BetaManagedAgentsAgentMessageEvent:
			for _, block := range event.Content {
				answer.WriteString(block.Text)
			}
		case anthropic.BetaManagedAgentsAgentToolUseEvent:
			row("PreToolUse", map[string]any{"tool_name": event.Name, "tool_use_id": event.ID})
		case anthropic.BetaManagedAgentsAgentMCPToolUseEvent:
			row("PreToolUse", map[string]any{"tool_name": "mcp__" + event.MCPServerName + "__" + event.Name, "tool_use_id": event.ID})
		case anthropic.BetaManagedAgentsSessionStatusIdleEvent:
			if event.StopReason.Type != "requires_action" {
				stop = event.StopReason.Type
				break events
			}
		case anthropic.BetaManagedAgentsSessionErrorEvent:
			return failed(errors.Newf("session %s: %s", session, event.Error.Message))
		}
	}
	if err := stream.Err(); err != nil {
		return failed(errors.Wrapf(err, "the stream of session %s broke", session))
	}
	row("Stop", map[string]any{"last_assistant_message": answer.String()})
	return &protocol.ManagedSaid{
		Answer: answer.String(), Stop: stop, Session: t.writes.session, ManagedSession: session, Model: model,
		TookMs: float64(time.Since(began).Milliseconds()), Unwritten: t.unwritten(),
	}, nil
}
