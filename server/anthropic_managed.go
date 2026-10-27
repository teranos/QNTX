package server

// An element agent run at Anthropic, as one of the Console's Agents (QNTX-Web
// stories/31-root-shakes-an-element-agent-at-anthropic.feature).
// "so we can have an agent, that isnt hosted on the same box as QNTX"
// "if we use their infra, we use their infra"

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// What the node keeps of the Console's objects: the Environment every element
// shares, in the Node Agent's home, and each element's own beside its session.
const (
	managedEnvironmentFile = "anthropic-environment"
	managedElementFile     = "anthropic-managed.json"
)

// managedOf is an element's own at the Console: its Agent, the Vault that
// carries its token to the node's MCP, and its one Session.
type managedOf struct {
	Agent   string `json:"agent"`
	Vault   string `json:"vault"`
	Session string `json:"session"`
}

// anthropicClient is the node at Anthropic on its own identity, at the rule ROOT gave.
func (s *QNTXServer) anthropicClient(kept anthropicKept) (anthropic.Client, error) {
	i, err := s.theIssuer()
	if err != nil {
		return anthropic.Client{}, err
	}
	base := s.anthropicBaseURL
	if base == "" {
		base = anthropicAPI
	}
	return anthropic.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(base),
		option.WithFederationTokenProvider(i.identityTokenOf(s.nodeDID.DID, auth.NamespaceSystem), option.FederationOptions{
			FederationRuleID: kept.ids["federation_rule_id"], OrganizationID: kept.ids["organization_id"],
			ServiceAccountID: kept.ids["service_account_id"], WorkspaceID: kept.ids["workspace_id"],
		}),
	), nil
}

// managedIsSaidToBe is what an element's agent at Anthropic is told it is.
func managedIsSaidToBe(a *rootAgent) string {
	return "You are the element " + a.element + " of a QNTX node's page, as ROOT speaks to it: one element, one conversation, continued every time ROOT shakes the phone with you on screen. " +
		"ROOT says what they see; what you do about it is the prompt. " +
		"You run at Anthropic, in a container of your own, not on the machine the node runs on. " +
		"Your DID is " + a.did + ". " +
		"The node's sigils are the tools of the MCP server named " + nodeAgentMCP + ", which you reach with your own token."
}

// runManaged says said to the element's agent at Anthropic, and reads its answer
// from the Session's events until the Session is idle again.
func (s *QNTXServer) runManaged(agent *rootAgent, said claudecode.Said, kept anthropicKept, each func(claudecode.Message)) (claudecode.Answer, error) {
	if s.ownURL == "" {
		return claudecode.Answer{}, errors.New("the node knows no URL of its own, so an agent at Anthropic has no MCP to reach it by")
	}
	client, err := s.anthropicClient(kept)
	if err != nil {
		return claudecode.Answer{}, err
	}
	ctx := s.ctx
	environment, err := s.managedEnvironment(ctx, client)
	if err != nil {
		return claudecode.Answer{}, err
	}
	held, err := s.managedElement(ctx, client, agent, said.Model, environment)
	if err != nil {
		return claudecode.Answer{}, err
	}

	// The stream is opened before the message is sent: it has no replay.
	stream := client.Beta.Sessions.Events.StreamEvents(ctx, held.Session, anthropic.BetaSessionEventStreamParams{})
	defer stream.Close()
	if _, err := client.Beta.Sessions.Events.Send(ctx, held.Session, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{{OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{
			Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{OfText: &anthropic.BetaManagedAgentsTextBlockParam{Text: said.Says}}},
		}}},
	}); err != nil {
		return claudecode.Answer{}, errors.Wrapf(err, "what was said did not reach session %s at Anthropic", held.Session)
	}

	var text, failed []string
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "agent.message":
			for _, block := range event.AsAgentMessage().Content {
				if block.Type == "text" {
					text = append(text, block.Text)
				}
			}
		case "session.error":
			failed = append(failed, event.RawJSON())
		case "session.status_idle", "session.status_terminated":
			// One message comes as blocks cut wherever a citation sits: they are read back to back.
			answer := claudecode.Answer{Text: strings.Join(text, ""), Session: held.Session, Model: said.Model}
			if len(text) == 0 && len(failed) > 0 {
				answer.Text, answer.IsError = strings.Join(failed, "\n"), true
			}
			each(claudecode.Message{Type: "result", Subtype: "success", IsError: answer.IsError, Result: answer.Text})
			return answer, nil
		}
	}
	if err := stream.Err(); err != nil {
		return claudecode.Answer{}, errors.Wrapf(err, "the events of session %s at Anthropic stopped before it was idle", held.Session)
	}
	return claudecode.Answer{}, errors.Newf("the events of session %s at Anthropic ended before it was idle", held.Session)
}

// managedEnvironment is the cloud Environment every element's agent runs in, made once.
func (s *QNTXServer) managedEnvironment(ctx context.Context, client anthropic.Client) (string, error) {
	file := filepath.Join(s.nodeAgent.home, managedEnvironmentFile)
	if held, err := os.ReadFile(file); err == nil {
		return strings.TrimSpace(string(held)), nil
	} else if !os.IsNotExist(err) {
		return "", errors.Wrapf(err, "the Environment at Anthropic kept in %s did not read", file)
	}
	made, err := client.Beta.Environments.New(ctx, anthropic.BetaEnvironmentNewParams{
		Name: "qntx-element-agents",
		Config: anthropic.BetaEnvironmentNewParamsConfigUnion{OfCloud: &anthropic.BetaCloudConfigParams{
			Networking: anthropic.BetaCloudConfigParamsNetworkingUnion{OfUnrestricted: &anthropic.BetaUnrestrictedNetworkParam{}},
		}},
	})
	if err != nil {
		return "", errors.Wrap(err, "no Environment was made at Anthropic for the node's element agents")
	}
	if err := os.WriteFile(file, []byte(made.ID), 0o600); err != nil {
		return "", errors.Wrapf(err, "the Environment %s made at Anthropic was not kept in %s", made.ID, file)
	}
	s.logger.Infow("an Environment was made at Anthropic for the node's element agents", "environment", made.ID)
	return made.ID, nil
}

// managedElement is the element's Agent, Vault and Session at Anthropic, each made once and kept.
func (s *QNTXServer) managedElement(ctx context.Context, client anthropic.Client, agent *rootAgent, model, environment string) (managedOf, error) {
	file := filepath.Join(agent.home, agent.sessionDir, managedElementFile)
	var held managedOf
	if raw, err := os.ReadFile(file); err == nil {
		if err := json.Unmarshal(raw, &held); err != nil {
			return managedOf{}, errors.Wrapf(err, "what the element %s keeps of Anthropic in %s did not read", agent.element, file)
		}
	} else if !os.IsNotExist(err) {
		return managedOf{}, errors.Wrapf(err, "what the element %s keeps of Anthropic in %s did not read", agent.element, file)
	}
	keep := func() error {
		raw, err := json.Marshal(held)
		if err != nil {
			return errors.Wrapf(err, "what the element %s has at Anthropic did not write", agent.element)
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return errors.Wrapf(err, "could not create %s", filepath.Dir(file))
		}
		return errors.Wrapf(os.WriteFile(file, raw, 0o600), "what the element %s has at Anthropic was not kept in %s", agent.element, file)
	}
	mcp := s.ownURL + "/mcp"
	if held.Agent == "" {
		made, err := client.Beta.Agents.New(ctx, anthropic.BetaAgentNewParams{
			Name:       "qntx-element-" + agent.element,
			Model:      anthropic.BetaManagedAgentsModelConfigParams{ID: anthropic.BetaManagedAgentsModel(model)},
			System:     anthropic.String(managedIsSaidToBe(agent)),
			MCPServers: []anthropic.BetaManagedAgentsURLMCPServerParams{{Name: nodeAgentMCP, URL: mcp}},
			Tools: []anthropic.BetaAgentNewParamsToolUnion{
				{OfAgentToolset20260401: &anthropic.BetaManagedAgentsAgentToolset20260401Params{}},
				// An MCP toolset asks before each call by default, and nobody is there to answer.
				{OfMCPToolset: &anthropic.BetaManagedAgentsMCPToolsetParams{
					MCPServerName: nodeAgentMCP,
					DefaultConfig: anthropic.BetaManagedAgentsMCPToolsetDefaultConfigParams{
						PermissionPolicy: anthropic.BetaManagedAgentsMCPToolsetDefaultConfigParamsPermissionPolicyUnion{OfAlwaysAllow: &anthropic.BetaManagedAgentsAlwaysAllowPolicyParam{}},
					},
				}},
			},
		})
		if err != nil {
			return managedOf{}, errors.Wrapf(err, "no Agent was made at Anthropic for the element %s", agent.element)
		}
		held.Agent = made.ID
		if err := keep(); err != nil {
			return managedOf{}, err
		}
	}
	if held.Vault == "" {
		vault, err := client.Beta.Vaults.New(ctx, anthropic.BetaVaultNewParams{DisplayName: "qntx-element-" + agent.element})
		if err != nil {
			return managedOf{}, errors.Wrapf(err, "no Vault was made at Anthropic for the element %s", agent.element)
		}
		if _, err := client.Beta.Vaults.Credentials.New(ctx, vault.ID, anthropic.BetaVaultCredentialNewParams{
			Auth: anthropic.BetaVaultCredentialNewParamsAuthUnion{OfStaticBearer: &anthropic.BetaManagedAgentsStaticBearerCreateParams{Token: agent.token, MCPServerURL: mcp}},
		}); err != nil {
			return managedOf{}, errors.Wrapf(err, "the element %s's token for %s was not put in Vault %s", agent.element, mcp, vault.ID)
		}
		held.Vault = vault.ID
		if err := keep(); err != nil {
			return managedOf{}, err
		}
	}
	// How long Anthropic keeps a Session is not said: one that has ended is said, and a new one is made.
	if held.Session != "" {
		session, err := client.Beta.Sessions.Get(ctx, held.Session, anthropic.BetaSessionGetParams{})
		if err != nil {
			return managedOf{}, errors.Wrapf(err, "the element %s's Session %s at Anthropic did not answer", agent.element, held.Session)
		}
		if session.Status == "terminated" {
			s.logger.Infow("the element's Session at Anthropic had ended, and a new one is made", "element", agent.element, "ended", held.Session)
			held.Session = ""
		}
	}
	if held.Session == "" {
		session, err := client.Beta.Sessions.New(ctx, anthropic.BetaSessionNewParams{
			Agent:         anthropic.BetaSessionNewParamsAgentUnion{OfString: anthropic.String(held.Agent)},
			EnvironmentID: environment,
			Title:         anthropic.String("the element " + agent.element),
			VaultIDs:      []string{held.Vault},
		})
		if err != nil {
			return managedOf{}, errors.Wrapf(err, "no Session was made at Anthropic for the element %s", agent.element)
		}
		held.Session = session.ID
		if err := keep(); err != nil {
			return managedOf{}, err
		}
		s.logger.Infow("the element's agent runs at Anthropic", "element", agent.element, "agent", held.Agent, "vault", held.Vault, "session", held.Session)
	}
	return held, nil
}
