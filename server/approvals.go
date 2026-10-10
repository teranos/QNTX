package server

// Approvals (ADR-052): what waits on the human, and the human's answer.
//
// "approvals are attestations obviously": the ask, each check on its head,
// every press and the merge are lines in system about the pull request, and
// the element is a reading of them. "Approvals is human only" "And only
// through it's element": the answer is a route, no sigil and no tool, and it
// refuses a token however ROOT the token is. "approvals is ROOT only for now".
//
// "Click 1 means merge after CI passes, press again to force merge. If we
// still wait for CI, the NO will cancel the yes. Press NO again and it's
// definitely NO."

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

const (
	approvalsPath       = "/api/approvals"
	approvalsAnswerPath = approvalsPath + "/answer"
)

// The two options a merge approval has, and what each press of them says.
// The element shows the same words; the node takes no others.
const (
	optionMerge     = "Merge"
	optionDontMerge = "Don’t merge"

	saysMergeWhenReady = "merge when main CI passes"
	saysForceMerge     = "force merge"
	saysCancel         = "cancel the merge"
	saysDefinitelyNo   = "definitely no"
)

// approvalSays is what each option may say, in the order its presses say it.
var approvalSays = map[string][]string{
	optionMerge:     {saysMergeWhenReady, saysForceMerge},
	optionDontMerge: {saysCancel, saysDefinitelyNo},
}

// What a press did (protocol.ApprovalAnswered.did).
const (
	didMerged  = "merged"
	didWaiting = "waiting"
	didNothing = "nothing"
)

// approvalCheck is one check on the head, as the newest line about it says.
type approvalCheck struct {
	name, state, link string
}

// approvalState is one pull request as its lines read: the newest ask, the
// checks on that head, what stands, and whether it is over.
type approvalState struct {
	subject, repo, base, sha, title, link string
	pull                                  int64
	askedAt                               time.Time
	checks                                []approvalCheck
	option, said                          string
	answeredAt                            time.Time
	// over is merged or closed: nothing more is asked of anybody.
	over bool
}

// presented is whether every check on the head is done: until then it cannot
// be decided, and one failed it is not ready for approval at all.
func (a *approvalState) presented() bool {
	for _, c := range a.checks {
		if c.state != "done" {
			return false
		}
	}
	return true
}

// waits is whether what stands waits on main's CI.
func (a *approvalState) waits() bool {
	return a.said == saysMergeWhenReady
}

func (a *approvalState) owner() string {
	owner, _, _ := strings.Cut(a.repo, "/")
	return owner
}

func (a *approvalState) name() string {
	_, name, _ := strings.Cut(a.repo, "/")
	return name
}

// approvalLines is every approval line the node holds, oldest first.
func (s *QNTXServer) approvalLines() ([]*types.As, error) {
	if s.held == nil {
		return nil, errors.New("this node holds no store to keep its approvals in")
	}
	where := auth.NamespaceDefault
	if s.held.KeepsSystem() {
		where = auth.NamespaceSystem
	}
	reading, err := s.held.Read(where)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the approval lines in %s", where)
	}
	// One read per line kind: a filter naming several predicates reads as all
	// of them at once, which no line is.
	var lines []*types.As
	for _, predicate := range watcher.ApprovalPredicates {
		found, err := reading.GetAttestations(ats.AttestationFilter{Predicates: []string{predicate}, Limit: ats.EveryRow})
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read the %s lines in %s", predicate, where)
		}
		lines = append(lines, found...)
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Timestamp.Before(lines[j].Timestamp) })
	return lines, nil
}

// foldApprovals reads the lines into one state per pull request. A line about
// a head the newest ask is not at is history and is not read.
func foldApprovals(lines []*types.As) map[string]*approvalState {
	states := map[string]*approvalState{}
	for _, as := range lines {
		if len(as.Subjects) == 0 || len(as.Predicates) == 0 {
			continue
		}
		subject := as.Subjects[0]
		sha := attrString(as.Attributes, "sha")
		switch as.Predicates[0] {
		case watcher.ApprovalAsked:
			states[subject] = &approvalState{
				subject: subject, repo: attrString(as.Attributes, "repo"), pull: attrInt(as.Attributes, "pull"),
				sha: sha, base: attrString(as.Attributes, "base"), title: attrString(as.Attributes, "title"),
				link: attrString(as.Attributes, "link"), askedAt: as.Timestamp,
			}
			continue
		}
		st, asked := states[subject]
		if !asked || st.sha != sha {
			continue
		}
		switch as.Predicates[0] {
		case watcher.ApprovalChecked:
			check := approvalCheck{name: attrString(as.Attributes, "name"), state: attrString(as.Attributes, "state"), link: attrString(as.Attributes, "link")}
			if i := slices.IndexFunc(st.checks, func(c approvalCheck) bool { return c.name == check.name }); i >= 0 {
				st.checks[i] = check
			} else {
				st.checks = append(st.checks, check)
			}
		case watcher.ApprovalAnswered:
			st.option, st.said, st.answeredAt = attrString(as.Attributes, "option"), attrString(as.Attributes, "said"), as.Timestamp
		case watcher.ApprovalMerged, watcher.ApprovalClosed:
			st.over = true
		}
	}
	return states
}

// openApprovals is every pull request still waiting on somebody, newest first.
func (s *QNTXServer) openApprovals() ([]*approvalState, error) {
	lines, err := s.approvalLines()
	if err != nil {
		return nil, err
	}
	var open []*approvalState
	for _, st := range foldApprovals(lines) {
		if !st.over {
			open = append(open, st)
		}
	}
	sort.Slice(open, func(i, j int) bool { return open[i].askedAt.After(open[j].askedAt) })
	return open, nil
}

func (a *approvalState) proto() *protocol.Approval {
	checks := make([]*protocol.ApprovalCheck, 0, len(a.checks))
	for _, c := range a.checks {
		checks = append(checks, &protocol.ApprovalCheck{Name: c.name, State: c.state, Link: c.link})
	}
	return &protocol.Approval{
		Subject: a.subject, Title: a.title, Link: a.link, Repo: a.repo, Pull: a.pull, Sha: a.sha, Base: a.base,
		AskedAt: a.askedAt.UTC().Format(time.RFC3339Nano), Checks: checks, Option: a.option, Said: a.said, Waits: a.waits(),
	}
}

// approvalLine writes one line about a pull request, by whoever it is by.
func (s *QNTXServer) approvalLine(by, subject, predicate, repo string, attributes map[string]any) error {
	return s.nodeRecords().nodeRecord(by, subject, predicate, repo, attributes)
}

func (s *QNTXServer) approvalsSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "approvals",
			Description: "Approvals: every pull request against main that waits on ROOT, with the checks on its head and what stands. The answer is the human's, through the approvals element, and no sigil.",
			Tags:        []string{"approval", "pull request", "merge"},
			Sigils: []*protocol.Sigil{
				{
					Name:   "list",
					Does:   "Every pull request that waits on ROOT, newest first: its checks, and what stands on it.",
					Answer: "protocol.Approvals",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: approvalsPath},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"list": s.approvalsList,
		},
	}
}

func (s *QNTXServer) approvalsList(context.Context, sigil.Sent) (any, *protocol.Refusal) {
	open, err := s.openApprovals()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	answer := &protocol.Approvals{Approvals: make([]*protocol.Approval, 0, len(open))}
	for _, st := range open {
		answer.Approvals = append(answer.Approvals, st.proto())
	}
	return answer, nil
}

// HandleApprovalAnswer takes one press: the human's, at ROOT, in a session.
// "Me, the human the logged in user". A token is refused whatever it holds,
// because a tool reaches it and the agent reaches every tool.
func (s *QNTXServer) HandleApprovalAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, approvalsAnswerPath+" answers POST, and this was "+r.Method)
		return
	}
	admitted, ok := auth.AdmissionFrom(r.Context())
	switch {
	case !ok:
		writeError(w, http.StatusUnauthorized, "an approval is answered by the logged-in human, and nobody is logged in")
		return
	case admitted.Grant != nil:
		writeError(w, http.StatusForbidden, "an approval is answered by the logged-in human, not by a token ("+admitted.Grant.Label+")")
		return
	case !admitted.IsRoot():
		writeError(w, http.StatusForbidden, "approvals are ROOT's, and this session is "+admitted.LevelName())
		return
	case admitted.Identity == "":
		writeError(w, http.StatusForbidden, "an approval is answered by somebody, and this session names nobody")
		return
	}
	var sent struct {
		Subject string `json:"subject"`
		Sha     string `json:"sha"`
		Option  string `json:"option"`
		Said    string `json:"said"`
	}
	if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
		writeError(w, http.StatusBadRequest, "what was sent is not a JSON object naming subject, sha, option and said: "+err.Error())
		return
	}
	says, known := approvalSays[sent.Option]
	if !known || !slices.Contains(says, sent.Said) {
		writeError(w, http.StatusBadRequest, "no press says "+strconv.Quote(sent.Said)+" of "+strconv.Quote(sent.Option))
		return
	}
	open, err := s.openApprovals()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	i := slices.IndexFunc(open, func(st *approvalState) bool { return st.subject == sent.Subject })
	if i < 0 {
		writeError(w, http.StatusNotFound, sent.Subject+" waits on nobody")
		return
	}
	st := open[i]
	if st.sha != sent.Sha {
		writeError(w, http.StatusConflict, sent.Subject+" moved: it waits at "+st.sha+", and the press was about "+sent.Sha)
		return
	}
	if !st.presented() {
		writeError(w, http.StatusConflict, sent.Subject+" cannot be decided until every check on "+st.sha+" has passed")
		return
	}
	if err := s.approvalLine(admitted.Identity, st.subject, watcher.ApprovalAnswered, st.repo,
		map[string]any{"sha": st.sha, "option": sent.Option, "said": sent.Said}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	st.option, st.said = sent.Option, sent.Said

	answered := &protocol.ApprovalAnswered{Subject: st.subject, Sha: st.sha, Option: st.option, Said: st.said, Did: didNothing}
	switch st.said {
	case saysForceMerge:
		mergeSha, err := s.mergeApproval(r.Context(), st, admitted.Identity)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		answered.Did, answered.MergeSha = didMerged, mergeSha
	case saysMergeWhenReady:
		answered.Did = didWaiting
		green, err := s.mainGreen(r.Context(), st)
		if err != nil {
			s.logger.Warnw("main's CI was not asked; the merge waits for its next suite", "subject", st.subject, "error", err)
			break
		}
		if green {
			mergeSha, err := s.mergeApproval(r.Context(), st, admitted.Identity)
			if err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
			answered.Did, answered.MergeSha = didMerged, mergeSha
		}
	}
	respond(w, s.logger, http.StatusOK, answered)
}

// mergeApproval merges the pull request at the head the human saw, as the
// App's installation where the repository is, and writes the merge down by
// the human: the node did it, and the human decided it.
func (s *QNTXServer) mergeApproval(ctx context.Context, st *approvalState, by string) (string, error) {
	said, err := s.gitHubService().MergeAPullRequest(services.AsInstallation(ctx), &protocol.GitHubMergeAPullRequestRequest{
		Owner: st.owner(), Repo: st.name(), PullNumber: st.pull, Sha: st.sha, MergeMethod: "merge",
	})
	if err != nil {
		return "", errors.Wrapf(err, "GitHub was not asked to merge %s", st.subject)
	}
	if !said.GetSuccess() {
		if err := s.approvalLine(s.nodeActor(), st.subject, watcher.ApprovalFailed, st.repo, map[string]any{"sha": st.sha, "error": said.GetError()}); err != nil {
			s.logger.Errorw("GitHub refused a merge and the refusal was not written down", "subject", st.subject, "error", err)
		}
		return "", errors.Newf("GitHub refused to merge %s at %s: %s", st.subject, st.sha, said.GetError())
	}
	if err := s.approvalLine(by, st.subject, watcher.ApprovalMerged, st.repo, map[string]any{"sha": st.sha, "merge_sha": said.GetSha()}); err != nil {
		return "", errors.Wrapf(err, "%s was merged and the merge was not written down", st.subject)
	}
	st.over = true
	return said.GetSha(), nil
}

// mainGreen is whether every check suite at the head of the branch the pull
// request targets has concluded well. A branch no suite runs on waits on
// nothing.
func (s *QNTXServer) mainGreen(ctx context.Context, st *approvalState) (bool, error) {
	said, err := s.gitHubService().ListCheckSuitesForAGitReference(services.AsInstallation(ctx), &protocol.GitHubListCheckSuitesForAGitReferenceRequest{
		Owner: st.owner(), Repo: st.name(), Ref: st.base, PerPage: 100,
	})
	if err != nil {
		return false, errors.Wrapf(err, "GitHub was not asked about %s's check suites", st.base)
	}
	if !said.GetSuccess() {
		return false, errors.Newf("%s's check suites were not listed: %s", st.base, said.GetError())
	}
	for _, suite := range said.GetCheckSuites() {
		if suite.GetLatestCheckRunsCount() == 0 {
			continue
		}
		if suite.GetStatus() != "completed" || !concludedWell(suite.GetConclusion()) {
			return false, nil
		}
	}
	return true, nil
}

// concludedWell is a conclusion GitHub itself merges on.
func concludedWell(conclusion string) bool {
	return conclusion == "success" || conclusion == "neutral" || conclusion == "skipped"
}

// mergeWhatWaits merges every pull request of repo whose standing answer waits
// on main's CI, now that main's CI concluded. Each is asked of GitHub again:
// the suite that concluded may not have been the last.
func (s *QNTXServer) mergeWhatWaits(ctx context.Context, repo string) []string {
	open, err := s.openApprovals()
	if err != nil {
		s.logger.Errorw("main's CI concluded and the approvals were not read", "repo", repo, "error", err)
		return []string{}
	}
	merged := []string{}
	for _, st := range open {
		if st.repo != repo || !st.waits() {
			continue
		}
		green, err := s.mainGreen(ctx, st)
		if err != nil {
			s.logger.Warnw("main's CI was not asked", "subject", st.subject, "error", err)
			continue
		}
		if !green {
			continue
		}
		by, err := s.approvalAnsweredBy(st)
		if err != nil {
			s.logger.Errorw("who answered was not read, so nothing was merged for them", "subject", st.subject, "error", err)
			continue
		}
		if _, err := s.mergeApproval(ctx, st, by); err != nil {
			s.logger.Errorw("the merge the human waited for did not happen", "subject", st.subject, "error", err)
			continue
		}
		merged = append(merged, st.subject)
	}
	return merged
}

// approvalAnsweredBy is who wrote the standing answer: the human the merge is by.
func (s *QNTXServer) approvalAnsweredBy(st *approvalState) (string, error) {
	lines, err := s.approvalLines()
	if err != nil {
		return "", err
	}
	by := ""
	for _, as := range lines {
		if len(as.Subjects) > 0 && as.Subjects[0] == st.subject && len(as.Predicates) > 0 && as.Predicates[0] == watcher.ApprovalAnswered &&
			attrString(as.Attributes, "sha") == st.sha && len(as.Actors) > 0 {
			by = as.Actors[0]
		}
	}
	if by == "" {
		return "", errors.Newf("no line says who answered %s at %s", st.subject, st.sha)
	}
	return by, nil
}

// attrInt reads a number an attribute holds, however the store gave it back.
func attrInt(attrs map[string]any, key string) int64 {
	switch v := attrs[key].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}
