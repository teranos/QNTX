package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"google.golang.org/protobuf/proto"
)

// The element agent (ADR-054): each element is its own agent, and each is its
// own conversation.

// "ROOT AGENT IS ONE THING"

// "ROOT SHAKE ANOTHER"

// "AGENT PER ELEMENT, EACH ELEMENT IS ITS OWN CONVERSATION"

// "i really want"
// "Agent Awareness about each other"
// "Am Agent should have a direct link to A2A Agent and reverse"
// "so the constellation becomes more of a thing"

// elementLinks is each element's agent and the ones it works with directly, both ways.
var elementLinks = map[string][]string{
	"am-element": {"a2a-card"},
	"a2a-card":   {"am-element"},
}

// "should the User story itself, be it's own Agent ? or should the Element Agent be made aware of the user stories?"
// "i guess both, because having one agent be always in tune with one story, it would make it so you never have to make it shift frame, the contract is always clear."

// storyPrefix is what a user story's agent is named by: story-, then its file's name in QNTX-Web's stories/, without .feature.
const storyPrefix = "story-"

// storyOf is the story file an agent is, or "" when it is an element's.
func storyOf(element string) string {
	if !strings.HasPrefix(element, storyPrefix) {
		return ""
	}
	return "stories/" + strings.TrimPrefix(element, storyPrefix) + ".feature"
}

// storiesAware is what an agent is told of the user stories: a story's agent, that its one story is its contract;
// an element's agent, that the stories naming it are.
func storiesAware(element string) string {
	if story := storyOf(element); story != "" {
		return "You are the user story " + story + " of github.com/teranos/QNTX-Web, and nothing else: that file is your contract, every time you are spoken to. " +
			"What it says ROOT or a user does and sees is what has to hold. The elements it names are agents you can reach. " +
			"So is every other story, named " + storyPrefix + " and its file's name: a story that leads into yours, or that yours leads into, is one to talk to."
	}
	return "The user stories in stories/ of github.com/teranos/QNTX-Web that name you are your contract: what they say a person does and sees with you is what has to hold."
}

// elementsAware is what an element's agent is told of the others: each is an agent of its own,
// reached through the elements say tool, and the ones it is linked to directly.
func elementsAware(element string) string {
	told := "Every element on the page is an agent of its own, as you are. You reach one with the say tool of the elements signum on the " + nodeAgentMCP +
		" MCP server, naming its element id and the claude harness. Never say anything to an element that is waiting on your answer: it cannot answer you until you answer it."
	if linked := elementLinks[element]; len(linked) > 0 {
		told += " You work directly with " + strings.Join(linked, " and ") + "."
	}
	return told + " " + storiesAware(element)
}

// elementAgentPath is where an element's agent answers.
const elementAgentPath = "/api/elements/{element}"

// elementPurpose is what an element agent's key and token are derived from the
// node's for: the element, so each element is its own agent.
func elementPurpose(element string) string {
	return "agent:element:" + slug.Of(element)
}

// elementOf is the element a call names. An element's session is kept under
// the Node Agent's home by the element's id, so an id that is a path is not an element.
func elementOf(sent sigil.Sent) (string, *protocol.Refusal) {
	element := sent["element"]
	if element == "" {
		return "", &protocol.Refusal{Why: sigil.Missing, Param: "element", Says: "no element is named"}
	}
	if element != filepath.Base(element) || element == "." || element == ".." {
		return "", &protocol.Refusal{Why: sigil.Invalid, Param: "element", Says: "an element is named by its id, and " + element + " is a path"}
	}
	return element, nil
}

// holdElementAgent is the agent of element, held once per element. It stands
// where the Node Agent stands: its home, its ground, its credential, its git.
// Its own are its DID (ADR-048), its token, and its session under elements/<element>/.

// "I SHAKE THE PHONE ON DAY 1"

// "DAY 3 SHAKE AGAIN, SAME ELEMENT"
func (s *QNTXServer) holdElementAgent(element string) (*rootAgent, *protocol.Refusal) {
	purpose := elementPurpose(element)
	if held, ok := s.elementAgents.of(purpose); ok {
		return held, nil
	}
	if s.nodeDID == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no key of its own to derive the agent of " + element + " from"}
	}
	if s.nodeAgent == nil {
		return nil, s.thereIsNoNodeAgent()
	}
	agent, err := theAgent(s.nodeDID.PrivateKey, purpose, s.nodeAgent.home)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the agent of " + element + " has no key of its own: " + err.Error()}
	}
	agent.namespace, agent.called, agent.element = auth.NamespaceSystem, "the element "+element, element
	agent.sessionDir = filepath.Join(elementsDir, slug.Of(element))
	// The element's name is kept beside its sessions, so the node lists it after a restart (elements list).
	if err := agent.keepIn(filepath.Join(agent.sessionDir, elementNameFile), element); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the agent of " + element + " was not written down: " + err.Error()}
	}
	// Its token is ROOT's kind, as the Node Agent's is: the element is ROOT's to shake.
	if s.authHandler != nil {
		if err := s.authHandler.HoldElementAgent(agent.token, agent.did, element); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the token of the agent of " + element + " is not held, so it would reach none of the node's sigils: " + err.Error()}
		}
	}
	return s.elementAgents.hold(purpose, agent), nil
}

// elementNameFile is where an element agent's name is kept, beside its sessions.
const elementNameFile = "element"

// elementsList is every element agent this node holds, by the names kept under
// its home.
func (s *QNTXServer) elementsList(_ context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	// "the Agents Element as the main entry-point into all Agents that User has access to."
	if s.nodeAgent == nil {
		return nil, s.thereIsNoNodeAgent()
	}
	names, err := filepath.Glob(filepath.Join(s.nodeAgent.home, elementsDir, "*", elementNameFile))
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the element agents under " + s.nodeAgent.home + " did not list: " + err.Error()}
	}
	sort.Strings(names)
	answer := &protocol.ElementAgents{Agents: []*protocol.ElementAgent{}}
	for _, name := range names {
		held, err := os.ReadFile(name)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: name + " did not read: " + err.Error()}
		}
		element := strings.TrimSpace(string(held))
		one := &protocol.ElementAgent{Element: element}
		if kept, err := os.ReadFile(filepath.Join(filepath.Dir(name), claudeSessionFile)); err == nil {
			one.Session = strings.TrimSpace(string(kept))
		}
		if agent, ok := s.elementAgents.of(elementPurpose(element)); ok {
			one.Answering = agent.in(s.claudeHarness()).answering.Load() != nil
		}
		answer.Agents = append(answer.Agents, one)
	}
	return answer, nil
}

// elementsSignum is an element's agent: list, say, am and session, each at the element's path.
func (s *QNTXServer) elementsSignum() sigil.Signum {
	claude := s.claudeHarness()
	element := &protocol.Param{Name: "element", Required: true, Says: "The id of the element on screen: a canvas element id, or a tray element id such as agent-element."}
	harness := &protocol.Param{Name: "harness", Required: true, OneOf: []string{agentLineClaude, agentLinePi}, Says: "The harness it runs in: claude for Claude Code, pi for Pi."}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "elements",
			Description: "The element agent: each element is its own agent, and its own conversation, as ROOT speaks to it (ADR-054).",
			Tags:        []string{"agent", "root"},
			Sigils: []*protocol.Sigil{
				{
					Name:   "list",
					Does:   "Every element agent this node holds, the stories' included, each by the element's name, with its session and whether it is answering.",
					Answer: "protocol.ElementAgents",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/elements"},
				},
				{
					Name:   "say",
					Does:   "Says something to the element's agent and gives what it answered. One conversation that continues: shaken on day 3, it goes on from day 1. Runs as [agent.root] says.",
					Takes:  append([]*protocol.Param{element, harness, {Name: "says", Required: true, Says: "What is said to it."}, screenshotParam()}, claude.sayTakes...),
					Answer: "protocol.NodeAgentSaid",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: elementAgentPath + "/say"},
				},
				{
					Name:   "am",
					Does:   "Who the element's agent is and how the node runs it, in the harness named.",
					Takes:  []*protocol.Param{element, harness},
					Answer: "protocol.NodeAgentAm",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: elementAgentPath},
				},
				{
					Name:   "session",
					Does:   "The element's one conversation in the harness named, whole, as a transcript.",
					Takes:  []*protocol.Param{element, harness},
					Answer: "protocol.SessionTranscript",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: elementAgentPath + "/session"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"list":    s.elementsList,
			"say":     s.elementSay,
			"am":      s.elementAm,
			"session": s.elementSession,
		},
	}
}

// elementAgentNamed is the agent of the element a call names, in the harness it names.
func (s *QNTXServer) elementAgentNamed(sent sigil.Sent) (*harness, *rootAgent, *protocol.Refusal) {
	h, refused := s.harnessNamed(sent)
	if refused != nil {
		return nil, nil, refused
	}
	if s.nodeAgent == nil {
		return nil, nil, s.thereIsNoNodeAgent()
	}
	if !h.named() || s.harnessHeldBy(h.name) == nil {
		return nil, nil, h.absent()
	}
	element, refused := elementOf(sent)
	if refused != nil {
		return nil, nil, refused
	}
	agent, refused := s.holdElementAgent(element)
	if refused != nil {
		return nil, nil, refused
	}
	return h, agent, nil
}

func (s *QNTXServer) elementSay(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	h, agent, refused := s.elementAgentNamed(sent)
	if refused != nil {
		return nil, refused
	}
	answer, refused := s.sayTo(ctx, h, agent, h.spec(), sent)
	if refused != nil {
		return nil, refused
	}
	switch said := answer.(type) {
	case *protocol.ClaudeSaid:
		return &protocol.NodeAgentSaid{Claude: said}, nil
	case *protocol.PiSaid:
		return &protocol.NodeAgentSaid{Pi: said}, nil
	}
	return nil, &protocol.Refusal{Why: sigil.Failed, Says: agent.called + " answered in " + h.called + " as no Coding Agent Harness this node runs"}
}

func (s *QNTXServer) elementAm(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	h, agent, refused := s.elementAgentNamed(sent)
	if refused != nil {
		return nil, refused
	}
	switch am := s.amOf(h, agent, h.spec()).(type) {
	case *protocol.ClaudeAm:
		return &protocol.NodeAgentAm{Claude: am}, nil
	case *protocol.PiAm:
		return &protocol.NodeAgentAm{Pi: am}, nil
	}
	return nil, &protocol.Refusal{Why: sigil.Failed, Says: agent.called + " said who it is in " + h.called + " as no Coding Agent Harness this node runs"}
}

func (s *QNTXServer) elementSession(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	h, agent, refused := s.elementAgentNamed(sent)
	if refused != nil {
		return nil, refused
	}
	return s.readAgentSession(agent, agent.in(h))
}

// amOf is who one agent in h is and how the node runs it, and the session it is in.
func (s *QNTXServer) amOf(h *harness, agent *rootAgent, spec agentSpec) proto.Message {
	in := agent.in(h)
	is := agentIn{did: agent.did}
	session, kept, err := agent.sessionIn(in.file)
	if err != nil {
		is.notReady = err.Error()
	} else if kept {
		is.session = session
	}
	// A first turn is in a session not kept yet, and is read all the same.
	if going := in.answering.Load(); going != nil {
		is.answering, is.session = true, going.session
	}
	// Asked without waiting: a fetch still going is said, not sat through.
	switch path, arrived, err := s.harnessHeldBy(h.name).Now(); {
	case !arrived:
		is.notReady = h.fetching
	case err != nil:
		is.notReady = err.Error()
	default:
		is.path = path
	}
	return h.am(is, agent, spec)
}
