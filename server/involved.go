package server

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
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

// wasInvolved writes down that the agent asking in ctx asked GitHub about a
// pull request. What is not written down is said in the log: the asking was
// answered either way.
func (g *gitHub) wasInvolved(ctx context.Context, operation string, request []byte, answered map[string]any) {
	pull, about := pullAskedAbout(request, answered)
	did := theAgentAsking(ctx)
	if !about || did == "" {
		return
	}
	if err := g.host.nodeRecords().nodeRecord(did, involvedIn+pull, did, "_", map[string]any{"operation": operation}); err != nil {
		g.host.log().Errorw("That an agent was involved in a pull request was not written down: it is told nothing when it is merged",
			"pull", pull, "agent", did, "operation", operation, "error", err)
	}
}

// collectedForDID is whose collection an agent's DID names: the Node Agent's,
// or that of the session that holds a name by it. No name held, there is
// nowhere the node delivers to.
func (s *QNTXServer) collectedForDID(did string) (collectedFor, bool) {
	if s.nodeAgent != nil && s.nodeAgent.did == did {
		return collectedFor{DID: did, Called: s.calledInMemory(s.nodeAgent.home), Model: s.theNodeAgentsModel()}, true
	}
	names, err := s.agentNames()
	if err != nil {
		return collectedFor{}, false
	}
	if at := slices.IndexFunc(names, func(kept agentName) bool { return kept.did == did && !kept.let }); at >= 0 {
		return collectedFor{DID: did, Called: names[at].name, Tenant: names[at].name}, true
	}
	return collectedFor{}, false
}

// pullIsOver collects, for each session that was involved in a pull request,
// that it was merged or closed. It wakes nobody.
func (s *QNTXServer) pullIsOver(subject, title, link string, merged bool) error {
	involved, err := newestLines(s, involvedIn+subject, "who was involved in a pull request")
	if err != nil {
		return err
	}
	became := "was closed without being merged"
	if merged {
		became = "was merged"
	}
	for did := range involved {
		to, held := s.collectedForDID(did)
		if !held {
			s.logger.Infow("A session that was involved in a pull request holds no name now: it is told nothing of it", "pull", subject, "agent", did)
			continue
		}
		if _, err := s.collect(to, collectedSaid{From: "GitHub", Says: subject + " " + became + ": " + title + "\n" + link, At: time.Now(), Quiet: true}); err != nil {
			return err
		}
	}
	return nil
}
