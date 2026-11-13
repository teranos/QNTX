package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// "Agents cant wake Agents willy nilly"
// "the messages collect"
// "and as a human i can dial it"
// "and as a human i can observe it"
// "ROOM WAKE NEEDS TO FOLLOW SAME SAY LIMIT RULES"
// "EXPERT OVERRIDE ALWAYS"
//
// What an agent says to an agent does not go out on its own. It is collected
// for the agent it was said to, and what is collected is delivered whole, as
// one thing said: no sooner than the agent's wait after the oldest of it, and
// no sooner than that after the last delivery. A person sees what waits, holds
// it longer, or releases it. What a person says is never collected.

const collectedPath = "/api/collected"

// How long what is collected waits, by the model of the agent it is for. An
// agent whose model the node does not hold waits the longer.
var (
	collectedForFable = 6 * time.Hour
	collectedForOpus  = 3 * time.Hour
)

// collectedLooked is how often the node looks for what is due.
var collectedLooked = time.Minute

// collectedFile is where what is collected is kept, beside the agents.
const collectedFile = "collected.json"

// collectedSaid is one thing an agent said to another.
type collectedSaid struct {
	From string    `json:"from"`
	Says string    `json:"says"`
	At   time.Time `json:"at"`
	Task string    `json:"task,omitempty"`
}

// collectedFor is what waits for one agent.
type collectedFor struct {
	// DID is the agent; Called the name it holds or what the node calls it.
	DID    string `json:"did"`
	Called string `json:"called"`
	// Tenant is where the node serves it over A2A: empty for the Node Agent,
	// its name for an agent that holds one.
	Tenant string `json:"tenant"`
	Model  string `json:"model,omitempty"`
	// HeldUntil is a person's word that nothing is delivered before it.
	HeldUntil time.Time       `json:"held_until,omitempty"`
	Delivered time.Time       `json:"delivered,omitempty"`
	Waiting   []collectedSaid `json:"waiting"`
}

// waitOf is how long what is collected for an agent on model waits.
func waitOf(model string) time.Duration {
	if strings.Contains(strings.ToLower(model), "opus") {
		return collectedForOpus
	}
	return collectedForFable
}

// due is when what waits is delivered if nobody holds or releases it.
func (c collectedFor) due() time.Time {
	if len(c.Waiting) == 0 {
		return time.Time{}
	}
	wait := waitOf(c.Model)
	due := c.Waiting[0].At.Add(wait)
	if after := c.Delivered.Add(wait); !c.Delivered.IsZero() && after.After(due) {
		due = after
	}
	if c.HeldUntil.After(due) {
		due = c.HeldUntil
	}
	return due
}

// collection is everything collected on this node, kept in one file.
type collection struct {
	mu   sync.Mutex
	path string
}

func (c *collection) read() (map[string]*collectedFor, error) {
	held := map[string]*collectedFor{}
	raw, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		return held, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "what is collected at %s was not read", c.path)
	}
	if err := json.Unmarshal(raw, &held); err != nil {
		return nil, errors.Wrapf(err, "what is collected at %s is not JSON", c.path)
	}
	return held, nil
}

func (c *collection) write(held map[string]*collectedFor) error {
	raw, err := json.MarshalIndent(held, "", "  ")
	if err != nil {
		return errors.Wrap(err, "what is collected did not marshal")
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return errors.Wrapf(err, "%s was not made to keep what is collected in", filepath.Dir(c.path))
	}
	// Written beside and moved over: a node that stops mid-write keeps the last whole one.
	beside := c.path + ".writing"
	if err := os.WriteFile(beside, raw, 0o600); err != nil {
		return errors.Wrapf(err, "what is collected was not written at %s", beside)
	}
	return errors.Wrapf(os.Rename(beside, c.path), "what is collected was not kept at %s", c.path)
}

// change reads what is collected, lets change it, and keeps it.
func (c *collection) change(change func(map[string]*collectedFor) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	held, err := c.read()
	if err != nil {
		return err
	}
	if err := change(held); err != nil {
		return err
	}
	return c.write(held)
}

// theCollection is this node's, kept beside its agents.
func (s *QNTXServer) theCollection() (*collection, error) {
	s.collectedOnce.Do(func() {
		home := ""
		if s.nodeAgent != nil {
			home = s.nodeAgent.home
		} else if at, err := nodeAgentHome(); err == nil {
			home = at
		}
		if home != "" {
			s.collected = &collection{path: filepath.Join(filepath.Dir(home), collectedFile)}
		}
	})
	if s.collected == nil {
		return nil, errors.New("this node keeps its agents nowhere, so nothing is collected")
	}
	return s.collected, nil
}

// deliversCollected marks a request as the node delivering what was
// collected: said for the agents that said it, released by a person or due.
type deliversCollected struct{}

// aPersonSpeaks is whether the one a request speaks for is a person: one the
// gate admitted who is no agent by a DID of its own (itsOwnDID). A person's
// session is one, and so is a client a person signed in to, which speaks for
// them. What nobody was admitted for, a delivery of GitHub's or a room's wake,
// is no person's. What the node delivers of what it collected is said as a
// person's is.
func aPersonSpeaks(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Context().Value(deliversCollected{}) != nil {
		return true
	}
	admitted, gated := auth.AdmissionFrom(r.Context())
	if !gated {
		// A node with no gate admits nobody, and whoever speaks there is its
		// person. A turn the node's own wake started names who wrote what
		// woke it (wake_github.go), and that is no person.
		return speakerOf(r.Context()) == ""
	}
	_, noAgent := itsOwnDID(admitted)
	return noAgent != nil
}

// collect keeps what an agent said to another, to be delivered with what else
// is collected for it, and answers when that is if nobody holds or releases it.
func (s *QNTXServer) collect(to collectedFor, said collectedSaid) (time.Time, error) {
	kept, err := s.theCollection()
	if err != nil {
		return time.Time{}, err
	}
	var due time.Time
	err = kept.change(func(held map[string]*collectedFor) error {
		waits, found := held[to.DID]
		if !found {
			waits = &collectedFor{DID: to.DID}
			held[to.DID] = waits
		}
		// What it is called, where it is served and what it runs on are as the node holds them now.
		waits.Called, waits.Tenant, waits.Model = to.Called, to.Tenant, to.Model
		waits.Waiting = append(waits.Waiting, said)
		due = waits.due()
		return nil
	})
	if err != nil {
		return time.Time{}, err
	}
	s.logger.Infow("What an agent said to an agent is collected", "to", to.Called, "from", said.From, "task", said.Task, "due", due.UTC().Format(time.RFC3339))
	return due, nil
}

// calledBy is the name the agent whose DID this is holds, or what was given:
// a DID that holds none, or who a delivery of GitHub's says wrote it.
func (s *QNTXServer) calledBy(who string) string {
	if names, err := s.agentNames(); err == nil {
		if at := slices.IndexFunc(names, func(kept agentName) bool { return kept.did == who && !kept.let }); at >= 0 {
			return names[at].name
		}
	}
	if who == "" {
		return "nobody the node admitted"
	}
	return who
}

// collectedSays is why what an agent said was not said to the agent now.
func collectedSays(called string, due time.Time) string {
	return "collected for " + called + ", not said to it now: what agents say to an agent is delivered together, here no sooner than " +
		due.UTC().Format(time.RFC3339) + ", or when a person releases it (collected list)"
}

// theNodeAgentsModel is the model the node holds its Node Agent to run on.
func (s *QNTXServer) theNodeAgentsModel() string {
	if s.deps == nil || s.deps.cfg == nil {
		return ""
	}
	return s.deps.cfg.Agent.Root.Claude.Model
}

// asOneThing is everything that waits for an agent, as the one thing said to it.
func asOneThing(waiting []collectedSaid) string {
	var said strings.Builder
	said.WriteString("Collected for you: what agents said to you since " + waiting[0].At.UTC().Format(time.RFC3339) + ", delivered together. No person said any of it.\n")
	for _, one := range waiting {
		said.WriteString("\n" + one.From + ", " + one.At.UTC().Format(time.RFC3339) + ":\n" + one.Says + "\n")
	}
	return said.String()
}

// deliverCollected says everything that waits for the agent did to it, as one
// thing, and ends each Task it was said as. Nothing waiting is nothing done.
func (s *QNTXServer) deliverCollected(ctx context.Context, did string) error {
	kept, err := s.theCollection()
	if err != nil {
		return err
	}
	kept.mu.Lock()
	held, err := kept.read()
	kept.mu.Unlock()
	if err != nil {
		return err
	}
	waits, found := held[did]
	if !found || len(waits.Waiting) == 0 {
		return nil
	}
	delivering := slices.Clone(waits.Waiting)

	// Said over the node's own A2A, as what a person says is: the agent's
	// turn, or its status row, as it is for anything said to it.
	as := auth.Admitted(auth.LevelRoot)
	said := context.WithValue(auth.WithAdmission(ctx, as), deliversCollected{}, true)
	client, _, refused := s.ownClient(said, waits.Tenant)
	if refused != nil {
		return errors.Newf("what is collected for %s was not delivered: %s", waits.Called, refused.GetSays())
	}
	defer func() { _ = client.Destroy() }()
	message := a2asdk.NewMessage(a2asdk.MessageRoleUser, a2asdk.NewTextPart(asOneThing(delivering)))
	returns := true
	answered, err := client.SendMessage(said, &a2asdk.SendMessageRequest{Message: message, Config: &a2asdk.SendMessageConfig{ReturnImmediately: returns}})
	if err != nil {
		return errors.Wrapf(err, "what is collected for %s was not delivered", waits.Called)
	}
	carried := ""
	if info := answered.TaskInfo(); info.TaskID != "" {
		carried = string(info.TaskID)
	}

	now := time.Now()
	if err := kept.change(func(held map[string]*collectedFor) error {
		waits, found := held[did]
		if !found {
			return nil
		}
		// What was said while this was delivered waits for the next.
		waits.Waiting = slices.DeleteFunc(waits.Waiting, func(one collectedSaid) bool {
			return slices.ContainsFunc(delivering, func(sent collectedSaid) bool {
				return sent.At.Equal(one.At) && sent.From == one.From && sent.Says == one.Says
			})
		})
		waits.Delivered, waits.HeldUntil = now, time.Time{}
		return nil
	}); err != nil {
		return err
	}
	lines := s.systemTasks(did)
	for _, one := range delivering {
		if one.Task == "" {
			continue
		}
		if err := s.endTask(lines, one.Task, a2asdk.TaskStateCompleted, did, "", "delivered to "+waits.Called+" with what else was collected, as Task "+carried, ""); err != nil {
			s.logger.Errorw("A Task collected and delivered was not ended", "task", one.Task, "to", waits.Called, "error", err)
		}
	}
	s.logger.Infow("What was collected for an agent is delivered", "to", waits.Called, "things", len(delivering), "task", carried)
	return nil
}

// keepDeliveringCollected delivers what is due until the node stops. What
// failed is said once for as long as it keeps failing the same way.
func (s *QNTXServer) keepDeliveringCollected() {
	tick := time.NewTicker(collectedLooked)
	defer tick.Stop()
	last := ""
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
			err := s.deliverDue(time.Now())
			switch {
			case err == nil:
				last = ""
			case err.Error() != last:
				last = err.Error()
				s.logger.Errorw("What is collected and due was not delivered", "error", err)
			}
		}
	}
}

// deliverDue delivers what is due at now, for every agent something waits for.
func (s *QNTXServer) deliverDue(now time.Time) error {
	kept, err := s.theCollection()
	if err != nil {
		return nil
	}
	kept.mu.Lock()
	held, err := kept.read()
	kept.mu.Unlock()
	if err != nil {
		return err
	}
	failed := []string{}
	for did, waits := range held {
		if len(waits.Waiting) == 0 || now.Before(waits.due()) {
			continue
		}
		if err := s.deliverCollected(s.ctx, did); err != nil {
			failed = append(failed, err.Error())
		}
	}
	if len(failed) > 0 {
		slices.Sort(failed)
		return errors.New(strings.Join(failed, "; "))
	}
	return nil
}

func (s *QNTXServer) collectedSignum() sigil.Signum {
	to := &protocol.Param{Name: "to", Required: true, Says: "The agent it waits for, by the name it holds or what the node calls it, as collected list gives it."}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "collected",
			Description: "What agents said to an agent, collected: it is delivered together, after the agent's wait, and a person sees it, holds it longer or releases it.",
			Tags:        []string{"agents", "collected"},
			Oasf:        []string{"ai_ml_engineering/agent_orchestration/multi_agent_coordination"},
			Sigils: []*protocol.Sigil{
				{
					Name:   "list",
					Does:   "What waits for each agent: who said it, what, when, and when it is delivered if nobody holds or releases it.",
					Answer: "protocol.CollectedAll",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: collectedPath},
				},
				{
					Name: "hold",
					Does: "Holds what waits for an agent: nothing collected for it is delivered before the moment said. A person's to say.",
					Takes: []*protocol.Param{to,
						{Name: "until", Required: true, Says: "The moment nothing is delivered before, RFC3339."},
					},
					Answer: "protocol.CollectedAll",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: collectedPath + "/hold"},
				},
				{
					Name:   "release",
					Does:   "Delivers what waits for an agent now, as one thing said to it. A person's to say.",
					Takes:  []*protocol.Param{to},
					Answer: "protocol.CollectedAll",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: collectedPath + "/release"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"list": s.collectedList, "hold": s.collectedHold, "release": s.collectedRelease,
		},
	}
}

func stamped(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

func (s *QNTXServer) collectedList(context.Context, sigil.Sent) (any, *protocol.Refusal) {
	answer := &protocol.CollectedAll{Collected: []*protocol.Collected{}}
	kept, err := s.theCollection()
	if err != nil {
		return answer, nil
	}
	kept.mu.Lock()
	held, err := kept.read()
	kept.mu.Unlock()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	for _, waits := range held {
		if len(waits.Waiting) == 0 && waits.HeldUntil.IsZero() {
			continue
		}
		one := &protocol.Collected{
			To: waits.Called, Did: waits.DID, Model: waits.Model, Waits: waitOf(waits.Model).String(),
			Due: stamped(waits.due()), HeldUntil: stamped(waits.HeldUntil), Delivered: stamped(waits.Delivered),
			Waiting: []*protocol.CollectedSaid{},
		}
		for _, said := range waits.Waiting {
			one.Waiting = append(one.Waiting, &protocol.CollectedSaid{From: said.From, Says: said.Says, At: stamped(said.At), Task: said.Task})
		}
		answer.Collected = append(answer.Collected, one)
	}
	slices.SortFunc(answer.Collected, func(a, b *protocol.Collected) int { return strings.Compare(a.GetTo(), b.GetTo()) })
	return answer, nil
}

// aPersonsToSay is nil when a person asks, and why not otherwise: what waits
// is held and released by a person, and by no agent.
func aPersonsToSay(ctx context.Context, does string) *protocol.Refusal {
	if aPersonSpeaks(sigil.Caller(ctx)) {
		return nil
	}
	return &protocol.Refusal{Why: sigil.NotAllowed, Says: "collected " + does + " is a person's to say, and this asks as an agent"}
}

// collectedNamed is the DID of the agent something waits for under name.
func (s *QNTXServer) collectedNamed(name string) (string, *protocol.Refusal) {
	kept, err := s.theCollection()
	if err != nil {
		return "", &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	kept.mu.Lock()
	held, err := kept.read()
	kept.mu.Unlock()
	if err != nil {
		return "", &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	for did, waits := range held {
		if strings.EqualFold(waits.Called, strings.TrimSpace(name)) {
			return did, nil
		}
	}
	return "", &protocol.Refusal{Why: sigil.NotFound, Param: "to", Says: "nothing is collected for " + name}
}

func (s *QNTXServer) collectedHold(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	if refused := aPersonsToSay(ctx, "hold"); refused != nil {
		return nil, refused
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(sent["until"]))
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "until", Says: "until is a moment, as RFC3339, and this said " + sent["until"]}
	}
	did, refused := s.collectedNamed(sent["to"])
	if refused != nil {
		return nil, refused
	}
	kept, err := s.theCollection()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if err := kept.change(func(held map[string]*collectedFor) error {
		held[did].HeldUntil = until
		return nil
	}); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return s.collectedList(ctx, sent)
}

func (s *QNTXServer) collectedRelease(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	if refused := aPersonsToSay(ctx, "release"); refused != nil {
		return nil, refused
	}
	did, refused := s.collectedNamed(sent["to"])
	if refused != nil {
		return nil, refused
	}
	if err := s.deliverCollected(ctx, did); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return s.collectedList(ctx, sent)
}
