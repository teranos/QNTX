package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/acp"
	"github.com/teranos/QNTX/internal/agy"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/comet"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

// 「[Opus 5.5] The Node Agent in agy, a fourth harness (ADR-048).」
// "wiring it into node and namespace config as an actual harness"

const (
	agentLineAgy   = "agy"
	agySessionFile = "agy-session"
	// 「[Opus 5.5] agyConversationFile is agy's own ACP session, kept apart from the」
	// 「[Opus 5.5] node's session: agy names it on a first turn and resumes only that.」
	agyConversationFile = "agy-conversation"
	// 「[Opus 5.5] googleAgyLineSubject is the line of ROOT's yes for agy.」
	googleAgyLineSubject = "GOOGLE-AGY"
	// 「[Opus 5.5] googleRunsModels lets the node's account call Vertex AI, which agy runs on.」
	googleRunsModels = "roles/aiplatform.user"
	// 「[Opus 5.5] googleModelService is Vertex AI.」
	googleModelService = "aiplatform.googleapis.com"
	// 「[Opus 5.5] agyLocation is where Vertex AI serves agy's model.」
	agyLocation = "global"
)

// 「[Opus 5.5] agyHarness is the Node Agent in agy.」
func (s *QNTXServer) agyHarness() *harness {
	named := func() appcfg.AgyConfig { return s.deps.cfg.Agent.Root.Agy }
	return &harness{
		name: agentLineAgy, called: "agy", file: agySessionFile,
		named:    func() bool { return named().Named() },
		absent:   s.thereIsNoAgy,
		fetching: "agy is still being fetched",
		spec:     func() agentSpec { return agentSpec{} },
		part: func(_ context.Context, _ sigil.Sent, agent *rootAgent, _ agentSpec) (aTurn, *protocol.Refusal) {
			return s.agyPart(named(), agent)
		},
		am: func(is agentIn, _ *rootAgent, _ agentSpec) proto.Message {
			return &protocol.AgyAm{
				Did: is.did, Model: named().Model,
				Session: is.session, Answering: is.answering, Agy: is.path, AgyVersion: agy.PinnedVersion, NotReady: is.notReady,
			}
		},
	}
}

// 「[Opus 5.5] holdAgy fetches the agy Parity pins into dir, off the boot's own time, as」
// 「[Opus 5.5] holdCodex does codex.」
func holdAgy(ctx context.Context, dir string, spawn func(string, func()), logger *zap.SugaredLogger) *harnessHeld {
	held := &harnessHeld{name: "agy", fetched: make(chan struct{})}
	spawn("agent.agy", func() {
		defer close(held.fetched)
		pin, err := agy.Pinned()
		if err != nil {
			held.err = errors.Wrap(err, "the agy this build pins did not read")
		} else {
			held.path, held.err = pin.Ensure(ctx, dir)
		}
		if held.err != nil {
			logger.Errorw("agy was not fetched, so the Node Agent does not run in it", "dir", dir, "error", held.err)
			return
		}
		logger.Infow("agy is on this node", "version", pin.Version, "path", held.path)
	})
	return held
}

// 「[Opus 5.5] thereIsNoAgy is why the Node Agent cannot be spoken to in agy on this node.」
func (s *QNTXServer) thereIsNoAgy() *protocol.Refusal {
	if s.nodeAgent == nil {
		return s.thereIsNoNodeAgent()
	}
	if !s.deps.cfg.Agent.Root.Agy.Named() {
		return &protocol.Refusal{Why: sigil.NotFound, Says: "this node's am.toml names no agy for the Node Agent ([agent.root.agy]), so it does not run in agy"}
	}
	return &protocol.Refusal{Why: sigil.Failed, Says: "this node holds no agy, so the Node Agent does not run in it"}
}

// 「[Opus 5.5] connectGoogleAgy lets the node's account call Vertex AI on ROOT's token: its own」
// 「[Opus 5.5] yes, for it is what spends. ROOT's token is let go once the node no longer needs it.」
func (s *QNTXServer) connectGoogleAgy(ctx context.Context, token, by string) (said string, err error) {
	defer s.letGoOfGoogle(token)
	defer func() { err = googleFixOf(err) }()
	kept, held, err := s.googleLine()
	if err != nil {
		return "", err
	}
	if !held {
		return "", errors.New("this node is not connected to Google Cloud yet: Connect it first")
	}
	if err := enableServices(ctx, token, kept.project.ProjectNumber, []string{googleModelService}); err != nil {
		return "", err
	}
	at := googleResourceManager + "/v1/projects/" + kept.project.ProjectID
	if err := grantOn(ctx, token, at, http.MethodPost, googleRunsModels, "serviceAccount:"+kept.account); err != nil {
		return "", errors.Wrapf(err, "%s was not granted %s in %s", kept.account, googleRunsModels, kept.project.ProjectID)
	}
	if refused := s.keepGoogleCostLine(googleAgyLineSubject, "wired", by, map[string]any{"role": googleRunsModels}); refused != nil {
		return "", errors.New(refused.Says)
	}
	s.tellGoogleOutcome(googleOutcomeMail("The node may call Vertex AI", "The node may call Vertex AI in "+kept.project.ProjectID+", for a turn in agy.\n"))
	return "The node may call Vertex AI in " + kept.project.ProjectID + ". A turn in agy spends what that project spends, and halts with what costs.", nil
}

// 「[Opus 5.5] googleAgyWired is whether ROOT let the node call Vertex AI.」
func (s *QNTXServer) googleAgyWired() (bool, error) {
	lines, err := s.googleCostLines(googleAgyLineSubject)
	return len(lines) > 0, err
}

// 「[Opus 5.5] agyAsTheNode is agy signed in as the node at Google: an external_account file in」
// 「[Opus 5.5] the agent's home, and the identity token it reads, fresh for the turn.」
func (s *QNTXServer) agyAsTheNode(agent *rootAgent) (*agy.Google, *protocol.Refusal) {
	kept, held, err := s.googleLine()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if !held {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Says: "agy runs as the node at Google Cloud, and this node is not connected to it: ROOT connects it from Node Connectors"}
	}
	let, err := s.googleAgyWired()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if !let {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Says: "this node may not call Vertex AI yet: ROOT presses Connect agy under Google Cloud in Node Connectors"}
	}
	dir := filepath.Dir(agyTokenFile(agent))
	tokenFile, credentials := agyTokenFile(agent), filepath.Join(dir, "external-account.json")
	file, err := json.Marshal(map[string]any{
		"type":                              "external_account",
		"audience":                          googleAudience(kept.project.ProjectNumber),
		"subject_token_type":                "urn:ietf:params:oauth:token-type:jwt",
		"token_url":                         googleSTS + "/v1/token",
		"service_account_impersonation_url": googleCredentials + "/v1/projects/-/serviceAccounts/" + kept.account + ":generateAccessToken",
		"credential_source":                 map[string]any{"file": tokenFile},
	})
	if err == nil {
		err = os.MkdirAll(dir, 0o700)
	}
	if err == nil {
		err = s.writeAgyToken(agent, kept)
	}
	if err == nil {
		err = os.WriteFile(credentials, file, 0o600)
	}
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: errors.Wrapf(err, "agy's identity at Google was not written to %s", dir).Error()}
	}
	if _, renewing := s.identityTokensRenewed.LoadOrStore(tokenFile, true); !renewing {
		sacred.Go("agy.identity-token", func() { s.keepAgyTokenFresh(agent) })
	}
	return &agy.Google{Credentials: credentials, Project: kept.project.ProjectID, Location: agyLocation}, nil
}

// 「[Opus 5.5] agyTokenFile is where agy reads the node's identity token for Google.」
func agyTokenFile(agent *rootAgent) string {
	return filepath.Join(agent.home, ".qntx-google", "identity-token")
}

// 「[Opus 5.5] writeAgyToken writes a fresh identity token for the node's pool at Google.」
func (s *QNTXServer) writeAgyToken(agent *rootAgent, kept googleKept) error {
	i, err := s.theIssuer()
	if err != nil {
		return err
	}
	token, err := i.identityTokenFor(s.nodeDID.DID, auth.NamespaceSystem, googleAudience(kept.project.ProjectNumber))
	if err != nil {
		return err
	}
	return errors.Wrapf(os.WriteFile(agyTokenFile(agent), []byte(token), 0o600), "the identity token was not written to %s", agyTokenFile(agent))
}

// 「[Opus 5.5] renewAgyToken rewrites the token, on the line as it is now. A node no longer」
// 「[Opus 5.5] connected has nothing to renew.」
func (s *QNTXServer) renewAgyToken(agent *rootAgent) error {
	kept, held, err := s.googleLine()
	if err != nil || !held {
		return err
	}
	return s.writeAgyToken(agent, kept)
}

// 「[Opus 5.5] keepAgyTokenFresh rewrites it before the last one expires: a turn outlasts the」
// 「[Opus 5.5] token, and agy's credentials read the file again at every exchange.」
func (s *QNTXServer) keepAgyTokenFresh(agent *rootAgent) {
	tick := time.NewTicker(identityTokenLifetime / 2)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
			if err := s.renewAgyToken(agent); err != nil {
				s.logger.Errorw("the identity token agy runs on at Google was not renewed", "file", agyTokenFile(agent), "error", err)
			}
		}
	}
}

// 「[Opus 5.5] agyPart is agy's part of one turn of the Node Agent: the node at Google, the」
// 「[Opus 5.5] agent's ground on every event agy sends, and its own conversation.」
func (s *QNTXServer) agyPart(named appcfg.AgyConfig, agent *rootAgent) (aTurn, *protocol.Refusal) {
	var binary string
	var google *agy.Google
	return aTurn{
		ready: func(ctx context.Context, _ string, kept bool) (bool, *protocol.Refusal) {
			var err error
			var refused *protocol.Refusal
			// "everything that costs anything needs to be halted until the budget frees up"
			if halted, why := s.googleSpendingHalted(); halted {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: "what costs at Google Cloud is halted, so agy does not run: " + why}
			}
			if google, refused = s.agyAsTheNode(agent); refused != nil {
				return false, refused
			}
			if binary, err = s.harnessHeldBy(agentLineAgy).Path(ctx); err != nil {
				return false, &protocol.Refusal{Why: sigil.Failed, Says: "this node has no agy to run: " + err.Error()}
			}
			if refused := agyUnderGround(agent); refused != nil {
				return false, refused
			}
			return kept, nil
		},
		run: func(t turnRun) (proto.Message, error) {
			conversation := ""
			if t.resumes {
				conversation = agyConversationOf(agent.home)
			}
			return s.agyTurn(t, agent, agy.Said{Binary: binary, Home: agent.home, Session: conversation, Says: t.says, Model: named.Model, Google: google, Env: t.env})
		},
	}, nil
}

// 「[Opus 5.5] agyUnderGround is whether agent runs under ground in agy: its ground built,」
// 「[Opus 5.5] and agy's hooks running it.」
func agyUnderGround(agent *rootAgent) *protocol.Refusal {
	if err := comet.Under(agent.home); err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: agent.called + " is said nothing until it runs under ground: " + err.Error()}
	}
	if err := comet.AgentAgyHooks(agent.home); err != nil {
		return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return nil
}

// 「[Opus 5.5] agyConversationOf is the conversation agy last answered agent in, or empty.」
func agyConversationOf(home string) string {
	held, err := os.ReadFile(filepath.Join(home, agyConversationFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(held))
}

// 「[Opus 5.5] agyTurn runs said, writes its ACP updates down as the session's rows, and keeps」
// 「[Opus 5.5] the session agy answered in.」
func (s *QNTXServer) agyTurn(t turnRun, agent *rootAgent, said agy.Said) (*protocol.AgySaid, error) {
	// 「[Opus 5.5] A silent turn is told from beside the turn's own updates.」
	var writing sync.Mutex
	write := func(rows []*types.As, err error) {
		writing.Lock()
		defer writing.Unlock()
		t.write(rows, err)
	}
	said.Silent = func(why string) { write(t.writes.agySilent(why, t.now())) }
	answer, err := said.Run(s.ctx, func(u acp.Update) { write(t.writes.rowsOfACP(u, agentLineAgy, t.now())) })
	if answer.Session != "" && answer.Session != said.Session {
		if keepErr := keepAgyConversation(agent.home, answer.Session); keepErr != nil {
			if err == nil {
				err = keepErr
			} else {
				err = errors.WithSecondaryError(err, keepErr)
			}
		}
	}
	if err != nil {
		t.write(t.writes.agyFailed(err.Error(), t.now()))
		return nil, err
	}
	t.write(t.writes.acpAnswered(answer.Text, t.now()))
	return &protocol.AgySaid{
		Answer: answer.Text, Session: t.session, Conversation: answer.Session, Model: said.Model, AgyVersion: agy.PinnedVersion,
		ContextUsed: int64(answer.Used), ContextSize: int64(answer.Size),
		TookMs: float64(answer.Took.Milliseconds()), Unwritten: t.unwritten(),
	}, nil
}

// 「[Opus 5.5] keepAgyConversation keeps the conversation agy answered in, whole or not at all.」
func keepAgyConversation(home, conversation string) error {
	f, err := os.CreateTemp(home, ".agy-conversation-*")
	if err != nil {
		return errors.Wrapf(err, "could not keep agy conversation %s in %s", conversation, home)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.WriteString(conversation + "\n"); err != nil {
		return errors.Wrapf(err, "could not write %s", f.Name())
	}
	if err := f.Close(); err != nil {
		return errors.Wrapf(err, "could not close %s", f.Name())
	}
	return errors.Wrapf(os.Rename(f.Name(), filepath.Join(home, agyConversationFile)), "could not keep agy conversation %s in %s", conversation, home)
}

// 「[Opus 5.5] agySilent is the row of a turn agy has said nothing of for a while, with what its log says.」
func (w sessionWriter) agySilent(why string, at time.Time) ([]*types.As, error) {
	row, err := w.row("Notification", at, map[string]any{"notification_type": "agy", "message": why})
	return []*types.As{row}, err
}

// 「[Opus 5.5] agyFailed is the row of a turn agy did not answer.」
func (w sessionWriter) agyFailed(why string, at time.Time) ([]*types.As, error) {
	row, err := w.row("StopFailure", at, map[string]any{"error": "agy", "last_assistant_message": why})
	return []*types.As{row}, err
}
