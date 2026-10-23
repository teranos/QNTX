package server

// The namespace agent (ADR-048).
//
// "an agent session for a particular namespace, different concept from the
// ROOT host agent"
//
// "The Namespace agent is a common Agent of the Namespace it's opted into. That
// means the one who sets it up knows it's shared amongst anyone who has REACH
// on it."
//
// "The one who set's it up does, they provide their own Subscription or API
// key"
//
// It is an agent the node hosts, standing in its namespace: its own DID derived
// from the node's key for that namespace and model, its own home, session and
// token, signed in through claude login, and its session written in the
// namespace, where nothing crosses (ADR-026). Which model it is and how it runs
// is a line in the namespace's own store: node and namespace configuration are
// different things (ADR-051), so nothing of it is in am.toml.

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/internal/pi"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/proto"
)

// An AGENT line in a namespace says which agent the namespace opted into; its
// predicate is the harness, and the newest line per harness holds.
const (
	agentLineSubject = "AGENT"
	agentLineClaude  = "claude"
	agentLinePi      = "pi"
)

// agentPurpose is what a namespace agent's key and token are derived from the
// node's for: the namespace and the model, so two models are two agents, and
// so is one model in two places.
func agentPurpose(namespace, model string) string {
	return "agent:" + slug.Of(namespace) + ":" + model
}

// agentHome is where the node keeps a namespace's agent: beside ROOT's, by the
// namespace and the model. Where agents are kept is set with the Node Agent
// (nameNodeAgent), and a node that set none keeps them nowhere.
func (s *QNTXServer) agentHome(namespace, model string) (string, error) {
	if s.agentsDir == "" {
		return "", errors.Newf("this node keeps agents nowhere: am.toml names no [agent.root], which is what sets where they are kept")
	}
	return filepath.Join(s.agentsDir, slug.Of(namespace), slug.Of(model)), nil
}

// agentLine is a namespace's agent as its newest AGENT line says it.
type agentLine struct {
	spec  agentSpec
	setBy string
	at    time.Time
}

// agentLineOf reads the newest AGENT line of the namespace. False is a
// namespace that opted into no agent.
func (s *QNTXServer) agentLineOf(namespace string) (agentLine, bool, error) {
	newest, setBy, held, err := s.newestAgentLine(namespace, agentLineClaude)
	if err != nil || !held {
		return agentLine{}, held, err
	}
	line := agentLine{at: newest.Timestamp, setBy: setBy}
	for name, into := range map[string]*string{"model": &line.spec.Model, "effort": &line.spec.Effort, "permission_mode": &line.spec.Mode} {
		if *into, err = oneText(newest.Attributes, name, agentLineSubject, newest.ID, namespace); err != nil {
			return agentLine{}, false, err
		}
	}
	if line.spec.Allow, err = texts(newest.Attributes, "allow", agentLineSubject, newest.ID, namespace); err != nil {
		return agentLine{}, false, err
	}
	return line, true, nil
}

// newestAgentLine is the newest AGENT line of the namespace in harness, and
// who set it. False is a namespace that opted into no agent there.
func (s *QNTXServer) newestAgentLine(namespace, harness string) (newestLine *types.As, setBy string, held bool, err error) {
	found, err := s.agentLinesIn(namespace)
	if err != nil {
		return nil, "", false, err
	}
	var newest *types.As
	for _, as := range found {
		if !slices.Contains(as.Predicates, harness) {
			continue
		}
		if !held || as.Timestamp.After(newest.Timestamp) {
			newest, held = as, true
		}
	}
	if !held {
		return nil, "", false, nil
	}
	setters, err := named.ItemsOf(agentLineSubject+" line "+newest.ID+" of "+namespace+", who set it,", newest.Actors)
	if err != nil {
		return nil, "", false, err
	}
	return newest, setters.First(), true, nil
}

// piLine is a namespace's agent in Pi as its newest AGENT line says it: the
// model, the thinking level, and the provider whose key of the namespace its
// model calls are made on.
type piLine struct {
	model, thinking, provider, setBy string
}

// piLineOf reads the newest AGENT line of the namespace in Pi. False is a
// namespace that opted into no agent there.
func (s *QNTXServer) piLineOf(namespace string) (piLine, bool, error) {
	newest, setBy, held, err := s.newestAgentLine(namespace, agentLinePi)
	if err != nil || !held {
		return piLine{}, held, err
	}
	line := piLine{setBy: setBy}
	for name, into := range map[string]*string{"model": &line.model, "thinking": &line.thinking, "provider": &line.provider} {
		if *into, err = oneText(newest.Attributes, name, agentLineSubject, newest.ID, namespace); err != nil {
			return piLine{}, false, err
		}
	}
	return line, true, nil
}

// piProviders is each provider a namespace's agent in Pi makes its model
// calls at, and the variable Pi reads that provider's key from: the key of
// the namespace by the provider's name (ADR-051).
var piProviders = map[string]string{
	"anthropic":  "ANTHROPIC_API_KEY",
	"openai":     "OPENAI_API_KEY",
	"openrouter": "OPENROUTER_API_KEY",
}

// piProviderNames is the providers by name, for a sigil to offer.
func piProviderNames() []string {
	names := make([]string, 0, len(piProviders))
	for name := range piProviders {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// agentLinesIn is every AGENT line of the namespace, newest or not.
func (s *QNTXServer) agentLinesIn(namespace string) ([]*types.As, error) {
	reading, err := s.held.Read(namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s line of %s", agentLineSubject, namespace)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Subjects: []string{agentLineSubject}, Limit: ats.EveryRow})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s line of %s", agentLineSubject, namespace)
	}
	return found, nil
}

// optedIntoAgent is whether the namespace opted into an agent: it holds an
// AGENT line for one.
func (s *QNTXServer) optedIntoAgent(namespace string) (bool, error) {
	found, err := s.agentLinesIn(namespace)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(found, func(as *types.As) bool { return slices.Contains(as.Predicates, agentLineClaude) }), nil
}

// textOf is a line's attribute that is one text, and a line without it is a
// line that does not read.
func oneText(as map[string]any, attribute, subject, id, name string) (string, error) {
	held, ok := as[attribute].(string)
	if !ok {
		return "", errors.Newf("%s line %s of %s: %s is %v, not text", subject, id, name, attribute, as[attribute])
	}
	return held, nil
}

// setAgentLine writes the namespace's agent in harness whole, as whoever set it.
func (s *QNTXServer) setAgentLine(admitted auth.Admission, namespace, harness string, attributes map[string]any) error {
	store, err := s.held.Write(admitted, namespace)
	if err != nil {
		return errors.Wrapf(err, "no store to write the %s line of %s in", agentLineSubject, namespace)
	}
	id, err := identity.GenerateASUIDWithRetry("AS", agentLineSubject, harness, namespace, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line of %s", agentLineSubject, namespace)
	}
	at := time.Now()
	return errors.Wrapf(store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{agentLineSubject}, Predicates: []string{harness}, Contexts: []string{namespace},
		Actors: []string{admitted.Identity}, Timestamp: at, CreatedAt: at, Source: "agent",
		Attributes: attributes,
	}), "the %s line of %s in %s was not written", agentLineSubject, namespace, harness)
}

// claudeLine is what the AGENT line of an agent in Claude Code says.
func claudeLine(spec agentSpec) map[string]any {
	return map[string]any{"model": spec.Model, "effort": spec.Effort, "permission_mode": spec.Mode, "allow": anyOf(spec.Allow)}
}

// anAsking is one call on the agents signum: who asked, and the namespace they
// named and act in.
type anAsking struct {
	admitted  auth.Admission
	namespace string
}

// agentNamespaceOf is the namespace a call names, when the caller acts in it.
// A namespace the caller does not act in is one that does not exist to them.
func agentNamespaceOf(ctx context.Context, sent sigil.Sent) (anAsking, *protocol.Refusal) {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return anAsking{}, &protocol.Refusal{Why: sigil.Failed, Says: "a namespace's agent is asked for through the gate, and this asking came through none"}
	}
	namespace := sent["namespace"]
	if namespace == "" {
		return anAsking{}, &protocol.Refusal{Why: sigil.Missing, Param: "namespace", Says: "namespace is required"}
	}
	if namespace == auth.NamespaceSystem {
		return anAsking{}, &protocol.Refusal{Why: sigil.NotAllowed, Param: "namespace", Says: "system has no agent of its own: the Node Agent is the node's (claude say)"}
	}
	if !admitted.MayActIn(namespace) {
		return anAsking{}, &protocol.Refusal{Why: sigil.NotFound, Param: "namespace", Says: "no namespace " + namespace + " that you act in"}
	}
	return anAsking{admitted: admitted, namespace: namespace}, nil
}

// anAgentHeld is a namespace's agent as the node holds it, and the line that
// says what it is.
type anAgentHeld struct {
	agent *rootAgent
	line  agentLine
}

// namespaceAgent is the namespace's agent, when the caller acts there and the
// namespace opted into one: held once per namespace and model, its token held
// where the gate reads tokens.
func (s *QNTXServer) namespaceAgent(ctx context.Context, sent sigil.Sent) (anAgentHeld, *protocol.Refusal) {
	asked, refused := agentNamespaceOf(ctx, sent)
	if refused != nil {
		return anAgentHeld{}, refused
	}
	namespace := asked.namespace
	line, set, err := s.agentLineOf(namespace)
	if err != nil {
		return anAgentHeld{}, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if !set {
		return anAgentHeld{}, &protocol.Refusal{Why: sigil.NotFound, Says: namespace + " opted into no agent: set one with agents set"}
	}
	if s.harnessHeldBy("claude") == nil {
		return anAgentHeld{}, &protocol.Refusal{Why: sigil.Failed, Says: "this node holds no Claude Code to run the agent of " + namespace + " in: am.toml names no [agent.root], which is what fetches it"}
	}
	agent, refused := s.holdNamespaceAgent(namespace, line.spec.Model, line.setBy)
	return anAgentHeld{agent: agent, line: line}, refused
}

// aPiAgentHeld is a namespace's agent in Pi as the node holds it, and the
// line that says what it is.
type aPiAgentHeld struct {
	agent *rootAgent
	line  piLine
}

// namespacePiAgent is the namespace's agent in Pi, when the caller acts there
// and the namespace opted into one there: "I want Pi to also serv the function
// of namespace agent alongside Claude code namespace agent".
func (s *QNTXServer) namespacePiAgent(ctx context.Context, sent sigil.Sent) (aPiAgentHeld, *protocol.Refusal) {
	asked, refused := agentNamespaceOf(ctx, sent)
	return then(asked, refused, func(asked anAsking) (aPiAgentHeld, *protocol.Refusal) {
		namespace := asked.namespace
		line, set, err := s.piLineOf(namespace)
		if err != nil {
			return aPiAgentHeld{}, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		if !set {
			return aPiAgentHeld{}, &protocol.Refusal{Why: sigil.NotFound, Says: namespace + " opted into no agent in Pi: set one with agents set, harness pi"}
		}
		if s.harnessHeldBy("pi") == nil {
			return aPiAgentHeld{}, piIsOff()
		}
		// The node holds Pi wherever it keeps agents (agentHome), so an agent
		// it can hold is one it can run.
		agent, refused := s.holdNamespaceAgent(namespace, line.model, line.setBy)
		return aPiAgentHeld{agent: agent, line: line}, refused
	})
}

// then is what does makes of held, once nothing refused it: the refusal that
// came first, or what does answers.
func then[T, U any](held T, refused *protocol.Refusal, does func(T) (U, *protocol.Refusal)) (U, *protocol.Refusal) {
	if refused != nil {
		var none U
		return none, refused
	}
	return does(held)
}

// holdNamespaceAgent is the agent of namespace that model is, set by setBy:
// held once per namespace and model, its token held where the gate reads
// tokens.
func (s *QNTXServer) holdNamespaceAgent(namespace, model, setBy string) (*rootAgent, *protocol.Refusal) {
	purpose := agentPurpose(namespace, model)
	if held, ok := s.namespaceAgents.of(purpose); ok {
		return held, nil
	}
	if s.nodeDID == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no key of its own to derive the agent of " + namespace + " from"}
	}
	home, err := s.agentHome(namespace, model)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	agent, err := theAgent(s.nodeDID.PrivateKey, purpose, home)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the agent of " + namespace + " has no key of its own: " + err.Error()}
	}
	agent.namespace, agent.called = namespace, "the agent of "+namespace
	// Its token is what keeps it in its namespace at the gate. A node without
	// auth has no gate to hold it, and would admit it as the node's one caller,
	// everywhere: it runs no namespace agent.
	if s.authHandler == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node runs without auth, so the agent of " + namespace + " would have no token keeping it in " + namespace + ": it runs none"}
	}
	if err := s.authHandler.HoldNamespaceAgent(agent.token, agent.did, purpose, setBy, namespace); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the token of the agent of " + namespace + " is not held, so it would reach none of the node's sigils: " + err.Error()}
	}
	held := s.namespaceAgents.hold(purpose, agent)
	// It runs under ground as the Node Agent does: its ground built in its home (ADR-050).
	if s.underGround.add(held.home) {
		s.groundAgent(held.home)
	}
	return held, nil
}

// agentsHeld is each namespace agent the node has held, by its purpose: one
// per namespace and model, however many ask for it at once.
type agentsHeld struct {
	mu   sync.Mutex
	held map[string]*rootAgent
}

// of is the agent held under purpose, and whether one is.
func (a *agentsHeld) of(purpose string) (*rootAgent, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	held, ok := a.held[purpose]
	return held, ok
}

// hold keeps agent under purpose, or gives the one already held there.
func (a *agentsHeld) hold(purpose string, agent *rootAgent) *rootAgent {
	a.mu.Lock()
	defer a.mu.Unlock()
	if held, ok := a.held[purpose]; ok {
		return held
	}
	if len(a.held) == 0 {
		a.held = map[string]*rootAgent{}
	}
	a.held[purpose] = agent
	return agent
}

// maySetAgent is who may opt a namespace into an agent: ROOT, SUPER, and the
// namespace's owner as its definition names them.
func (s *QNTXServer) maySetAgent(admitted auth.Admission, namespace string) *protocol.Refusal {
	return s.mayHandle(admitted, namespace, "the agent of "+namespace+" is set")
}

// mayHandle is who handles what a namespace holds of its own: ROOT, SUPER, and
// the namespace's owner as its definition names them. what is what they
// handle, as a refusal says it.
func (s *QNTXServer) mayHandle(admitted auth.Admission, namespace, what string) *protocol.Refusal {
	if admitted.ReachesEveryNamespace() {
		return nil
	}
	known := s.held.Known()
	if known == nil {
		return &protocol.Refusal{Why: sigil.NotAllowed, Says: "this node keeps no namespace definitions, so " + what + " by ROOT or SUPER alone"}
	}
	listed, err := known.List()
	if err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "failed to read who owns %s", namespace).Error()}
	}
	found, err := namespaces.Named(listed, namespace)
	if err != nil {
		return &protocol.Refusal{Why: sigil.NotFound, Param: "namespace", Says: err.Error()}
	}
	if found.Definition != nil && found.Definition.Owner != "" && found.Definition.Owner == admitted.Identity {
		return nil
	}
	return &protocol.Refusal{Why: sigil.NotAllowed, Says: what + " by its owner, ROOT or SUPER"}
}

// agentsSignum is the namespace agent's sigils: set, am, login, say and session,
// each at the namespace's path.
func (s *QNTXServer) agentsSignum() sigil.Signum {
	h := s.claudeHarness()
	at := "/api/agents/{namespace}"
	namespace := &protocol.Param{Name: "namespace", Required: true, Says: "The namespace whose agent this is."}
	with := func(params ...*protocol.Param) []*protocol.Param {
		return append([]*protocol.Param{namespace}, params...)
	}
	harness := &protocol.Param{Name: "harness", Required: true, OneOf: []string{agentLineClaude, agentLinePi}, Says: "The harness it runs in: claude for Claude Code, pi for Pi. A namespace holds an agent in each."}
	login := s.loginSigil()
	login.sigil.Takes = with(login.sigil.Takes...)
	login.sigil.Http = &protocol.Endpoint{Method: http.MethodPost, Path: at + "/login"}
	login.sigil.Does = "Signs the namespace's agent in to Claude Code, by Claude Code's own flow, on the subscription of whoever sets it up. " + login.sigil.Does
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "agents",
			Description: "The namespace agent: a common agent of the namespace it is opted into, shared by everyone who has reach on it, on its own sign-in.",
			Tags:        []string{"agent", "namespaces"},
			Sigils: []*protocol.Sigil{
				{
					Name: "set",
					Does: "Opts the namespace into an agent: the model it is, and how it runs. Set by the namespace's owner, ROOT or SUPER. A different model is a different agent.",
					Takes: with(
						harness,
						&protocol.Param{Name: "model", Required: true, Says: "The model it is: Claude's full name in Claude Code, the id the gateway knows it as in Pi."},
						&protocol.Param{Name: "effort", Says: "In Claude Code, the effort it runs at: low, medium, high, xhigh or max."},
						&protocol.Param{Name: "permission_mode", OneOf: appcfg.PermissionModes, Says: "In Claude Code, the permission mode it runs in when whoever speaks names none."},
						&protocol.Param{Name: "allow", Says: "In Claude Code, the tools it may use without being asked, by Claude Code's own names, comma-separated."},
						&protocol.Param{Name: "thinking", OneOf: appcfg.ThinkingLevels, Says: "In Pi, the thinking level it runs at."},
						&protocol.Param{Name: "provider", OneOf: piProviderNames(), Says: "In Pi, the provider its model calls are made at, on the namespace's key of that name (keys set)."},
					),
					Answer: "protocol.NamespaceAgentAm",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: at},
				},
				{
					Name:   "am",
					Does:   "Who the namespace's agent is and how it runs, in the harness named.",
					Takes:  with(harness),
					Answer: "protocol.NamespaceAgentAm",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: at},
				},
				login.sigil,
				{
					Name:   "say",
					Does:   "Says something to the namespace's agent and gives what it answered. One session that continues, shared by everyone who has reach on the namespace, written down in the namespace by the agent as it goes. A picture attached travels by its file id (ADR-054).",
					Takes:  with(append([]*protocol.Param{harness, {Name: "says", Required: true, Says: "What is said to it."}, screenshotParam()}, h.sayTakes...)...),
					Answer: "protocol.NamespaceAgentSaid",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: at + "/say"},
				},
				{
					Name:   "session",
					Does:   "The namespace agent's one session in the harness named, whole: everything said to it there by whoever said it, as a transcript.",
					Takes:  with(harness),
					Answer: "protocol.SessionTranscript",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: at + "/session"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"set":     s.agentsSet,
			"am":      s.agentsAm,
			"login":   s.agentsLogin,
			"say":     s.agentsSay,
			"session": s.agentsSession,
		},
	}
}

func (s *QNTXServer) agentsSet(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	asked, refused := agentNamespaceOf(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	admitted, namespace := asked.admitted, asked.namespace
	if refused := s.maySetAgent(admitted, namespace); refused != nil {
		return nil, refused
	}
	model, err := named.TextOf("model", strings.TrimSpace(sent["model"]))
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "model", Says: err.Error() + ": the agent is the model named, and nothing stands in for it"}
	}
	switch sent["harness"] {
	case agentLineClaude:
		return s.setClaudeAgent(ctx, sent, admitted, namespace, model.String())
	case agentLinePi:
		return s.setPiAgent(sent, admitted, namespace, model.String())
	}
	return nil, notAHarness(sent)
}

// setClaudeAgent opts the namespace into its agent in Claude Code.
func (s *QNTXServer) setClaudeAgent(ctx context.Context, sent sigil.Sent, admitted auth.Admission, namespace, model string) (any, *protocol.Refusal) {
	effort, err := named.TextOf("effort", strings.TrimSpace(sent["effort"]))
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "effort", Says: err.Error() + ": an agent in Claude Code runs at the effort named"}
	}
	mode, err := named.TextOf("permission_mode", sent["permission_mode"])
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "permission_mode", Says: err.Error() + ": an agent in Claude Code runs in the permission mode named"}
	}
	spec := agentSpec{Model: model, Effort: effort.String(), Mode: mode.String(), Allow: []string{}}
	for _, tool := range strings.Split(sent["allow"], ",") {
		if tool = strings.TrimSpace(tool); tool != "" {
			spec.Allow = append(spec.Allow, tool)
		}
	}
	if err := s.setAgentLine(admitted, namespace, agentLineClaude, claudeLine(spec)); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	s.logger.Infow("a namespace opted into an agent", "namespace", namespace, "harness", agentLineClaude, "model", spec.Model, "by", admitted.Identity)
	return s.agentsAm(ctx, sent)
}

// setPiAgent opts the namespace into its agent in Pi: "I want Pi to also serv
// the function of namespace agent alongside Claude code namespace agent".
func (s *QNTXServer) setPiAgent(sent sigil.Sent, admitted auth.Admission, namespace, model string) (any, *protocol.Refusal) {
	thinking, err := named.TextOf("thinking", sent["thinking"])
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "thinking", Says: err.Error() + ": an agent in Pi runs at the thinking level named"}
	}
	provider, err := named.TextOf("provider", sent["provider"])
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "provider", Says: err.Error() + ": an agent in Pi makes its model calls at the provider named, on the namespace's key of that name"}
	}
	node, err := named.Pointed("this node's key, which the agent of "+namespace+" is derived from,", s.nodeDID)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	home, err := s.agentHome(namespace, model)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	agent, err := theAgent(node.PrivateKey, agentPurpose(namespace, model), home)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the agent of " + namespace + " in Pi has no key of its own: " + err.Error()}
	}
	if err := s.setAgentLine(admitted, namespace, agentLinePi, map[string]any{"model": model, "thinking": thinking.String(), "provider": provider.String()}); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	s.logger.Infow("a namespace opted into an agent", "namespace", namespace, "harness", agentLinePi, "model", model, "by", admitted.Identity)
	return &protocol.NamespaceAgentAm{Namespace: namespace, SetBy: admitted.Identity, Pi: &protocol.PiAm{Did: agent.did, Model: model, Thinking: thinking.String(), Provider: provider.String()}}, nil
}

func (s *QNTXServer) agentsAm(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	switch sent["harness"] {
	case agentLineClaude:
		held, refused := s.namespaceAgent(ctx, sent)
		return then(held, refused, func(held anAgentHeld) (any, *protocol.Refusal) {
			am, isClaude := s.amOf(s.claudeHarness(), held.agent, held.line.spec).(*protocol.ClaudeAm)
			if !isClaude {
				return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the namespace agent in " + held.agent.namespace + " did not answer as Claude Code"}
			}
			return &protocol.NamespaceAgentAm{Namespace: held.agent.namespace, SetBy: held.line.setBy, Agent: am}, nil
		})
	case agentLinePi:
		held, refused := s.namespacePiAgent(ctx, sent)
		return then(held, refused, func(held aPiAgentHeld) (any, *protocol.Refusal) {
			am, isPi := s.amOf(s.namespacePiHarness(held), held.agent, agentSpec{}).(*protocol.PiAm)
			if !isPi {
				return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the namespace agent in " + held.agent.namespace + " did not answer as Pi"}
			}
			return &protocol.NamespaceAgentAm{Namespace: held.agent.namespace, SetBy: held.line.setBy, Pi: am}, nil
		})
	}
	return nil, notAHarness(sent)
}

func (s *QNTXServer) agentsLogin(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	held, refused := s.namespaceAgent(ctx, sent)
	return then(held, refused, func(held anAgentHeld) (any, *protocol.Refusal) {
		return s.loginAgent(ctx, s.claudeHarness(), held.agent, sent)
	})
}

func (s *QNTXServer) agentsSay(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	switch sent["harness"] {
	case agentLineClaude:
		held, refused := s.namespaceAgent(ctx, sent)
		return then(held, refused, func(held anAgentHeld) (any, *protocol.Refusal) {
			answer, refused := s.sayTo(ctx, s.claudeHarness(), held.agent, held.line.spec, sent)
			return then(answer, refused, func(answer any) (any, *protocol.Refusal) {
				said, isClaude := answer.(*protocol.ClaudeSaid)
				if !isClaude {
					return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the namespace agent in " + held.agent.namespace + " did not answer as Claude Code"}
				}
				return &protocol.NamespaceAgentSaid{Claude: said}, nil
			})
		})
	case agentLinePi:
		held, refused := s.namespacePiAgent(ctx, sent)
		return then(held, refused, func(held aPiAgentHeld) (any, *protocol.Refusal) {
			answer, refused := s.sayTo(ctx, s.namespacePiHarness(held), held.agent, agentSpec{}, sent)
			return then(answer, refused, func(answer any) (any, *protocol.Refusal) {
				said, isPi := answer.(*protocol.PiSaid)
				if !isPi {
					return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the namespace agent in " + held.agent.namespace + " did not answer as Pi"}
				}
				return &protocol.NamespaceAgentSaid{Pi: said}, nil
			})
		})
	}
	return nil, notAHarness(sent)
}

func (s *QNTXServer) agentsSession(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	switch sent["harness"] {
	case agentLineClaude:
		held, refused := s.namespaceAgent(ctx, sent)
		return then(held, refused, func(held anAgentHeld) (any, *protocol.Refusal) {
			return s.readAgentSession(held.agent, held.agent.in(s.claudeHarness()))
		})
	case agentLinePi:
		held, refused := s.namespacePiAgent(ctx, sent)
		return then(held, refused, func(held aPiAgentHeld) (any, *protocol.Refusal) {
			return s.readAgentSession(held.agent, held.agent.in(s.piHarness()))
		})
	}
	return nil, notAHarness(sent)
}

// notAHarness is the refusal of a harness that is neither Claude Code nor Pi.
func notAHarness(sent sigil.Sent) *protocol.Refusal {
	return &protocol.Refusal{Why: sigil.NotOneOf, Param: "harness", Says: "harness is claude or pi, and " + sent["harness"] + " is neither"}
}

// namespacePiHarness is Pi as a namespace's agent runs in it: its session and
// its binary are Pi's, and what am says and its part of a turn are the AGENT
// line's.
func (s *QNTXServer) namespacePiHarness(held aPiAgentHeld) *harness {
	h := *s.piHarness()
	line := held.line
	h.am = func(is agentIn, _ *rootAgent, _ agentSpec) proto.Message {
		return &protocol.PiAm{
			Did: is.did, Model: line.model, Thinking: line.thinking, Provider: line.provider,
			Session: is.session, Answering: is.answering, Pi: is.path, PiVersion: pi.PinnedVersion, NotReady: is.notReady,
		}
	}
	h.part = func(context.Context, sigil.Sent, *rootAgent, agentSpec) (aTurn, *protocol.Refusal) {
		return s.namespacePiPart(held)
	}
	return &h
}
