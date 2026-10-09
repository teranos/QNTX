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
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

// An AGENT line in a namespace says which agent the namespace opted into; its
// predicate is the harness, and the newest line per harness holds.
const (
	agentLineSubject = "AGENT"
	agentLineClaude  = "claude"
)

// agentPurpose is what a namespace agent's key and token are derived from the
// node's for: the namespace and the model, so two models are two agents, and
// so is one model in two places.
func agentPurpose(namespace, model string) string {
	return "agent:" + slug.Of(namespace) + ":" + model
}

// agentHome is where the node keeps a namespace's agent: beside ROOT's, by the
// namespace and the model. Where agents are kept is set with the ROOT agent
// (nameRootAgent), and a node that set none keeps them nowhere.
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
	reading, err := s.held.Read(namespace)
	if err != nil {
		return agentLine{}, false, errors.Wrapf(err, "failed to read the %s line of %s", agentLineSubject, namespace)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{Subjects: []string{agentLineSubject}, Limit: storage.MaxAttestationLimit})
	if err != nil {
		return agentLine{}, false, errors.Wrapf(err, "failed to read the %s line of %s", agentLineSubject, namespace)
	}
	var newest *types.As
	held := false
	for _, as := range found {
		if !slices.Contains(as.Predicates, agentLineClaude) {
			continue
		}
		if !held || as.Timestamp.After(newest.Timestamp) {
			newest, held = as, true
		}
	}
	if !held {
		return agentLine{}, false, nil
	}
	line := agentLine{at: newest.Timestamp}
	if len(newest.Actors) == 0 {
		return agentLine{}, false, errors.Newf("%s line %s of %s names nobody who set it", agentLineSubject, newest.ID, namespace)
	}
	line.setBy = newest.Actors[0]
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

// textOf is a line's attribute that is one text, and a line without it is a
// line that does not read.
func oneText(as map[string]any, attribute, subject, id, name string) (string, error) {
	held, ok := as[attribute].(string)
	if !ok {
		return "", errors.Newf("%s line %s of %s: %s is %v, not text", subject, id, name, attribute, as[attribute])
	}
	return held, nil
}

// setAgentLine writes the namespace's agent whole, as whoever set it.
func (s *QNTXServer) setAgentLine(admitted auth.Admission, namespace string, spec agentSpec) error {
	store, err := s.held.Write(admitted, namespace)
	if err != nil {
		return errors.Wrapf(err, "no store to write the %s line of %s in", agentLineSubject, namespace)
	}
	id, err := identity.GenerateASUIDWithRetry("AS", agentLineSubject, agentLineClaude, namespace, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line of %s", agentLineSubject, namespace)
	}
	at := time.Now()
	return errors.Wrapf(store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{agentLineSubject}, Predicates: []string{agentLineClaude}, Contexts: []string{namespace},
		Actors: []string{admitted.Identity}, Timestamp: at, CreatedAt: at, Source: "agent",
		Attributes: map[string]any{"model": spec.Model, "effort": spec.Effort, "permission_mode": spec.Mode, "allow": anyOf(spec.Allow)},
	}), "the %s line of %s was not written", agentLineSubject, namespace)
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
		return anAsking{}, &protocol.Refusal{Why: sigil.NotAllowed, Param: "namespace", Says: "system has no agent of its own: the ROOT agent is the node's (claude say)"}
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
	purpose := agentPurpose(namespace, line.spec.Model)
	if held, ok := s.namespaceAgents.of(purpose); ok {
		return anAgentHeld{agent: held, line: line}, nil
	}
	if s.nodeDID == nil {
		return anAgentHeld{}, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no key of its own to derive the agent of " + namespace + " from"}
	}
	home, err := s.agentHome(namespace, line.spec.Model)
	if err != nil {
		return anAgentHeld{}, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	agent, err := theAgent(s.nodeDID.PrivateKey, purpose, home)
	if err != nil {
		return anAgentHeld{}, &protocol.Refusal{Why: sigil.Failed, Says: "the agent of " + namespace + " has no key of its own: " + err.Error()}
	}
	agent.namespace, agent.called = namespace, "the agent of "+namespace
	// Its token is what keeps it in its namespace at the gate. A node without
	// auth has no gate to hold it, and would admit it as the node's one caller,
	// everywhere: it runs no namespace agent.
	if s.authHandler == nil {
		return anAgentHeld{}, &protocol.Refusal{Why: sigil.Failed, Says: "this node runs without auth, so the agent of " + namespace + " would have no token keeping it in " + namespace + ": it runs none"}
	}
	if err := s.authHandler.HoldNamespaceAgent(agent.token, agent.did, purpose, line.setBy, namespace); err != nil {
		return anAgentHeld{}, &protocol.Refusal{Why: sigil.Failed, Says: "the token of the agent of " + namespace + " is not held, so it would reach none of the node's sigils: " + err.Error()}
	}
	return anAgentHeld{agent: s.namespaceAgents.hold(purpose, agent), line: line}, nil
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
	if admitted.ReachesEveryNamespace() {
		return nil
	}
	known := s.held.Known()
	if known == nil {
		return &protocol.Refusal{Why: sigil.NotAllowed, Says: "this node keeps no namespace definitions, so only ROOT or SUPER sets an agent"}
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
	return &protocol.Refusal{Why: sigil.NotAllowed, Says: "the agent of " + namespace + " is set by its owner, ROOT or SUPER"}
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
	amGives := append([]*protocol.Field{
		{Name: "namespace", Says: "The namespace it stands in."},
		{Name: "set_by", Says: "Who opted the namespace into it."},
	}, h.amGives...)
	login := s.loginSigil(h)
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
						&protocol.Param{Name: "model", Required: true, Says: "The Claude model it is, by its full name."},
						&protocol.Param{Name: "effort", Required: true, Says: "The effort it runs at: low, medium, high, xhigh or max."},
						&protocol.Param{Name: "permission_mode", Required: true, OneOf: appcfg.PermissionModes, Says: "The permission mode it runs in when whoever speaks names none."},
						&protocol.Param{Name: "allow", Says: "The tools it may use without being asked, by Claude Code's own names, comma-separated."},
					),
					Gives: amGives,
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: at},
				},
				{
					Name:  "am",
					Does:  "Who the namespace's agent is and how it runs.",
					Takes: with(),
					Gives: amGives,
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: at},
				},
				login.sigil,
				{
					Name:  "say",
					Does:  "Says something to the namespace's agent and gives what it answered. One session that continues, shared by everyone who has reach on the namespace, written down in the namespace by the agent as it goes.",
					Takes: with(append([]*protocol.Param{{Name: "says", Required: true, Says: "What is said to it."}}, h.sayTakes...)...),
					Gives: h.sayGives,
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: at + "/say"},
				},
				{
					Name:  "session",
					Does:  "The namespace agent's one session, whole: everything said to it by whoever said it, as a transcript.",
					Takes: with(),
					Gives: []*protocol.Field{{Name: "transcript", Says: h.transcriptSays, Message: "protocol.Transcript"}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: at + "/session"},
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
	spec := agentSpec{Model: strings.TrimSpace(sent["model"]), Effort: strings.TrimSpace(sent["effort"]), Mode: sent["permission_mode"], Allow: []string{}}
	if spec.Model == "" {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "model", Says: "model is required: the agent is the model named, and nothing stands in for it"}
	}
	if spec.Effort == "" {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "effort", Says: "effort is required"}
	}
	for _, tool := range strings.Split(sent["allow"], ",") {
		if tool = strings.TrimSpace(tool); tool != "" {
			spec.Allow = append(spec.Allow, tool)
		}
	}
	if err := s.setAgentLine(admitted, namespace, spec); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	s.logger.Infow("a namespace opted into an agent", "namespace", namespace, "model", spec.Model, "by", admitted.Identity)
	return s.agentsAm(ctx, sent)
}

func (s *QNTXServer) agentsAm(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	held, refused := s.namespaceAgent(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	is := s.amOf(s.claudeHarness(), held.agent, held.line.spec)
	is["namespace"], is["set_by"] = held.agent.namespace, held.line.setBy
	return is, nil
}

func (s *QNTXServer) agentsLogin(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	held, refused := s.namespaceAgent(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	return s.loginAgent(ctx, s.claudeHarness(), held.agent, sent)
}

func (s *QNTXServer) agentsSay(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	held, refused := s.namespaceAgent(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	return s.sayTo(ctx, s.claudeHarness(), held.agent, held.line.spec, sent)
}

func (s *QNTXServer) agentsSession(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	held, refused := s.namespaceAgent(ctx, sent)
	if refused != nil {
		return nil, refused
	}
	return s.readAgentSession(held.agent, held.agent.in(s.claudeHarness()))
}
