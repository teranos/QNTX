package server

// "APPROVALS INTEGRATE DIRECTLY WITH THE GITHUB APP": the App's webhook is
// where a pull request against main becomes an approval, where each check on
// its head is written down, and where main's CI concluding merges what waited.

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"

	"github.com/teranos/QNTX/ats/watcher"
	errors "github.com/teranos/sacred-error"
)

// gitHubPullRequestEvent is the part of a pull_request event an approval reads.
type gitHubPullRequestEvent struct {
	Action      string `json:"action"`
	Number      int64  `json:"number"`
	PullRequest struct {
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		Draft   bool   `json:"draft"`
		Merged  bool   `json:"merged"`
		Head    struct {
			Sha string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
	Repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
}

// gitHubCheckRunEvent is the part of a check_run event an approval reads.
type gitHubCheckRunEvent struct {
	Action   string `json:"action"`
	CheckRun struct {
		Name       string `json:"name"`
		HeadSha    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
	} `json:"check_run"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// gitHubCheckSuiteEvent is the part of a check_suite event an approval reads.
type gitHubCheckSuiteEvent struct {
	Action     string `json:"action"`
	CheckSuite struct {
		HeadBranch string `json:"head_branch"`
		HeadSha    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	} `json:"check_suite"`
	Repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
}

// checkState is the segment a check run is drawn as, from what GitHub said of
// it: queued is waiting, in progress is running, and completed is done or
// failed by its conclusion.
func checkState(status, conclusion string) string {
	switch status {
	case "completed":
		if concludedWell(conclusion) {
			return checkDone
		}
		return checkFailed
	case "in_progress":
		return checkRunning
	}
	return checkWaiting
}

// approvalSubject is the pull request's name: owner/repo#n.
func approvalSubject(repo string, number int64) string {
	return repo + "#" + strconv.FormatInt(number, 10)
}

// approvalsFromGitHub reads one delivery of the App's webhook for what it says
// about approvals, writes that down, and names the pull requests it touched.
// An event about nothing an approval reads touches none.
func (s *QNTXServer) approvalsFromGitHub(ctx context.Context, event string, body []byte) ([]string, error) {
	switch event {
	case "pull_request":
		var e gitHubPullRequestEvent
		if err := json.Unmarshal(body, &e); err != nil {
			return nil, errors.Wrap(err, "the pull_request event is not readable JSON")
		}
		return s.pullRequestEvent(e)
	case "check_run":
		var e gitHubCheckRunEvent
		if err := json.Unmarshal(body, &e); err != nil {
			return nil, errors.Wrap(err, "the check_run event is not readable JSON")
		}
		return s.checkRunEvent(e)
	case "check_suite":
		var e gitHubCheckSuiteEvent
		if err := json.Unmarshal(body, &e); err != nil {
			return nil, errors.Wrap(err, "the check_suite event is not readable JSON")
		}
		return s.checkSuiteEvent(ctx, e)
	}
	return []string{}, nil
}

// pullRequestEvent: a pull request against the default branch, not a draft,
// waits on the human from the moment it is opened, and again at each head it
// is pushed to. Closed, merged or made a draft, it waits on nobody.
func (s *QNTXServer) pullRequestEvent(e gitHubPullRequestEvent) ([]string, error) {
	subject := approvalSubject(e.Repository.FullName, e.Number)
	attrs := map[string]any{"sha": e.PullRequest.Head.Sha}
	switch e.Action {
	case "opened", "reopened", "ready_for_review", "synchronize":
		if e.PullRequest.Draft || e.PullRequest.Base.Ref != e.Repository.DefaultBranch {
			return []string{}, nil
		}
		attrs["repo"], attrs["pull"], attrs["base"] = e.Repository.FullName, e.Number, e.PullRequest.Base.Ref
		attrs["title"], attrs["link"], attrs["author"] = e.PullRequest.Title, e.PullRequest.HTMLURL, e.PullRequest.User.Login
		if err := s.approvalLine(s.nodeActor(), subject, watcher.ApprovalAsked, e.Repository.FullName, attrs); err != nil {
			return nil, err
		}
	case "closed", "converted_to_draft":
		attrs["merged"] = e.PullRequest.Merged
		if err := s.approvalLine(s.nodeActor(), subject, watcher.ApprovalClosed, e.Repository.FullName, attrs); err != nil {
			return nil, err
		}
	case "assigned", "unassigned", "labeled", "unlabeled", "edited", "locked", "unlocked",
		"review_requested", "review_request_removed", "milestoned", "demilestoned",
		"auto_merge_enabled", "auto_merge_disabled", "enqueued", "dequeued", "typed", "untyped":
		// What GitHub says of a pull request that is not about whether it
		// waits, or at which head.
		return []string{}, nil
	}
	if !slices.Contains(pullRequestActionsRead, e.Action) {
		return nil, errors.Newf("the pull_request action %q on %s is one this node does not read", e.Action, subject)
	}
	return []string{subject}, nil
}

// pullRequestActionsRead is every action the switch above answers with the
// pull request: the ones that make it wait, and the ones that end it.
var pullRequestActionsRead = []string{"opened", "reopened", "ready_for_review", "synchronize", "closed", "converted_to_draft"}

// checkRunEvent writes a check run down on every open approval waiting at its head.
func (s *QNTXServer) checkRunEvent(e gitHubCheckRunEvent) ([]string, error) {
	open, err := s.openApprovals()
	if err != nil {
		return nil, err
	}
	touched := []string{}
	for _, st := range open {
		if st.repo != e.Repository.FullName || st.sha != e.CheckRun.HeadSha {
			continue
		}
		if err := s.approvalLine(s.nodeActor(), st.subject, watcher.ApprovalChecked, st.repo, map[string]any{
			"sha": st.sha, "name": e.CheckRun.Name, "state": checkState(e.CheckRun.Status, e.CheckRun.Conclusion), "link": e.CheckRun.HTMLURL,
		}); err != nil {
			return nil, err
		}
		touched = append(touched, st.subject)
	}
	return touched, nil
}

// checkSuiteEvent: a suite concluding on the default branch is main's CI
// moving, and what waited on it is merged if it is green now.
func (s *QNTXServer) checkSuiteEvent(ctx context.Context, e gitHubCheckSuiteEvent) ([]string, error) {
	if e.Action != "completed" || e.CheckSuite.HeadBranch != e.Repository.DefaultBranch {
		return []string{}, nil
	}
	return s.mergeWhatWaits(ctx, e.Repository.FullName)
}
