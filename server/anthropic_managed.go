package server

// An element agent run at Anthropic, as one of the Console's Agents (QNTX-Web
// stories/31-root-shakes-an-element-agent-at-anthropic.feature).
// "so we can have an agent, that isnt hosted on the same box as QNTX"
// "if we use their infra, we use their infra"

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
// carries its token to the node's MCP, the Memory it keeps across Sessions, and its Session.
type managedOf struct {
	Agent   string `json:"agent"`
	Vault   string `json:"vault"`
	Memory  string `json:"memory"`
	Session string `json:"session"`
	// Spent is what Session had cost by the end of the last turn, in USD: a turn's cost is what it adds.
	Spent float64 `json:"spent"`
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
		"The node's sigils are the tools of the MCP server named " + nodeAgentMCP + ", which you reach with your own token. " +
		"What ROOT has said about you is quoted in your source's comments, and in the commit messages of github.com/teranos/QNTX-Web and github.com/teranos/QNTX that touch your files: " +
		"ROOT's words are the quoted lines, and a commit's session: line names the conversation they were said in."
}

// runManaged says said to the element's agent at Anthropic, and reads its answer
// from the Session's events until the Session is idle again.
func (s *QNTXServer) runManaged(agent *rootAgent, said claudecode.Said, shot shaken, kept anthropicKept, each func(claudecode.Message)) (claudecode.Answer, error) {
	if s.authHandler == nil {
		return claudecode.Answer{}, errors.New("this node runs without auth, so it has no public origin an agent at Anthropic could reach its MCP by")
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
	content, err := shot.content(said.Says)
	if err != nil {
		return claudecode.Answer{}, err
	}
	if _, err := client.Beta.Sessions.Events.Send(ctx, held.Session, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{{OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{Content: content}}},
	}); err != nil {
		return claudecode.Answer{}, errors.Wrapf(err, "what was said did not reach session %s at Anthropic", held.Session)
	}

	var text, failed []string
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		// What it reaches for is shown as it happens, as a Claude Code turn shows it (claude_turns.go).
		case "agent.tool_use", "agent.mcp_tool_use":
			name, input := event.Name, event.Input
			if event.Type == "agent.mcp_tool_use" {
				name = "mcp__" + event.MCPServerName + "__" + event.Name
			}
			if reached, err := reachedFor(event.ID, name, input); err == nil {
				each(reached)
			} else {
				s.logger.Errorw("what an agent at Anthropic reached for was not shown", "element", agent.element, "tool", name, "error", err)
			}
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
			if spent, err := s.spentBy(ctx, client, agent, &held); err != nil {
				s.logger.Errorw("what the turn cost at Anthropic was not read", "element", agent.element, "session", held.Session, "error", err)
			} else {
				answer.CostUSD = spent
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

// reachedFor is a tool an agent at Anthropic reached for, as a Claude Code stream says one.
func reachedFor(id, name string, input any) (claudecode.Message, error) {
	blocks, err := json.Marshal([]map[string]any{{"type": "tool_use", "id": id, "name": name, "input": input}})
	if err != nil {
		return claudecode.Message{}, errors.Wrapf(err, "the call to %s did not write", name)
	}
	m := claudecode.Message{Type: "assistant"}
	m.Message = &struct {
		Content json.RawMessage `json:"content"`
	}{Content: blocks}
	return m, nil
}

// spentBy is what the turn just ended cost: what its Session has cost now, less what it had before.
func (s *QNTXServer) spentBy(ctx context.Context, client anthropic.Client, agent *rootAgent, held *managedOf) (float64, error) {
	session, err := client.Beta.Sessions.Get(ctx, held.Session, anthropic.BetaSessionGetParams{})
	if err != nil {
		return 0, errors.Wrapf(err, "session %s at Anthropic did not answer", held.Session)
	}
	total, err := strconv.ParseFloat(session.Usage.ListCost.Amount, 64)
	if err != nil {
		return 0, errors.Wrapf(err, "session %s at Anthropic said it cost %q", held.Session, session.Usage.ListCost.Amount)
	}
	spent := total - held.Spent
	held.Spent = total
	return spent, keepManaged(agent, *held)
}

// keepManaged writes down what the element has at Anthropic, beside its session.
func keepManaged(agent *rootAgent, held managedOf) error {
	file := filepath.Join(agent.home, agent.sessionDir, managedElementFile)
	raw, err := json.Marshal(held)
	if err != nil {
		return errors.Wrapf(err, "what the element %s has at Anthropic did not write", agent.element)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return errors.Wrapf(err, "could not create %s", filepath.Dir(file))
	}
	return errors.Wrapf(os.WriteFile(file, raw, 0o600), "what the element %s has at Anthropic was not kept in %s", agent.element, file)
}

// shaken is what ROOT said, the element it was said in, and where its screenshot was put on the node.
type shaken struct {
	says, element, screenshot string
}

// content is what is said to an agent at Anthropic: the words, and the screenshot itself, since a path
// on the node is nothing it can open. With no screenshot, it is what the harness was told.
func (s shaken) content(told string) ([]anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion, error) {
	if s.screenshot == "" {
		return []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{OfText: &anthropic.BetaManagedAgentsTextBlockParam{Text: told}}}, nil
	}
	picture, err := os.ReadFile(s.screenshot)
	if err != nil {
		return nil, errors.Wrapf(err, "the screenshot of element %s at %s did not read", s.element, s.screenshot)
	}
	return []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{
		{OfText: &anthropic.BetaManagedAgentsTextBlockParam{Text: s.says + "\n\nThe screenshot of element " + s.element + " is with this message."}},
		{OfImage: &anthropic.BetaManagedAgentsImageBlockParam{Source: anthropic.BetaManagedAgentsImageBlockSourceUnionParam{
			OfBase64: &anthropic.BetaManagedAgentsBase64ImageSourceParam{Data: base64.StdEncoding.EncodeToString(picture), MediaType: http.DetectContentType(picture)},
		}}},
	}, nil
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
	keep := func() error { return keepManaged(agent, held) }
	// Anthropic reaches the node from outside: at its public origin, not the address it serves on.
	mcp := s.authHandler.PublicOrigin() + "/mcp"
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
	// A Session that ends takes what the agent learned with it; its Memory does not.
	if held.Memory == "" {
		memory, err := client.Beta.MemoryStores.New(ctx, anthropic.BetaMemoryStoreNewParams{
			Name:        "qntx-element-" + agent.element,
			Description: anthropic.String("What you, the element " + agent.element + ", learned in your conversations with ROOT, kept for every conversation after."),
		})
		if err != nil {
			return managedOf{}, errors.Wrapf(err, "no Memory was made at Anthropic for the element %s", agent.element)
		}
		held.Memory = memory.ID
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
			Resources:     []anthropic.BetaSessionNewParamsResourceUnion{{OfMemoryStore: &anthropic.BetaManagedAgentsMemoryStoreResourceParam{MemoryStoreID: held.Memory}}},
		})
		if err != nil {
			return managedOf{}, errors.Wrapf(err, "no Session was made at Anthropic for the element %s", agent.element)
		}
		held.Session, held.Spent = session.ID, 0
		if err := keep(); err != nil {
			return managedOf{}, err
		}
		s.logger.Infow("the element's agent runs at Anthropic", "element", agent.element, "agent", held.Agent, "vault", held.Vault, "memory", held.Memory, "session", held.Session)
	}
	return held, nil
}
