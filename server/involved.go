package server

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// "IF WE KNOW SESSION A WAS INVOLVED, THATS WHY WE KNOW WHERE TO SEND"
// "IF SESSION A AND B ARE INVOLVED, THEY EACH GET THE PR ADDED ONTO THERI COLLECTION"
// "SO THEY KNOW DURING THE NEXT WAKE"
// "NO, ANY ONE THAT WAKES. PR MERGE UPDATES DONT WAKE PER SE, THEY GET COLLECTED ON A WAKING COLLECTION"
//
// A session is involved in a pull request when it asked GitHub about it
// through the node (github ask): the node writes that down. When the pull
// request is merged or closed, that is collected for each session that was
// involved, as something that wakes nobody: they know at their next wake.

// involvedIn is what the lines about who was involved in one pull request are
// about, the pull request by its name after it: owner/repo#n. Each line's
// predicate is one agent's DID.
const involvedIn = "INVOLVED IN "

// pullAskedAbout is the pull request an asking of GitHub is about, by its
// name, owner/repo#n: what was asked names it, or, for one that was opened by
// the asking, what GitHub answered does.
func pullAskedAbout(request []byte, answered map[string]any) (string, bool) {
	var asked struct {
		Owner       string `json:"owner"`
		Repo        string `json:"repo"`
		PullNumber  int64  `json:"pull_number"`
		IssueNumber int64  `json:"issue_number"`
	}
	if err := json.Unmarshal(request, &asked); err != nil || asked.Owner == "" || asked.Repo == "" {
		return "", false
	}
	number := asked.PullNumber
	if number == 0 {
		number = asked.IssueNumber
	}
	if number == 0 {
		// Opened by this asking: GitHub answers its number, and that it is a pull request by its head.
		if _, opened := answered["head"]; opened {
			if said, isOne := answered["number"].(float64); isOne {
				number = int64(said)
			}
		}
	}
	if number <= 0 {
		return "", false
	}
	return asked.Owner + "/" + asked.Repo + "#" + strconv.FormatInt(number, 10), true
}

// theAgentAsking is the DID of the agent that asks in ctx, by a key of its
// own, and empty where a person asks or nobody the node admitted.
func theAgentAsking(ctx context.Context) string {
	caller := sigil.Caller(ctx)
	if caller == nil {
		return ""
	}
	admitted, gated := auth.AdmissionFrom(caller.Context())
	if !gated {
		return ""
	}
	did, aPerson := itsOwnDID(admitted)
	if aPerson != nil {
		return ""
	}
	return did
}

// involvedKey is what the lines about one pull request are kept under. GitHub
// names a repository in any case, and what was asked may name it in another
// than GitHub does when it says the pull request is over: it is kept in one.
func involvedKey(kind, pull string) string {
	return kind + strings.ToLower(pull)
}

// wasInvolved writes down that the agent asking in ctx asked GitHub about a
// pull request, and answers what was not written down: an agent that is not
// written down is told nothing when the pull request is over.
func (g *gitHub) wasInvolved(ctx context.Context, operation, pull string) error {
	did := theAgentAsking(ctx)
	if did == "" {
		return nil
	}
	if err := g.host.nodeRecords().nodeRecord(did, involvedKey(involvedIn, pull), did, "_", map[string]any{"operation": operation}); err != nil {
		g.host.log().Errorw("That an agent was involved in a pull request was not written down: it is told nothing when it is merged",
			"pull", pull, "agent", did, "operation", operation, "error", err)
		return errors.Wrapf(err, "that you were involved in %s was not written down, so you are told nothing when it is merged or closed", pull)
	}
	return nil
}

// collectedForDID is whose collection an agent's DID names: the Node Agent's,
// or that of the session that holds a name by it. No name held, there is
// nowhere the node delivers to. Names that were not read are an error, and
// not a session without one.
func (s *QNTXServer) collectedForDID(did string) (collectedFor, bool, error) {
	if s.nodeAgent != nil && s.nodeAgent.did == did {
		return collectedFor{DID: did, Called: s.calledInMemory(s.nodeAgent.home), Model: s.theNodeAgentsModel()}, true, nil
	}
	names, err := s.agentNames()
	if err != nil {
		return collectedFor{}, false, errors.Wrapf(err, "the names agents hold were not read, so whose %s is is not known", did)
	}
	if at := slices.IndexFunc(names, func(kept agentName) bool { return kept.did == did && !kept.let }); at >= 0 {
		return collectedFor{DID: did, Called: names[at].name, Tenant: names[at].name}, true, nil
	}
	return collectedFor{}, false, nil
}

// toldOver is what the lines about who was told a pull request is over are
// about, the pull request after it. Each line's predicate is one agent's DID,
// and it says which closing it was told of.
const toldOver = "TOLD OVER "

// tellingOver is held for the whole of one telling: GitHub delivering the same
// closing twice tells each session once.
var tellingOver sync.Mutex

// pullIsOver collects, for each session that was involved in a pull request,
// that it was merged or closed. It wakes nobody. closedAt is GitHub's time of
// this closing, which names it: a session is told of one closing once, however
// often GitHub delivers it, and one that was not told is told when GitHub
// delivers it again. A pull request opened again and closed again is another
// closing.
func (s *QNTXServer) pullIsOver(subject, title, link, closedAt string, merged bool) error {
	tellingOver.Lock()
	defer tellingOver.Unlock()
	involved, err := newestLines(s, involvedKey(involvedIn, subject), "who was involved in a pull request")
	if err != nil {
		return err
	}
	told, err := newestLines(s, involvedKey(toldOver, subject), "who was told a pull request is over")
	if err != nil {
		return err
	}
	became := "was closed without being merged"
	if merged {
		became = "was merged"
	}
	failed := []string{}
	for did := range involved {
		if line, was := told[did]; was {
			if of, _ := line.Attributes["closed_at"].(string); of == closedAt {
				continue
			}
		}
		to, held, err := s.collectedForDID(did)
		if err != nil {
			failed = append(failed, err.Error())
			continue
		}
		if !held {
			s.logger.Infow("A session that was involved in a pull request holds no name now: it is told nothing of it", "pull", subject, "agent", did)
			continue
		}
		if _, err := s.collect(to, collectedSaid{From: "GitHub", Says: subject + " " + became + ": " + title + "\n" + link, At: time.Now(), Quiet: true}); err != nil {
			failed = append(failed, err.Error())
			continue
		}
		if err := s.nodeRecords().nodeRecord(s.nodeActor(), involvedKey(toldOver, subject), did, "_", map[string]any{"closed_at": closedAt, "merged": merged}); err != nil {
			// Collected and not written down: delivered again, GitHub's closing is collected twice for this one.
			s.logger.Errorw("That a session was told a pull request is over was not written down", "pull", subject, "agent", did, "error", err)
		}
	}
	if len(failed) > 0 {
		slices.Sort(failed)
		return errors.Newf("%s is over, and not every session involved in it was told: %s", subject, strings.Join(failed, "; "))
	}
	return nil
}
