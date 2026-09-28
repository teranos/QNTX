package services

import (
	"context"
	"net/http"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// githubRoutes is where each GitHubService RPC goes on GitHub, keyed by RPC
// name, each under the page that documents it.
var githubRoutes = map[string]githubRoute{
	// https://docs.github.com/en/rest/rate-limit/rate-limit?apiVersion=2026-03-10
	"RateLimit": {method: http.MethodGet, path: "/rate_limit"},
	// https://docs.github.com/en/rest/commits/statuses?apiVersion=2026-03-10#get-the-combined-status-for-a-specific-reference
	"GetTheCombinedStatusForASpecificReference": {method: http.MethodGet, path: "/repos/{owner}/{repo}/commits/{ref}/status", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/commits/statuses?apiVersion=2026-03-10#list-commit-statuses-for-a-reference
	"ListCommitStatusesForAReference": {method: http.MethodGet, path: "/repos/{owner}/{repo}/commits/{ref}/statuses", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/commits/statuses?apiVersion=2026-03-10#create-a-commit-status
	"CreateACommitStatus": {method: http.MethodPost, path: "/repos/{owner}/{repo}/statuses/{sha}", body: []string{"state", "target_url", "description", "context"}},
	// https://docs.github.com/en/rest/commits/comments?apiVersion=2026-03-10#list-commit-comments-for-a-repository
	"ListCommitCommentsForARepository": {method: http.MethodGet, path: "/repos/{owner}/{repo}/comments", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/commits/comments?apiVersion=2026-03-10#get-a-commit-comment
	"GetACommitComment": {method: http.MethodGet, path: "/repos/{owner}/{repo}/comments/{comment_id}"},
	// https://docs.github.com/en/rest/commits/comments?apiVersion=2026-03-10#update-a-commit-comment
	"UpdateACommitComment": {method: http.MethodPatch, path: "/repos/{owner}/{repo}/comments/{comment_id}", body: []string{"body"}},
	// https://docs.github.com/en/rest/commits/comments?apiVersion=2026-03-10#delete-a-commit-comment
	"DeleteACommitComment": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/comments/{comment_id}"},
	// https://docs.github.com/en/rest/commits/comments?apiVersion=2026-03-10#list-commit-comments
	"ListCommitComments": {method: http.MethodGet, path: "/repos/{owner}/{repo}/commits/{commit_sha}/comments", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/commits/comments?apiVersion=2026-03-10#create-a-commit-comment
	"CreateACommitComment": {method: http.MethodPost, path: "/repos/{owner}/{repo}/commits/{commit_sha}/comments", body: []string{"body", "path", "position", "line"}},
	// https://docs.github.com/en/rest/commits/commits?apiVersion=2026-03-10#list-commits
	"ListCommits": {method: http.MethodGet, path: "/repos/{owner}/{repo}/commits", query: []string{"sha", "path", "author", "committer", "since", "until", "per_page", "page"}},
	// https://docs.github.com/en/rest/commits/commits?apiVersion=2026-03-10#list-branches-for-head-commit
	"ListBranchesForHEADCommit": {method: http.MethodGet, path: "/repos/{owner}/{repo}/commits/{commit_sha}/branches-where-head"},
	// https://docs.github.com/en/rest/commits/commits?apiVersion=2026-03-10#list-pull-requests-associated-with-a-commit
	"ListPullRequestsAssociatedWithACommit": {method: http.MethodGet, path: "/repos/{owner}/{repo}/commits/{commit_sha}/pulls", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/commits/commits?apiVersion=2026-03-10#get-a-commit
	"GetACommit": {method: http.MethodGet, path: "/repos/{owner}/{repo}/commits/{ref}", query: []string{"page", "per_page"}},
	// https://docs.github.com/en/rest/commits/commits?apiVersion=2026-03-10#compare-two-commits
	"CompareTwoCommits": {method: http.MethodGet, path: "/repos/{owner}/{repo}/compare/{basehead}", query: []string{"page", "per_page"}},
	// https://docs.github.com/en/rest/branches/branches?apiVersion=2026-03-10#list-branches
	"ListBranches": {method: http.MethodGet, path: "/repos/{owner}/{repo}/branches", query: []string{"protected", "per_page", "page"}},
	// https://docs.github.com/en/rest/branches/branches?apiVersion=2026-03-10#get-a-branch
	"GetABranch": {method: http.MethodGet, path: "/repos/{owner}/{repo}/branches/{branch}"},
	// https://docs.github.com/en/rest/branches/branches?apiVersion=2026-03-10#rename-a-branch
	"RenameABranch": {method: http.MethodPost, path: "/repos/{owner}/{repo}/branches/{branch}/rename", body: []string{"new_name"}},
	// https://docs.github.com/en/rest/branches/branches?apiVersion=2026-03-10#sync-a-fork-branch-with-the-upstream-repository
	"SyncAForkBranchWithTheUpstreamRepository": {method: http.MethodPost, path: "/repos/{owner}/{repo}/merge-upstream", body: []string{"branch"}},
	// https://docs.github.com/en/rest/branches/branches?apiVersion=2026-03-10#merge-a-branch
	"MergeABranch": {method: http.MethodPost, path: "/repos/{owner}/{repo}/merges", body: []string{"base", "head", "commit_message"}},
	// https://docs.github.com/en/rest/pulls/review-requests?apiVersion=2026-03-10#get-all-requested-reviewers-for-a-pull-request
	"GetAllRequestedReviewersForAPullRequest": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls/{pull_number}/requested_reviewers"},
	// https://docs.github.com/en/rest/pulls/review-requests?apiVersion=2026-03-10#request-reviewers-for-a-pull-request
	"RequestReviewersForAPullRequest": {method: http.MethodPost, path: "/repos/{owner}/{repo}/pulls/{pull_number}/requested_reviewers", body: []string{"reviewers", "team_reviewers"}},
	// https://docs.github.com/en/rest/pulls/review-requests?apiVersion=2026-03-10#remove-requested-reviewers-from-a-pull-request
	"RemoveRequestedReviewersFromAPullRequest": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/pulls/{pull_number}/requested_reviewers", body: []string{"reviewers", "team_reviewers"}},
	// https://docs.github.com/en/rest/pulls/reviews?apiVersion=2026-03-10#list-reviews-for-a-pull-request
	"ListReviewsForAPullRequest": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls/{pull_number}/reviews", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/pulls/reviews?apiVersion=2026-03-10#create-a-review-for-a-pull-request
	"CreateAReviewForAPullRequest": {method: http.MethodPost, path: "/repos/{owner}/{repo}/pulls/{pull_number}/reviews", body: []string{"commit_id", "body", "event", "comments"}},
	// https://docs.github.com/en/rest/pulls/reviews?apiVersion=2026-03-10#get-a-review-for-a-pull-request
	"GetAReviewForAPullRequest": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls/{pull_number}/reviews/{review_id}"},
	// https://docs.github.com/en/rest/pulls/reviews?apiVersion=2026-03-10#update-a-review-for-a-pull-request
	"UpdateAReviewForAPullRequest": {method: http.MethodPut, path: "/repos/{owner}/{repo}/pulls/{pull_number}/reviews/{review_id}", body: []string{"body"}},
	// https://docs.github.com/en/rest/pulls/reviews?apiVersion=2026-03-10#delete-a-pending-review-for-a-pull-request
	"DeleteAPendingReviewForAPullRequest": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/pulls/{pull_number}/reviews/{review_id}"},
	// https://docs.github.com/en/rest/pulls/reviews?apiVersion=2026-03-10#list-comments-for-a-pull-request-review
	"ListCommentsForAPullRequestReview": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls/{pull_number}/reviews/{review_id}/comments", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/pulls/reviews?apiVersion=2026-03-10#dismiss-a-review-for-a-pull-request
	"DismissAReviewForAPullRequest": {method: http.MethodPut, path: "/repos/{owner}/{repo}/pulls/{pull_number}/reviews/{review_id}/dismissals", body: []string{"message", "event"}},
	// https://docs.github.com/en/rest/pulls/reviews?apiVersion=2026-03-10#submit-a-review-for-a-pull-request
	"SubmitAReviewForAPullRequest": {method: http.MethodPost, path: "/repos/{owner}/{repo}/pulls/{pull_number}/reviews/{review_id}/events", body: []string{"body", "event"}},
	// https://docs.github.com/en/rest/pulls/comments?apiVersion=2026-03-10#list-review-comments-in-a-repository
	"ListReviewCommentsInARepository": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls/comments", query: []string{"sort", "direction", "since", "per_page", "page"}},
	// https://docs.github.com/en/rest/pulls/comments?apiVersion=2026-03-10#get-a-review-comment-for-a-pull-request
	"GetAReviewCommentForAPullRequest": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls/comments/{comment_id}"},
	// https://docs.github.com/en/rest/pulls/comments?apiVersion=2026-03-10#update-a-review-comment-for-a-pull-request
	"UpdateAReviewCommentForAPullRequest": {method: http.MethodPatch, path: "/repos/{owner}/{repo}/pulls/comments/{comment_id}", body: []string{"body"}},
	// https://docs.github.com/en/rest/pulls/comments?apiVersion=2026-03-10#delete-a-review-comment-for-a-pull-request
	"DeleteAReviewCommentForAPullRequest": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/pulls/comments/{comment_id}"},
	// https://docs.github.com/en/rest/pulls/comments?apiVersion=2026-03-10#list-review-comments-on-a-pull-request
	"ListReviewCommentsOnAPullRequest": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls/{pull_number}/comments", query: []string{"sort", "direction", "since", "per_page", "page"}},
	// https://docs.github.com/en/rest/pulls/comments?apiVersion=2026-03-10#create-a-review-comment-for-a-pull-request
	"CreateAReviewCommentForAPullRequest": {method: http.MethodPost, path: "/repos/{owner}/{repo}/pulls/{pull_number}/comments", body: []string{"body", "commit_id", "path", "position", "side", "line", "start_line", "start_side", "in_reply_to", "subject_type"}},
	// https://docs.github.com/en/rest/pulls/comments?apiVersion=2026-03-10#create-a-reply-for-a-review-comment
	"CreateAReplyForAReviewComment": {method: http.MethodPost, path: "/repos/{owner}/{repo}/pulls/{pull_number}/comments/{comment_id}/replies", body: []string{"body"}},
	// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#list-pull-requests
	"ListPullRequests": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls", query: []string{"state", "head", "base", "sort", "direction", "per_page", "page"}},
	// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#create-a-pull-request
	"CreateAPullRequest": {method: http.MethodPost, path: "/repos/{owner}/{repo}/pulls", body: []string{"title", "head", "head_repo", "base", "body", "maintainer_can_modify", "draft", "issue"}},
	// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#get-a-pull-request
	"GetAPullRequest": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls/{pull_number}"},
	// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#update-a-pull-request
	"UpdateAPullRequest": {method: http.MethodPatch, path: "/repos/{owner}/{repo}/pulls/{pull_number}", body: []string{"title", "body", "state", "base", "maintainer_can_modify"}},
	// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#list-commits-on-a-pull-request
	"ListCommitsOnAPullRequest": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls/{pull_number}/commits", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#list-pull-requests-files
	"ListPullRequestsFiles": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls/{pull_number}/files", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#check-if-a-pull-request-has-been-merged
	"CheckIfAPullRequestHasBeenMerged": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls/{pull_number}/merge"},
	// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#merge-a-pull-request
	"MergeAPullRequest": {method: http.MethodPut, path: "/repos/{owner}/{repo}/pulls/{pull_number}/merge", body: []string{"commit_title", "commit_message", "sha", "merge_method"}},
	// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#merge-a-pull-request-asynchronously
	"MergeAPullRequestAsynchronously": {method: http.MethodPut, path: "/repos/{owner}/{repo}/pulls/{pull_number}/merge-async", body: []string{"commit_title", "commit_message", "sha", "merge_method", "merge_action"}},
	// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#get-the-result-of-an-asynchronous-merge
	"GetTheResultOfAnAsynchronousMerge": {method: http.MethodGet, path: "/repos/{owner}/{repo}/pulls/{pull_number}/merge-async/{uuid}"},
	// https://docs.github.com/en/rest/pulls/pulls?apiVersion=2026-03-10#update-a-pull-request-branch
	"UpdateAPullRequestBranch": {method: http.MethodPut, path: "/repos/{owner}/{repo}/pulls/{pull_number}/update-branch", body: []string{"expected_head_sha"}},
	// https://docs.github.com/en/rest/actions/workflows?apiVersion=2026-03-10#list-repository-workflows
	"ListRepositoryWorkflows": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/workflows", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/actions/workflows?apiVersion=2026-03-10#get-a-workflow
	"GetAWorkflow": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/workflows/{workflow_id}"},
	// https://docs.github.com/en/rest/actions/workflows?apiVersion=2026-03-10#disable-a-workflow
	"DisableAWorkflow": {method: http.MethodPut, path: "/repos/{owner}/{repo}/actions/workflows/{workflow_id}/disable"},
	// https://docs.github.com/en/rest/actions/workflows?apiVersion=2026-03-10#create-a-workflow-dispatch-event
	"CreateAWorkflowDispatchEvent": {method: http.MethodPost, path: "/repos/{owner}/{repo}/actions/workflows/{workflow_id}/dispatches", body: []string{"ref", "inputs"}},
	// https://docs.github.com/en/rest/actions/workflows?apiVersion=2026-03-10#enable-a-workflow
	"EnableAWorkflow": {method: http.MethodPut, path: "/repos/{owner}/{repo}/actions/workflows/{workflow_id}/enable"},
	// https://docs.github.com/en/rest/actions/workflows?apiVersion=2026-03-10#get-workflow-usage
	"GetWorkflowUsage": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/workflows/{workflow_id}/timing"},
	// https://docs.github.com/en/rest/actions/workflow-jobs?apiVersion=2026-03-10#get-a-job-for-a-workflow-run
	"GetAJobForAWorkflowRun": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/jobs/{job_id}"},
	// https://docs.github.com/en/rest/actions/workflow-jobs?apiVersion=2026-03-10#download-job-logs-for-a-workflow-run
	"DownloadJobLogsForAWorkflowRun": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/jobs/{job_id}/logs"},
	// https://docs.github.com/en/rest/actions/workflow-jobs?apiVersion=2026-03-10#list-jobs-for-a-workflow-run-attempt
	"ListJobsForAWorkflowRunAttempt": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/attempts/{attempt_number}/jobs", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/actions/workflow-jobs?apiVersion=2026-03-10#list-jobs-for-a-workflow-run
	"ListJobsForAWorkflowRun": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/jobs", query: []string{"filter", "per_page", "page"}},
	// https://docs.github.com/en/rest/actions/artifacts?apiVersion=2026-03-10#list-artifacts-for-a-repository
	"ListArtifactsForARepository": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/artifacts", query: []string{"per_page", "page", "name"}},
	// https://docs.github.com/en/rest/actions/artifacts?apiVersion=2026-03-10#get-an-artifact
	"GetAnArtifact": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/artifacts/{artifact_id}"},
	// https://docs.github.com/en/rest/actions/artifacts?apiVersion=2026-03-10#delete-an-artifact
	"DeleteAnArtifact": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/actions/artifacts/{artifact_id}"},
	// https://docs.github.com/en/rest/actions/artifacts?apiVersion=2026-03-10#download-an-artifact
	"DownloadAnArtifact": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/artifacts/{artifact_id}/{archive_format}"},
	// https://docs.github.com/en/rest/actions/artifacts?apiVersion=2026-03-10#list-workflow-run-artifacts
	"ListWorkflowRunArtifacts": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/artifacts", query: []string{"per_page", "page", "name", "direction"}},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#re-run-a-job-from-a-workflow-run
	"ReRunAJobFromAWorkflowRun": {method: http.MethodPost, path: "/repos/{owner}/{repo}/actions/jobs/{job_id}/rerun", body: []string{"enable_debug_logging", "enable_debugger"}},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#list-workflow-runs-for-a-repository
	"ListWorkflowRunsForARepository": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/runs", query: []string{"actor", "branch", "event", "status", "per_page", "page", "created", "exclude_pull_requests", "check_suite_id", "head_sha"}},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#get-a-workflow-run
	"GetAWorkflowRun": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/runs/{run_id}", query: []string{"exclude_pull_requests"}},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#delete-a-workflow-run
	"DeleteAWorkflowRun": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/actions/runs/{run_id}"},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#get-the-review-history-for-a-workflow-run
	"GetTheReviewHistoryForAWorkflowRun": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/approvals"},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#approve-a-workflow-run-for-a-fork-pull-request
	"ApproveAWorkflowRunForAForkPullRequest": {method: http.MethodPost, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/approve"},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#get-a-workflow-run-attempt
	"GetAWorkflowRunAttempt": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/attempts/{attempt_number}", query: []string{"exclude_pull_requests"}},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#download-workflow-run-attempt-logs
	"DownloadWorkflowRunAttemptLogs": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/attempts/{attempt_number}/logs"},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#cancel-a-workflow-run
	"CancelAWorkflowRun": {method: http.MethodPost, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/cancel"},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#review-custom-deployment-protection-rules-for-a-workflow-run
	"ReviewCustomDeploymentProtectionRulesForAWorkflowRun": {method: http.MethodPost, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/deployment_protection_rule"},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#force-cancel-a-workflow-run
	"ForceCancelAWorkflowRun": {method: http.MethodPost, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/force-cancel"},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#download-workflow-run-logs
	"DownloadWorkflowRunLogs": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/logs"},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#delete-workflow-run-logs
	"DeleteWorkflowRunLogs": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/logs"},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#get-pending-deployments-for-a-workflow-run
	"GetPendingDeploymentsForAWorkflowRun": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/pending_deployments"},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#review-pending-deployments-for-a-workflow-run
	"ReviewPendingDeploymentsForAWorkflowRun": {method: http.MethodPost, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/pending_deployments", body: []string{"environment_ids", "state", "comment"}},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#re-run-a-workflow
	"ReRunAWorkflow": {method: http.MethodPost, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/rerun", body: []string{"enable_debug_logging"}},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#re-run-failed-jobs-from-a-workflow-run
	"ReRunFailedJobsFromAWorkflowRun": {method: http.MethodPost, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/rerun-failed-jobs", body: []string{"enable_debug_logging"}},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#get-workflow-run-usage
	"GetWorkflowRunUsage": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/runs/{run_id}/timing"},
	// https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2026-03-10#list-workflow-runs-for-a-workflow
	"ListWorkflowRunsForAWorkflow": {method: http.MethodGet, path: "/repos/{owner}/{repo}/actions/workflows/{workflow_id}/runs", query: []string{"actor", "branch", "event", "status", "per_page", "page", "created", "exclude_pull_requests", "check_suite_id", "head_sha"}},
	// https://docs.github.com/en/rest/checks/runs?apiVersion=2026-03-10#create-a-check-run
	"CreateACheckRun": {method: http.MethodPost, path: "/repos/{owner}/{repo}/check-runs", body: []string{"status"}},
	// https://docs.github.com/en/rest/checks/runs?apiVersion=2026-03-10#get-a-check-run
	"GetACheckRun": {method: http.MethodGet, path: "/repos/{owner}/{repo}/check-runs/{check_run_id}"},
	// https://docs.github.com/en/rest/checks/runs?apiVersion=2026-03-10#update-a-check-run
	"UpdateACheckRun": {method: http.MethodPatch, path: "/repos/{owner}/{repo}/check-runs/{check_run_id}", body: []string{"name", "details_url", "external_id", "started_at", "status", "conclusion", "completed_at", "output", "actions"}},
	// https://docs.github.com/en/rest/checks/runs?apiVersion=2026-03-10#list-check-run-annotations
	"ListCheckRunAnnotations": {method: http.MethodGet, path: "/repos/{owner}/{repo}/check-runs/{check_run_id}/annotations", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/checks/runs?apiVersion=2026-03-10#rerequest-a-check-run
	"RerequestACheckRun": {method: http.MethodPost, path: "/repos/{owner}/{repo}/check-runs/{check_run_id}/rerequest"},
	// https://docs.github.com/en/rest/checks/runs?apiVersion=2026-03-10#list-check-runs-in-a-check-suite
	"ListCheckRunsInACheckSuite": {method: http.MethodGet, path: "/repos/{owner}/{repo}/check-suites/{check_suite_id}/check-runs", query: []string{"check_name", "status", "filter", "per_page", "page"}},
	// https://docs.github.com/en/rest/checks/runs?apiVersion=2026-03-10#list-check-runs-for-a-git-reference
	"ListCheckRunsForAGitReference": {method: http.MethodGet, path: "/repos/{owner}/{repo}/commits/{ref}/check-runs", query: []string{"check_name", "status", "filter", "per_page", "page", "app_id"}},
	// https://docs.github.com/en/rest/checks/suites?apiVersion=2026-03-10#create-a-check-suite
	"CreateACheckSuite": {method: http.MethodPost, path: "/repos/{owner}/{repo}/check-suites", body: []string{"head_sha"}},
	// https://docs.github.com/en/rest/checks/suites?apiVersion=2026-03-10#update-repository-preferences-for-check-suites
	"UpdateRepositoryPreferencesForCheckSuites": {method: http.MethodPatch, path: "/repos/{owner}/{repo}/check-suites/preferences", body: []string{"auto_trigger_checks"}},
	// https://docs.github.com/en/rest/checks/suites?apiVersion=2026-03-10#get-a-check-suite
	"GetACheckSuite": {method: http.MethodGet, path: "/repos/{owner}/{repo}/check-suites/{check_suite_id}"},
	// https://docs.github.com/en/rest/checks/suites?apiVersion=2026-03-10#rerequest-a-check-suite
	"RerequestACheckSuite": {method: http.MethodPost, path: "/repos/{owner}/{repo}/check-suites/{check_suite_id}/rerequest"},
	// https://docs.github.com/en/rest/checks/suites?apiVersion=2026-03-10#list-check-suites-for-a-git-reference
	"ListCheckSuitesForAGitReference": {method: http.MethodGet, path: "/repos/{owner}/{repo}/commits/{ref}/check-suites", query: []string{"app_id", "check_name", "per_page", "page"}},
	// https://docs.github.com/en/rest/issues/comments?apiVersion=2026-03-10#list-issue-comments-for-a-repository
	"ListIssueCommentsForARepository": {method: http.MethodGet, path: "/repos/{owner}/{repo}/issues/comments", query: []string{"sort", "direction", "since", "per_page", "page"}},
	// https://docs.github.com/en/rest/issues/comments?apiVersion=2026-03-10#get-an-issue-comment
	"GetAnIssueComment": {method: http.MethodGet, path: "/repos/{owner}/{repo}/issues/comments/{comment_id}"},
	// https://docs.github.com/en/rest/issues/comments?apiVersion=2026-03-10#update-an-issue-comment
	"UpdateAnIssueComment": {method: http.MethodPatch, path: "/repos/{owner}/{repo}/issues/comments/{comment_id}", body: []string{"body"}},
	// https://docs.github.com/en/rest/issues/comments?apiVersion=2026-03-10#delete-an-issue-comment
	"DeleteAnIssueComment": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/issues/comments/{comment_id}"},
	// https://docs.github.com/en/rest/issues/comments?apiVersion=2026-03-10#pin-an-issue-comment
	"PinAnIssueComment": {method: http.MethodPut, path: "/repos/{owner}/{repo}/issues/comments/{comment_id}/pin"},
	// https://docs.github.com/en/rest/issues/comments?apiVersion=2026-03-10#unpin-an-issue-comment
	"UnpinAnIssueComment": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/issues/comments/{comment_id}/pin"},
	// https://docs.github.com/en/rest/issues/comments?apiVersion=2026-03-10#list-issue-comments
	"ListIssueComments": {method: http.MethodGet, path: "/repos/{owner}/{repo}/issues/{issue_number}/comments", query: []string{"since", "per_page", "page"}},
	// https://docs.github.com/en/rest/issues/comments?apiVersion=2026-03-10#create-an-issue-comment
	"CreateAnIssueComment": {method: http.MethodPost, path: "/repos/{owner}/{repo}/issues/{issue_number}/comments", body: []string{"body"}},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#list-failed-organization-invitations
	"ListFailedOrganizationInvitations": {method: http.MethodGet, path: "/orgs/{org}/failed_invitations", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#list-pending-organization-invitations
	"ListPendingOrganizationInvitations": {method: http.MethodGet, path: "/orgs/{org}/invitations", query: []string{"per_page", "page", "role", "invitation_source"}},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#create-an-organization-invitation
	"CreateAnOrganizationInvitation": {method: http.MethodPost, path: "/orgs/{org}/invitations", body: []string{"invitee_id", "email", "role", "team_ids"}},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#cancel-an-organization-invitation
	"CancelAnOrganizationInvitation": {method: http.MethodDelete, path: "/orgs/{org}/invitations/{invitation_id}"},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#list-organization-invitation-teams
	"ListOrganizationInvitationTeams": {method: http.MethodGet, path: "/orgs/{org}/invitations/{invitation_id}/teams", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#list-organization-members
	"ListOrganizationMembers": {method: http.MethodGet, path: "/orgs/{org}/members", query: []string{"filter", "role", "per_page", "page"}},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#check-organization-membership-for-a-user
	"CheckOrganizationMembershipForAUser": {method: http.MethodGet, path: "/orgs/{org}/members/{username}"},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#remove-an-organization-member
	"RemoveAnOrganizationMember": {method: http.MethodDelete, path: "/orgs/{org}/members/{username}"},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#get-organization-membership-for-a-user
	"GetOrganizationMembershipForAUser": {method: http.MethodGet, path: "/orgs/{org}/memberships/{username}"},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#set-organization-membership-for-a-user
	"SetOrganizationMembershipForAUser": {method: http.MethodPut, path: "/orgs/{org}/memberships/{username}", body: []string{"role"}},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#remove-organization-membership-for-a-user
	"RemoveOrganizationMembershipForAUser": {method: http.MethodDelete, path: "/orgs/{org}/memberships/{username}"},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#list-public-organization-members
	"ListPublicOrganizationMembers": {method: http.MethodGet, path: "/orgs/{org}/public_members", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#check-public-organization-membership-for-a-user
	"CheckPublicOrganizationMembershipForAUser": {method: http.MethodGet, path: "/orgs/{org}/public_members/{username}"},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#set-public-organization-membership-for-the-authenticated-user
	"SetPublicOrganizationMembershipForTheAuthenticatedUser": {method: http.MethodPut, path: "/orgs/{org}/public_members/{username}"},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#remove-public-organization-membership-for-the-authenticated-user
	"RemovePublicOrganizationMembershipForTheAuthenticatedUser": {method: http.MethodDelete, path: "/orgs/{org}/public_members/{username}"},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#list-organization-memberships-for-the-authenticated-user
	"ListOrganizationMembershipsForTheAuthenticatedUser": {method: http.MethodGet, path: "/user/memberships/orgs", query: []string{"state", "per_page", "page"}},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#get-an-organization-membership-for-the-authenticated-user
	"GetAnOrganizationMembershipForTheAuthenticatedUser": {method: http.MethodGet, path: "/user/memberships/orgs/{org}"},
	// https://docs.github.com/en/rest/orgs/members?apiVersion=2026-03-10#update-an-organization-membership-for-the-authenticated-user
	"UpdateAnOrganizationMembershipForTheAuthenticatedUser": {method: http.MethodPatch, path: "/user/memberships/orgs/{org}", body: []string{"state"}},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#list-organizations
	"ListOrganizations": {method: http.MethodGet, path: "/organizations", query: []string{"since", "per_page"}},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#get-an-organization
	"GetAnOrganization": {method: http.MethodGet, path: "/orgs/{org}"},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#update-an-organization
	"UpdateAnOrganization": {method: http.MethodPatch, path: "/orgs/{org}", body: []string{"billing_email", "company", "email", "twitter_username", "location", "name", "description", "has_organization_projects", "has_repository_projects", "default_repository_permission", "members_can_create_repositories", "members_can_create_internal_repositories", "members_can_create_private_repositories", "members_can_create_public_repositories", "members_allowed_repository_creation_type", "members_can_create_pages", "members_can_create_public_pages", "members_can_create_private_pages", "members_can_fork_private_repositories", "web_commit_signoff_required", "blog", "advanced_security_enabled_for_new_repositories", "dependabot_alerts_enabled_for_new_repositories", "dependabot_security_updates_enabled_for_new_repositories", "dependency_graph_enabled_for_new_repositories", "secret_scanning_enabled_for_new_repositories", "secret_scanning_push_protection_enabled_for_new_repositories", "secret_scanning_push_protection_custom_link", "deploy_keys_enabled_for_repositories"}},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#delete-an-organization
	"DeleteAnOrganization": {method: http.MethodDelete, path: "/orgs/{org}"},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#list-app-installations-for-an-organization
	"ListAppInstallationsForAnOrganization": {method: http.MethodGet, path: "/orgs/{org}/installations", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#get-immutable-releases-settings-for-an-organization
	"GetImmutableReleasesSettingsForAnOrganization": {method: http.MethodGet, path: "/orgs/{org}/settings/immutable-releases"},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#set-immutable-releases-settings-for-an-organization
	"SetImmutableReleasesSettingsForAnOrganization": {method: http.MethodPut, path: "/orgs/{org}/settings/immutable-releases", body: []string{"enforced_repositories", "selected_repository_ids"}},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#list-selected-repositories-for-immutable-releases-enforcement
	"ListSelectedRepositoriesForImmutableReleasesEnforcement": {method: http.MethodGet, path: "/orgs/{org}/settings/immutable-releases/repositories", query: []string{"page", "per_page"}},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#set-selected-repositories-for-immutable-releases-enforcement
	"SetSelectedRepositoriesForImmutableReleasesEnforcement": {method: http.MethodPut, path: "/orgs/{org}/settings/immutable-releases/repositories", body: []string{"selected_repository_ids"}},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#enable-a-selected-repository-for-immutable-releases-in-an-organization
	"EnableASelectedRepositoryForImmutableReleasesInAnOrganization": {method: http.MethodPut, path: "/orgs/{org}/settings/immutable-releases/repositories/{repository_id}"},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#disable-a-selected-repository-for-immutable-releases-in-an-organization
	"DisableASelectedRepositoryForImmutableReleasesInAnOrganization": {method: http.MethodDelete, path: "/orgs/{org}/settings/immutable-releases/repositories/{repository_id}"},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#enable-or-disable-a-security-feature-for-an-organization
	"EnableOrDisableASecurityFeatureForAnOrganization": {method: http.MethodPost, path: "/orgs/{org}/{security_product}/{enablement}", body: []string{"query_suite"}},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#list-organizations-for-the-authenticated-user
	"ListOrganizationsForTheAuthenticatedUser": {method: http.MethodGet, path: "/user/orgs", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/orgs/orgs?apiVersion=2026-03-10#list-organizations-for-a-user
	"ListOrganizationsForAUser": {method: http.MethodGet, path: "/users/{username}/orgs", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10#list-issues-assigned-to-the-authenticated-user
	"ListIssuesAssignedToTheAuthenticatedUser": {method: http.MethodGet, path: "/issues", query: []string{"filter", "state", "labels", "sort", "direction", "since", "collab", "orgs", "owned", "pulls", "per_page", "page"}},
	// https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10#list-organization-issues-assigned-to-the-authenticated-user
	"ListOrganizationIssuesAssignedToTheAuthenticatedUser": {method: http.MethodGet, path: "/orgs/{org}/issues", query: []string{"filter", "state", "labels", "type", "sort", "direction", "since", "per_page", "page"}},
	// https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10#list-repository-issues
	"ListRepositoryIssues": {method: http.MethodGet, path: "/repos/{owner}/{repo}/issues", query: []string{"milestone", "state", "assignee", "type", "creator", "mentioned", "issue_field_values", "labels", "sort", "direction", "since", "per_page", "page"}},
	// https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10#create-an-issue
	"CreateAnIssue": {method: http.MethodPost, path: "/repos/{owner}/{repo}/issues", body: []string{"title", "body", "milestone", "labels", "assignees", "issue_field_values", "type", "parent_issue_id"}},
	// https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10#get-an-issue
	"GetAnIssue": {method: http.MethodGet, path: "/repos/{owner}/{repo}/issues/{issue_number}"},
	// https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10#update-an-issue
	"UpdateAnIssue": {method: http.MethodPatch, path: "/repos/{owner}/{repo}/issues/{issue_number}", body: []string{"title", "body", "state", "state_reason", "duplicate_issue_id", "milestone", "labels", "assignees", "issue_field_values", "type"}},
	// https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10#lock-an-issue
	"LockAnIssue": {method: http.MethodPut, path: "/repos/{owner}/{repo}/issues/{issue_number}/lock", body: []string{"lock_reason"}},
	// https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10#unlock-an-issue
	"UnlockAnIssue": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/issues/{issue_number}/lock"},
	// https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10#list-issue-suggestions
	"ListIssueSuggestions": {method: http.MethodGet, path: "/repos/{owner}/{repo}/issues/{issue_number}/suggestions", query: []string{"state", "action", "per_page", "page"}},
	// https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10#approve-an-issue-suggestion
	"ApproveAnIssueSuggestion": {method: http.MethodPost, path: "/repos/{owner}/{repo}/issues/{issue_number}/suggestions/{suggestion_id}/approve"},
	// https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10#dismiss-an-issue-suggestion
	"DismissAnIssueSuggestion": {method: http.MethodPost, path: "/repos/{owner}/{repo}/issues/{issue_number}/suggestions/{suggestion_id}/dismiss"},
	// https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10#list-user-account-issues-assigned-to-the-authenticated-user
	"ListUserAccountIssuesAssignedToTheAuthenticatedUser": {method: http.MethodGet, path: "/user/issues", query: []string{"filter", "state", "labels", "sort", "direction", "since", "per_page", "page"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#list-organization-repositories
	"ListOrganizationRepositories": {method: http.MethodGet, path: "/orgs/{org}/repos", query: []string{"type", "sort", "direction", "per_page", "page"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#create-an-organization-repository
	"CreateAnOrganizationRepository": {method: http.MethodPost, path: "/orgs/{org}/repos", body: []string{"name", "description", "homepage", "private", "visibility", "has_issues", "has_projects", "has_wiki", "has_downloads", "is_template", "team_id", "auto_init", "gitignore_template", "license_template", "allow_squash_merge", "allow_merge_commit", "allow_rebase_merge", "allow_auto_merge", "delete_branch_on_merge", "use_squash_pr_title_as_default", "squash_merge_commit_title", "squash_merge_commit_message", "merge_commit_title", "merge_commit_message", "custom_properties"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#get-a-repository
	"GetARepository": {method: http.MethodGet, path: "/repos/{owner}/{repo}"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#update-a-repository
	"UpdateARepository": {method: http.MethodPatch, path: "/repos/{owner}/{repo}", body: []string{"name", "description", "homepage", "private", "visibility", "security_and_analysis", "has_issues", "has_projects", "has_wiki", "has_pull_requests", "pull_request_creation_policy", "is_template", "default_branch", "allow_squash_merge", "allow_merge_commit", "allow_rebase_merge", "allow_auto_merge", "delete_branch_on_merge", "allow_update_branch", "use_squash_pr_title_as_default", "squash_merge_commit_title", "squash_merge_commit_message", "merge_commit_title", "merge_commit_message", "archived", "allow_forking", "web_commit_signoff_required"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#delete-a-repository
	"DeleteARepository": {method: http.MethodDelete, path: "/repos/{owner}/{repo}"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#list-repository-activities
	"ListRepositoryActivities": {method: http.MethodGet, path: "/repos/{owner}/{repo}/activity", query: []string{"direction", "per_page", "before", "after", "ref", "actor", "time_period", "activity_type"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#check-if-dependabot-security-updates-are-enabled-for-a-repository
	"CheckIfDependabotSecurityUpdatesAreEnabledForARepository": {method: http.MethodGet, path: "/repos/{owner}/{repo}/automated-security-fixes"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#enable-dependabot-security-updates
	"EnableDependabotSecurityUpdates": {method: http.MethodPut, path: "/repos/{owner}/{repo}/automated-security-fixes"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#disable-dependabot-security-updates
	"DisableDependabotSecurityUpdates": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/automated-security-fixes"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#list-codeowners-errors
	"ListCODEOWNERSErrors": {method: http.MethodGet, path: "/repos/{owner}/{repo}/codeowners/errors", query: []string{"ref"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#list-repository-contributors
	"ListRepositoryContributors": {method: http.MethodGet, path: "/repos/{owner}/{repo}/contributors", query: []string{"anon", "per_page", "page"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#create-a-repository-dispatch-event
	"CreateARepositoryDispatchEvent": {method: http.MethodPost, path: "/repos/{owner}/{repo}/dispatches", body: []string{"event_type", "client_payload"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#get-the-hash-algorithm-for-a-repository
	"GetTheHashAlgorithmForARepository": {method: http.MethodGet, path: "/repos/{owner}/{repo}/hash-algorithm"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#check-if-immutable-releases-are-enabled-for-a-repository
	"CheckIfImmutableReleasesAreEnabledForARepository": {method: http.MethodGet, path: "/repos/{owner}/{repo}/immutable-releases"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#enable-immutable-releases
	"EnableImmutableReleases": {method: http.MethodPut, path: "/repos/{owner}/{repo}/immutable-releases"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#disable-immutable-releases
	"DisableImmutableReleases": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/immutable-releases"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#list-repository-languages
	"ListRepositoryLanguages": {method: http.MethodGet, path: "/repos/{owner}/{repo}/languages"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#check-if-private-vulnerability-reporting-is-enabled-for-a-repository
	"CheckIfPrivateVulnerabilityReportingIsEnabledForARepository": {method: http.MethodGet, path: "/repos/{owner}/{repo}/private-vulnerability-reporting"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#enable-private-vulnerability-reporting-for-a-repository
	"EnablePrivateVulnerabilityReportingForARepository": {method: http.MethodPut, path: "/repos/{owner}/{repo}/private-vulnerability-reporting"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#disable-private-vulnerability-reporting-for-a-repository
	"DisablePrivateVulnerabilityReportingForARepository": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/private-vulnerability-reporting"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#list-repository-tags
	"ListRepositoryTags": {method: http.MethodGet, path: "/repos/{owner}/{repo}/tags", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#list-repository-teams
	"ListRepositoryTeams": {method: http.MethodGet, path: "/repos/{owner}/{repo}/teams", query: []string{"per_page", "page"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#get-all-repository-topics
	"GetAllRepositoryTopics": {method: http.MethodGet, path: "/repos/{owner}/{repo}/topics", query: []string{"page", "per_page"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#replace-all-repository-topics
	"ReplaceAllRepositoryTopics": {method: http.MethodPut, path: "/repos/{owner}/{repo}/topics", body: []string{"names"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#transfer-a-repository
	"TransferARepository": {method: http.MethodPost, path: "/repos/{owner}/{repo}/transfer", body: []string{"new_owner", "new_name", "team_ids"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#check-if-vulnerability-alerts-are-enabled-for-a-repository
	"CheckIfVulnerabilityAlertsAreEnabledForARepository": {method: http.MethodGet, path: "/repos/{owner}/{repo}/vulnerability-alerts"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#enable-vulnerability-alerts
	"EnableVulnerabilityAlerts": {method: http.MethodPut, path: "/repos/{owner}/{repo}/vulnerability-alerts"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#disable-vulnerability-alerts
	"DisableVulnerabilityAlerts": {method: http.MethodDelete, path: "/repos/{owner}/{repo}/vulnerability-alerts"},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#create-a-repository-using-a-template
	"CreateARepositoryUsingATemplate": {method: http.MethodPost, path: "/repos/{template_owner}/{template_repo}/generate", body: []string{"owner", "name", "description", "include_all_branches", "private"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#list-public-repositories
	"ListPublicRepositories": {method: http.MethodGet, path: "/repositories", query: []string{"since"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#list-repositories-for-the-authenticated-user
	"ListRepositoriesForTheAuthenticatedUser": {method: http.MethodGet, path: "/user/repos", query: []string{"visibility", "affiliation", "type", "sort", "direction", "per_page", "page", "since", "before"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#create-a-repository-for-the-authenticated-user
	"CreateARepositoryForTheAuthenticatedUser": {method: http.MethodPost, path: "/user/repos", body: []string{"name", "description", "homepage", "private", "has_issues", "has_projects", "has_wiki", "has_discussions", "team_id", "auto_init", "gitignore_template", "license_template", "allow_squash_merge", "allow_merge_commit", "allow_rebase_merge", "allow_auto_merge", "delete_branch_on_merge", "squash_merge_commit_title", "squash_merge_commit_message", "merge_commit_title", "merge_commit_message", "has_downloads", "is_template"}},
	// https://docs.github.com/en/rest/repos/repos?apiVersion=2026-03-10#list-repositories-for-a-user
	"ListRepositoriesForAUser": {method: http.MethodGet, path: "/users/{username}/repos", query: []string{"type", "sort", "direction", "per_page", "page"}},
}

func (s *GitHubServer) GetTheCombinedStatusForASpecificReference(ctx context.Context, req *protocol.GitHubGetTheCombinedStatusForASpecificReferenceRequest) (*protocol.GitHubGetTheCombinedStatusForASpecificReferenceResponse, error) {
	return githubAnswer(s, ctx, "GetTheCombinedStatusForASpecificReference", req, &protocol.GitHubGetTheCombinedStatusForASpecificReferenceResponse{})
}

func (s *GitHubServer) ListCommitStatusesForAReference(ctx context.Context, req *protocol.GitHubListCommitStatusesForAReferenceRequest) (*protocol.GitHubListCommitStatusesForAReferenceResponse, error) {
	return githubAnswer(s, ctx, "ListCommitStatusesForAReference", req, &protocol.GitHubListCommitStatusesForAReferenceResponse{})
}

func (s *GitHubServer) CreateACommitStatus(ctx context.Context, req *protocol.GitHubCreateACommitStatusRequest) (*protocol.GitHubCreateACommitStatusResponse, error) {
	return githubAnswer(s, ctx, "CreateACommitStatus", req, &protocol.GitHubCreateACommitStatusResponse{})
}

func (s *GitHubServer) ListCommitCommentsForARepository(ctx context.Context, req *protocol.GitHubListCommitCommentsForARepositoryRequest) (*protocol.GitHubListCommitCommentsForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "ListCommitCommentsForARepository", req, &protocol.GitHubListCommitCommentsForARepositoryResponse{})
}

func (s *GitHubServer) GetACommitComment(ctx context.Context, req *protocol.GitHubGetACommitCommentRequest) (*protocol.GitHubGetACommitCommentResponse, error) {
	return githubAnswer(s, ctx, "GetACommitComment", req, &protocol.GitHubGetACommitCommentResponse{})
}

func (s *GitHubServer) UpdateACommitComment(ctx context.Context, req *protocol.GitHubUpdateACommitCommentRequest) (*protocol.GitHubGetACommitCommentResponse, error) {
	return githubAnswer(s, ctx, "UpdateACommitComment", req, &protocol.GitHubGetACommitCommentResponse{})
}

func (s *GitHubServer) DeleteACommitComment(ctx context.Context, req *protocol.GitHubDeleteACommitCommentRequest) (*protocol.GitHubDeleteACommitCommentResponse, error) {
	return githubAnswer(s, ctx, "DeleteACommitComment", req, &protocol.GitHubDeleteACommitCommentResponse{})
}

func (s *GitHubServer) ListCommitComments(ctx context.Context, req *protocol.GitHubListCommitCommentsRequest) (*protocol.GitHubListCommitCommentsForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "ListCommitComments", req, &protocol.GitHubListCommitCommentsForARepositoryResponse{})
}

func (s *GitHubServer) CreateACommitComment(ctx context.Context, req *protocol.GitHubCreateACommitCommentRequest) (*protocol.GitHubGetACommitCommentResponse, error) {
	return githubAnswer(s, ctx, "CreateACommitComment", req, &protocol.GitHubGetACommitCommentResponse{})
}

func (s *GitHubServer) ListCommits(ctx context.Context, req *protocol.GitHubListCommitsRequest) (*protocol.GitHubListCommitsResponse, error) {
	return githubAnswer(s, ctx, "ListCommits", req, &protocol.GitHubListCommitsResponse{})
}

func (s *GitHubServer) ListBranchesForHEADCommit(ctx context.Context, req *protocol.GitHubListBranchesForHEADCommitRequest) (*protocol.GitHubListBranchesForHEADCommitResponse, error) {
	return githubAnswer(s, ctx, "ListBranchesForHEADCommit", req, &protocol.GitHubListBranchesForHEADCommitResponse{})
}

func (s *GitHubServer) ListPullRequestsAssociatedWithACommit(ctx context.Context, req *protocol.GitHubListPullRequestsAssociatedWithACommitRequest) (*protocol.GitHubListPullRequestsAssociatedWithACommitResponse, error) {
	return githubAnswer(s, ctx, "ListPullRequestsAssociatedWithACommit", req, &protocol.GitHubListPullRequestsAssociatedWithACommitResponse{})
}

func (s *GitHubServer) GetACommit(ctx context.Context, req *protocol.GitHubGetACommitRequest) (*protocol.GitHubGetACommitResponse, error) {
	return githubAnswer(s, ctx, "GetACommit", req, &protocol.GitHubGetACommitResponse{})
}

func (s *GitHubServer) CompareTwoCommits(ctx context.Context, req *protocol.GitHubCompareTwoCommitsRequest) (*protocol.GitHubCompareTwoCommitsResponse, error) {
	return githubAnswer(s, ctx, "CompareTwoCommits", req, &protocol.GitHubCompareTwoCommitsResponse{})
}

func (s *GitHubServer) ListBranches(ctx context.Context, req *protocol.GitHubListBranchesRequest) (*protocol.GitHubListBranchesResponse, error) {
	return githubAnswer(s, ctx, "ListBranches", req, &protocol.GitHubListBranchesResponse{})
}

func (s *GitHubServer) GetABranch(ctx context.Context, req *protocol.GitHubGetABranchRequest) (*protocol.GitHubGetABranchResponse, error) {
	return githubAnswer(s, ctx, "GetABranch", req, &protocol.GitHubGetABranchResponse{})
}

func (s *GitHubServer) RenameABranch(ctx context.Context, req *protocol.GitHubRenameABranchRequest) (*protocol.GitHubGetABranchResponse, error) {
	return githubAnswer(s, ctx, "RenameABranch", req, &protocol.GitHubGetABranchResponse{})
}

func (s *GitHubServer) SyncAForkBranchWithTheUpstreamRepository(ctx context.Context, req *protocol.GitHubSyncAForkBranchWithTheUpstreamRepositoryRequest) (*protocol.GitHubSyncAForkBranchWithTheUpstreamRepositoryResponse, error) {
	return githubAnswer(s, ctx, "SyncAForkBranchWithTheUpstreamRepository", req, &protocol.GitHubSyncAForkBranchWithTheUpstreamRepositoryResponse{})
}

func (s *GitHubServer) MergeABranch(ctx context.Context, req *protocol.GitHubMergeABranchRequest) (*protocol.GitHubMergeABranchResponse, error) {
	return githubAnswer(s, ctx, "MergeABranch", req, &protocol.GitHubMergeABranchResponse{})
}

func (s *GitHubServer) GetAllRequestedReviewersForAPullRequest(ctx context.Context, req *protocol.GitHubGetAllRequestedReviewersForAPullRequestRequest) (*protocol.GitHubGetAllRequestedReviewersForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "GetAllRequestedReviewersForAPullRequest", req, &protocol.GitHubGetAllRequestedReviewersForAPullRequestResponse{})
}

func (s *GitHubServer) RequestReviewersForAPullRequest(ctx context.Context, req *protocol.GitHubRequestReviewersForAPullRequestRequest) (*protocol.GitHubRequestReviewersForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "RequestReviewersForAPullRequest", req, &protocol.GitHubRequestReviewersForAPullRequestResponse{})
}

func (s *GitHubServer) RemoveRequestedReviewersFromAPullRequest(ctx context.Context, req *protocol.GitHubRemoveRequestedReviewersFromAPullRequestRequest) (*protocol.GitHubRequestReviewersForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "RemoveRequestedReviewersFromAPullRequest", req, &protocol.GitHubRequestReviewersForAPullRequestResponse{})
}

func (s *GitHubServer) ListReviewsForAPullRequest(ctx context.Context, req *protocol.GitHubListReviewsForAPullRequestRequest) (*protocol.GitHubListReviewsForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "ListReviewsForAPullRequest", req, &protocol.GitHubListReviewsForAPullRequestResponse{})
}

func (s *GitHubServer) CreateAReviewForAPullRequest(ctx context.Context, req *protocol.GitHubCreateAReviewForAPullRequestRequest) (*protocol.GitHubCreateAReviewForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "CreateAReviewForAPullRequest", req, &protocol.GitHubCreateAReviewForAPullRequestResponse{})
}

func (s *GitHubServer) GetAReviewForAPullRequest(ctx context.Context, req *protocol.GitHubGetAReviewForAPullRequestRequest) (*protocol.GitHubCreateAReviewForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "GetAReviewForAPullRequest", req, &protocol.GitHubCreateAReviewForAPullRequestResponse{})
}

func (s *GitHubServer) UpdateAReviewForAPullRequest(ctx context.Context, req *protocol.GitHubUpdateAReviewForAPullRequestRequest) (*protocol.GitHubCreateAReviewForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "UpdateAReviewForAPullRequest", req, &protocol.GitHubCreateAReviewForAPullRequestResponse{})
}

func (s *GitHubServer) DeleteAPendingReviewForAPullRequest(ctx context.Context, req *protocol.GitHubDeleteAPendingReviewForAPullRequestRequest) (*protocol.GitHubCreateAReviewForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "DeleteAPendingReviewForAPullRequest", req, &protocol.GitHubCreateAReviewForAPullRequestResponse{})
}

func (s *GitHubServer) ListCommentsForAPullRequestReview(ctx context.Context, req *protocol.GitHubListCommentsForAPullRequestReviewRequest) (*protocol.GitHubListCommentsForAPullRequestReviewResponse, error) {
	return githubAnswer(s, ctx, "ListCommentsForAPullRequestReview", req, &protocol.GitHubListCommentsForAPullRequestReviewResponse{})
}

func (s *GitHubServer) DismissAReviewForAPullRequest(ctx context.Context, req *protocol.GitHubDismissAReviewForAPullRequestRequest) (*protocol.GitHubCreateAReviewForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "DismissAReviewForAPullRequest", req, &protocol.GitHubCreateAReviewForAPullRequestResponse{})
}

func (s *GitHubServer) SubmitAReviewForAPullRequest(ctx context.Context, req *protocol.GitHubSubmitAReviewForAPullRequestRequest) (*protocol.GitHubCreateAReviewForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "SubmitAReviewForAPullRequest", req, &protocol.GitHubCreateAReviewForAPullRequestResponse{})
}

func (s *GitHubServer) ListReviewCommentsInARepository(ctx context.Context, req *protocol.GitHubListReviewCommentsInARepositoryRequest) (*protocol.GitHubListReviewCommentsInARepositoryResponse, error) {
	return githubAnswer(s, ctx, "ListReviewCommentsInARepository", req, &protocol.GitHubListReviewCommentsInARepositoryResponse{})
}

func (s *GitHubServer) GetAReviewCommentForAPullRequest(ctx context.Context, req *protocol.GitHubGetAReviewCommentForAPullRequestRequest) (*protocol.GitHubGetAReviewCommentForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "GetAReviewCommentForAPullRequest", req, &protocol.GitHubGetAReviewCommentForAPullRequestResponse{})
}

func (s *GitHubServer) UpdateAReviewCommentForAPullRequest(ctx context.Context, req *protocol.GitHubUpdateAReviewCommentForAPullRequestRequest) (*protocol.GitHubGetAReviewCommentForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "UpdateAReviewCommentForAPullRequest", req, &protocol.GitHubGetAReviewCommentForAPullRequestResponse{})
}

func (s *GitHubServer) DeleteAReviewCommentForAPullRequest(ctx context.Context, req *protocol.GitHubDeleteAReviewCommentForAPullRequestRequest) (*protocol.GitHubDeleteAReviewCommentForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "DeleteAReviewCommentForAPullRequest", req, &protocol.GitHubDeleteAReviewCommentForAPullRequestResponse{})
}

func (s *GitHubServer) ListReviewCommentsOnAPullRequest(ctx context.Context, req *protocol.GitHubListReviewCommentsOnAPullRequestRequest) (*protocol.GitHubListReviewCommentsInARepositoryResponse, error) {
	return githubAnswer(s, ctx, "ListReviewCommentsOnAPullRequest", req, &protocol.GitHubListReviewCommentsInARepositoryResponse{})
}

func (s *GitHubServer) CreateAReviewCommentForAPullRequest(ctx context.Context, req *protocol.GitHubCreateAReviewCommentForAPullRequestRequest) (*protocol.GitHubGetAReviewCommentForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "CreateAReviewCommentForAPullRequest", req, &protocol.GitHubGetAReviewCommentForAPullRequestResponse{})
}

func (s *GitHubServer) CreateAReplyForAReviewComment(ctx context.Context, req *protocol.GitHubCreateAReplyForAReviewCommentRequest) (*protocol.GitHubGetAReviewCommentForAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "CreateAReplyForAReviewComment", req, &protocol.GitHubGetAReviewCommentForAPullRequestResponse{})
}

func (s *GitHubServer) ListPullRequests(ctx context.Context, req *protocol.GitHubListPullRequestsRequest) (*protocol.GitHubListPullRequestsResponse, error) {
	return githubAnswer(s, ctx, "ListPullRequests", req, &protocol.GitHubListPullRequestsResponse{})
}

func (s *GitHubServer) CreateAPullRequest(ctx context.Context, req *protocol.GitHubCreateAPullRequestRequest) (*protocol.GitHubCreateAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "CreateAPullRequest", req, &protocol.GitHubCreateAPullRequestResponse{})
}

func (s *GitHubServer) GetAPullRequest(ctx context.Context, req *protocol.GitHubGetAPullRequestRequest) (*protocol.GitHubCreateAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "GetAPullRequest", req, &protocol.GitHubCreateAPullRequestResponse{})
}

func (s *GitHubServer) UpdateAPullRequest(ctx context.Context, req *protocol.GitHubUpdateAPullRequestRequest) (*protocol.GitHubCreateAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "UpdateAPullRequest", req, &protocol.GitHubCreateAPullRequestResponse{})
}

func (s *GitHubServer) ListCommitsOnAPullRequest(ctx context.Context, req *protocol.GitHubListCommitsOnAPullRequestRequest) (*protocol.GitHubListCommitsOnAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "ListCommitsOnAPullRequest", req, &protocol.GitHubListCommitsOnAPullRequestResponse{})
}

func (s *GitHubServer) ListPullRequestsFiles(ctx context.Context, req *protocol.GitHubListPullRequestsFilesRequest) (*protocol.GitHubListPullRequestsFilesResponse, error) {
	return githubAnswer(s, ctx, "ListPullRequestsFiles", req, &protocol.GitHubListPullRequestsFilesResponse{})
}

func (s *GitHubServer) CheckIfAPullRequestHasBeenMerged(ctx context.Context, req *protocol.GitHubCheckIfAPullRequestHasBeenMergedRequest) (*protocol.GitHubCheckIfAPullRequestHasBeenMergedResponse, error) {
	return githubAnswer(s, ctx, "CheckIfAPullRequestHasBeenMerged", req, &protocol.GitHubCheckIfAPullRequestHasBeenMergedResponse{})
}

func (s *GitHubServer) MergeAPullRequest(ctx context.Context, req *protocol.GitHubMergeAPullRequestRequest) (*protocol.GitHubMergeAPullRequestResponse, error) {
	return githubAnswer(s, ctx, "MergeAPullRequest", req, &protocol.GitHubMergeAPullRequestResponse{})
}

func (s *GitHubServer) MergeAPullRequestAsynchronously(ctx context.Context, req *protocol.GitHubMergeAPullRequestAsynchronouslyRequest) (*protocol.GitHubMergeAPullRequestAsynchronouslyResponse, error) {
	return githubAnswer(s, ctx, "MergeAPullRequestAsynchronously", req, &protocol.GitHubMergeAPullRequestAsynchronouslyResponse{})
}

func (s *GitHubServer) GetTheResultOfAnAsynchronousMerge(ctx context.Context, req *protocol.GitHubGetTheResultOfAnAsynchronousMergeRequest) (*protocol.GitHubMergeAPullRequestAsynchronouslyResponse, error) {
	return githubAnswer(s, ctx, "GetTheResultOfAnAsynchronousMerge", req, &protocol.GitHubMergeAPullRequestAsynchronouslyResponse{})
}

func (s *GitHubServer) UpdateAPullRequestBranch(ctx context.Context, req *protocol.GitHubUpdateAPullRequestBranchRequest) (*protocol.GitHubUpdateAPullRequestBranchResponse, error) {
	return githubAnswer(s, ctx, "UpdateAPullRequestBranch", req, &protocol.GitHubUpdateAPullRequestBranchResponse{})
}

func (s *GitHubServer) ListRepositoryWorkflows(ctx context.Context, req *protocol.GitHubListRepositoryWorkflowsRequest) (*protocol.GitHubListRepositoryWorkflowsResponse, error) {
	return githubAnswer(s, ctx, "ListRepositoryWorkflows", req, &protocol.GitHubListRepositoryWorkflowsResponse{})
}

func (s *GitHubServer) GetAWorkflow(ctx context.Context, req *protocol.GitHubGetAWorkflowRequest) (*protocol.GitHubGetAWorkflowResponse, error) {
	return githubAnswer(s, ctx, "GetAWorkflow", req, &protocol.GitHubGetAWorkflowResponse{})
}

func (s *GitHubServer) DisableAWorkflow(ctx context.Context, req *protocol.GitHubDisableAWorkflowRequest) (*protocol.GitHubDisableAWorkflowResponse, error) {
	return githubAnswer(s, ctx, "DisableAWorkflow", req, &protocol.GitHubDisableAWorkflowResponse{})
}

func (s *GitHubServer) CreateAWorkflowDispatchEvent(ctx context.Context, req *protocol.GitHubCreateAWorkflowDispatchEventRequest) (*protocol.GitHubCreateAWorkflowDispatchEventResponse, error) {
	return githubAnswer(s, ctx, "CreateAWorkflowDispatchEvent", req, &protocol.GitHubCreateAWorkflowDispatchEventResponse{})
}

func (s *GitHubServer) EnableAWorkflow(ctx context.Context, req *protocol.GitHubEnableAWorkflowRequest) (*protocol.GitHubEnableAWorkflowResponse, error) {
	return githubAnswer(s, ctx, "EnableAWorkflow", req, &protocol.GitHubEnableAWorkflowResponse{})
}

func (s *GitHubServer) GetWorkflowUsage(ctx context.Context, req *protocol.GitHubGetWorkflowUsageRequest) (*protocol.GitHubGetWorkflowUsageResponse, error) {
	return githubAnswer(s, ctx, "GetWorkflowUsage", req, &protocol.GitHubGetWorkflowUsageResponse{})
}

func (s *GitHubServer) GetAJobForAWorkflowRun(ctx context.Context, req *protocol.GitHubGetAJobForAWorkflowRunRequest) (*protocol.GitHubGetAJobForAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "GetAJobForAWorkflowRun", req, &protocol.GitHubGetAJobForAWorkflowRunResponse{})
}

func (s *GitHubServer) DownloadJobLogsForAWorkflowRun(ctx context.Context, req *protocol.GitHubDownloadJobLogsForAWorkflowRunRequest) (*protocol.GitHubDownloadJobLogsForAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "DownloadJobLogsForAWorkflowRun", req, &protocol.GitHubDownloadJobLogsForAWorkflowRunResponse{})
}

func (s *GitHubServer) ListJobsForAWorkflowRunAttempt(ctx context.Context, req *protocol.GitHubListJobsForAWorkflowRunAttemptRequest) (*protocol.GitHubListJobsForAWorkflowRunAttemptResponse, error) {
	return githubAnswer(s, ctx, "ListJobsForAWorkflowRunAttempt", req, &protocol.GitHubListJobsForAWorkflowRunAttemptResponse{})
}

func (s *GitHubServer) ListJobsForAWorkflowRun(ctx context.Context, req *protocol.GitHubListJobsForAWorkflowRunRequest) (*protocol.GitHubListJobsForAWorkflowRunAttemptResponse, error) {
	return githubAnswer(s, ctx, "ListJobsForAWorkflowRun", req, &protocol.GitHubListJobsForAWorkflowRunAttemptResponse{})
}

func (s *GitHubServer) ListArtifactsForARepository(ctx context.Context, req *protocol.GitHubListArtifactsForARepositoryRequest) (*protocol.GitHubListArtifactsForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "ListArtifactsForARepository", req, &protocol.GitHubListArtifactsForARepositoryResponse{})
}

func (s *GitHubServer) GetAnArtifact(ctx context.Context, req *protocol.GitHubGetAnArtifactRequest) (*protocol.GitHubGetAnArtifactResponse, error) {
	return githubAnswer(s, ctx, "GetAnArtifact", req, &protocol.GitHubGetAnArtifactResponse{})
}

func (s *GitHubServer) DeleteAnArtifact(ctx context.Context, req *protocol.GitHubDeleteAnArtifactRequest) (*protocol.GitHubDeleteAnArtifactResponse, error) {
	return githubAnswer(s, ctx, "DeleteAnArtifact", req, &protocol.GitHubDeleteAnArtifactResponse{})
}

func (s *GitHubServer) DownloadAnArtifact(ctx context.Context, req *protocol.GitHubDownloadAnArtifactRequest) (*protocol.GitHubDownloadAnArtifactResponse, error) {
	return githubAnswer(s, ctx, "DownloadAnArtifact", req, &protocol.GitHubDownloadAnArtifactResponse{})
}

func (s *GitHubServer) ListWorkflowRunArtifacts(ctx context.Context, req *protocol.GitHubListWorkflowRunArtifactsRequest) (*protocol.GitHubListArtifactsForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "ListWorkflowRunArtifacts", req, &protocol.GitHubListArtifactsForARepositoryResponse{})
}

func (s *GitHubServer) ReRunAJobFromAWorkflowRun(ctx context.Context, req *protocol.GitHubReRunAJobFromAWorkflowRunRequest) (*protocol.GitHubReRunAJobFromAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "ReRunAJobFromAWorkflowRun", req, &protocol.GitHubReRunAJobFromAWorkflowRunResponse{})
}

func (s *GitHubServer) ListWorkflowRunsForARepository(ctx context.Context, req *protocol.GitHubListWorkflowRunsForARepositoryRequest) (*protocol.GitHubListWorkflowRunsForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "ListWorkflowRunsForARepository", req, &protocol.GitHubListWorkflowRunsForARepositoryResponse{})
}

func (s *GitHubServer) GetAWorkflowRun(ctx context.Context, req *protocol.GitHubGetAWorkflowRunRequest) (*protocol.GitHubGetAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "GetAWorkflowRun", req, &protocol.GitHubGetAWorkflowRunResponse{})
}

func (s *GitHubServer) DeleteAWorkflowRun(ctx context.Context, req *protocol.GitHubDeleteAWorkflowRunRequest) (*protocol.GitHubDeleteAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "DeleteAWorkflowRun", req, &protocol.GitHubDeleteAWorkflowRunResponse{})
}

func (s *GitHubServer) GetTheReviewHistoryForAWorkflowRun(ctx context.Context, req *protocol.GitHubGetTheReviewHistoryForAWorkflowRunRequest) (*protocol.GitHubGetTheReviewHistoryForAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "GetTheReviewHistoryForAWorkflowRun", req, &protocol.GitHubGetTheReviewHistoryForAWorkflowRunResponse{})
}

func (s *GitHubServer) ApproveAWorkflowRunForAForkPullRequest(ctx context.Context, req *protocol.GitHubApproveAWorkflowRunForAForkPullRequestRequest) (*protocol.GitHubApproveAWorkflowRunForAForkPullRequestResponse, error) {
	return githubAnswer(s, ctx, "ApproveAWorkflowRunForAForkPullRequest", req, &protocol.GitHubApproveAWorkflowRunForAForkPullRequestResponse{})
}

func (s *GitHubServer) GetAWorkflowRunAttempt(ctx context.Context, req *protocol.GitHubGetAWorkflowRunAttemptRequest) (*protocol.GitHubGetAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "GetAWorkflowRunAttempt", req, &protocol.GitHubGetAWorkflowRunResponse{})
}

func (s *GitHubServer) DownloadWorkflowRunAttemptLogs(ctx context.Context, req *protocol.GitHubDownloadWorkflowRunAttemptLogsRequest) (*protocol.GitHubDownloadWorkflowRunAttemptLogsResponse, error) {
	return githubAnswer(s, ctx, "DownloadWorkflowRunAttemptLogs", req, &protocol.GitHubDownloadWorkflowRunAttemptLogsResponse{})
}

func (s *GitHubServer) CancelAWorkflowRun(ctx context.Context, req *protocol.GitHubCancelAWorkflowRunRequest) (*protocol.GitHubCancelAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "CancelAWorkflowRun", req, &protocol.GitHubCancelAWorkflowRunResponse{})
}

func (s *GitHubServer) ReviewCustomDeploymentProtectionRulesForAWorkflowRun(ctx context.Context, req *protocol.GitHubReviewCustomDeploymentProtectionRulesForAWorkflowRunRequest) (*protocol.GitHubReviewCustomDeploymentProtectionRulesForAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "ReviewCustomDeploymentProtectionRulesForAWorkflowRun", req, &protocol.GitHubReviewCustomDeploymentProtectionRulesForAWorkflowRunResponse{})
}

func (s *GitHubServer) ForceCancelAWorkflowRun(ctx context.Context, req *protocol.GitHubForceCancelAWorkflowRunRequest) (*protocol.GitHubForceCancelAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "ForceCancelAWorkflowRun", req, &protocol.GitHubForceCancelAWorkflowRunResponse{})
}

func (s *GitHubServer) DownloadWorkflowRunLogs(ctx context.Context, req *protocol.GitHubDownloadWorkflowRunLogsRequest) (*protocol.GitHubDownloadWorkflowRunLogsResponse, error) {
	return githubAnswer(s, ctx, "DownloadWorkflowRunLogs", req, &protocol.GitHubDownloadWorkflowRunLogsResponse{})
}

func (s *GitHubServer) DeleteWorkflowRunLogs(ctx context.Context, req *protocol.GitHubDeleteWorkflowRunLogsRequest) (*protocol.GitHubDeleteWorkflowRunLogsResponse, error) {
	return githubAnswer(s, ctx, "DeleteWorkflowRunLogs", req, &protocol.GitHubDeleteWorkflowRunLogsResponse{})
}

func (s *GitHubServer) GetPendingDeploymentsForAWorkflowRun(ctx context.Context, req *protocol.GitHubGetPendingDeploymentsForAWorkflowRunRequest) (*protocol.GitHubGetPendingDeploymentsForAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "GetPendingDeploymentsForAWorkflowRun", req, &protocol.GitHubGetPendingDeploymentsForAWorkflowRunResponse{})
}

func (s *GitHubServer) ReviewPendingDeploymentsForAWorkflowRun(ctx context.Context, req *protocol.GitHubReviewPendingDeploymentsForAWorkflowRunRequest) (*protocol.GitHubReviewPendingDeploymentsForAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "ReviewPendingDeploymentsForAWorkflowRun", req, &protocol.GitHubReviewPendingDeploymentsForAWorkflowRunResponse{})
}

func (s *GitHubServer) ReRunAWorkflow(ctx context.Context, req *protocol.GitHubReRunAWorkflowRequest) (*protocol.GitHubReRunAWorkflowResponse, error) {
	return githubAnswer(s, ctx, "ReRunAWorkflow", req, &protocol.GitHubReRunAWorkflowResponse{})
}

func (s *GitHubServer) ReRunFailedJobsFromAWorkflowRun(ctx context.Context, req *protocol.GitHubReRunFailedJobsFromAWorkflowRunRequest) (*protocol.GitHubReRunFailedJobsFromAWorkflowRunResponse, error) {
	return githubAnswer(s, ctx, "ReRunFailedJobsFromAWorkflowRun", req, &protocol.GitHubReRunFailedJobsFromAWorkflowRunResponse{})
}

func (s *GitHubServer) GetWorkflowRunUsage(ctx context.Context, req *protocol.GitHubGetWorkflowRunUsageRequest) (*protocol.GitHubGetWorkflowRunUsageResponse, error) {
	return githubAnswer(s, ctx, "GetWorkflowRunUsage", req, &protocol.GitHubGetWorkflowRunUsageResponse{})
}

func (s *GitHubServer) ListWorkflowRunsForAWorkflow(ctx context.Context, req *protocol.GitHubListWorkflowRunsForAWorkflowRequest) (*protocol.GitHubListWorkflowRunsForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "ListWorkflowRunsForAWorkflow", req, &protocol.GitHubListWorkflowRunsForARepositoryResponse{})
}

func (s *GitHubServer) CreateACheckRun(ctx context.Context, req *protocol.GitHubCreateACheckRunRequest) (*protocol.GitHubCreateACheckRunResponse, error) {
	return githubAnswer(s, ctx, "CreateACheckRun", req, &protocol.GitHubCreateACheckRunResponse{})
}

func (s *GitHubServer) GetACheckRun(ctx context.Context, req *protocol.GitHubGetACheckRunRequest) (*protocol.GitHubCreateACheckRunResponse, error) {
	return githubAnswer(s, ctx, "GetACheckRun", req, &protocol.GitHubCreateACheckRunResponse{})
}

func (s *GitHubServer) UpdateACheckRun(ctx context.Context, req *protocol.GitHubUpdateACheckRunRequest) (*protocol.GitHubCreateACheckRunResponse, error) {
	return githubAnswer(s, ctx, "UpdateACheckRun", req, &protocol.GitHubCreateACheckRunResponse{})
}

func (s *GitHubServer) ListCheckRunAnnotations(ctx context.Context, req *protocol.GitHubListCheckRunAnnotationsRequest) (*protocol.GitHubListCheckRunAnnotationsResponse, error) {
	return githubAnswer(s, ctx, "ListCheckRunAnnotations", req, &protocol.GitHubListCheckRunAnnotationsResponse{})
}

func (s *GitHubServer) RerequestACheckRun(ctx context.Context, req *protocol.GitHubRerequestACheckRunRequest) (*protocol.GitHubRerequestACheckRunResponse, error) {
	return githubAnswer(s, ctx, "RerequestACheckRun", req, &protocol.GitHubRerequestACheckRunResponse{})
}

func (s *GitHubServer) ListCheckRunsInACheckSuite(ctx context.Context, req *protocol.GitHubListCheckRunsInACheckSuiteRequest) (*protocol.GitHubListCheckRunsInACheckSuiteResponse, error) {
	return githubAnswer(s, ctx, "ListCheckRunsInACheckSuite", req, &protocol.GitHubListCheckRunsInACheckSuiteResponse{})
}

func (s *GitHubServer) ListCheckRunsForAGitReference(ctx context.Context, req *protocol.GitHubListCheckRunsForAGitReferenceRequest) (*protocol.GitHubListCheckRunsInACheckSuiteResponse, error) {
	return githubAnswer(s, ctx, "ListCheckRunsForAGitReference", req, &protocol.GitHubListCheckRunsInACheckSuiteResponse{})
}

func (s *GitHubServer) CreateACheckSuite(ctx context.Context, req *protocol.GitHubCreateACheckSuiteRequest) (*protocol.GitHubCreateACheckSuiteResponse, error) {
	return githubAnswer(s, ctx, "CreateACheckSuite", req, &protocol.GitHubCreateACheckSuiteResponse{})
}

func (s *GitHubServer) UpdateRepositoryPreferencesForCheckSuites(ctx context.Context, req *protocol.GitHubUpdateRepositoryPreferencesForCheckSuitesRequest) (*protocol.GitHubUpdateRepositoryPreferencesForCheckSuitesResponse, error) {
	return githubAnswer(s, ctx, "UpdateRepositoryPreferencesForCheckSuites", req, &protocol.GitHubUpdateRepositoryPreferencesForCheckSuitesResponse{})
}

func (s *GitHubServer) GetACheckSuite(ctx context.Context, req *protocol.GitHubGetACheckSuiteRequest) (*protocol.GitHubCreateACheckSuiteResponse, error) {
	return githubAnswer(s, ctx, "GetACheckSuite", req, &protocol.GitHubCreateACheckSuiteResponse{})
}

func (s *GitHubServer) RerequestACheckSuite(ctx context.Context, req *protocol.GitHubRerequestACheckSuiteRequest) (*protocol.GitHubRerequestACheckSuiteResponse, error) {
	return githubAnswer(s, ctx, "RerequestACheckSuite", req, &protocol.GitHubRerequestACheckSuiteResponse{})
}

func (s *GitHubServer) ListCheckSuitesForAGitReference(ctx context.Context, req *protocol.GitHubListCheckSuitesForAGitReferenceRequest) (*protocol.GitHubListCheckSuitesForAGitReferenceResponse, error) {
	return githubAnswer(s, ctx, "ListCheckSuitesForAGitReference", req, &protocol.GitHubListCheckSuitesForAGitReferenceResponse{})
}

func (s *GitHubServer) ListIssueCommentsForARepository(ctx context.Context, req *protocol.GitHubListIssueCommentsForARepositoryRequest) (*protocol.GitHubListIssueCommentsForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "ListIssueCommentsForARepository", req, &protocol.GitHubListIssueCommentsForARepositoryResponse{})
}

func (s *GitHubServer) GetAnIssueComment(ctx context.Context, req *protocol.GitHubGetAnIssueCommentRequest) (*protocol.GitHubGetAnIssueCommentResponse, error) {
	return githubAnswer(s, ctx, "GetAnIssueComment", req, &protocol.GitHubGetAnIssueCommentResponse{})
}

func (s *GitHubServer) UpdateAnIssueComment(ctx context.Context, req *protocol.GitHubUpdateAnIssueCommentRequest) (*protocol.GitHubGetAnIssueCommentResponse, error) {
	return githubAnswer(s, ctx, "UpdateAnIssueComment", req, &protocol.GitHubGetAnIssueCommentResponse{})
}

func (s *GitHubServer) DeleteAnIssueComment(ctx context.Context, req *protocol.GitHubDeleteAnIssueCommentRequest) (*protocol.GitHubDeleteAnIssueCommentResponse, error) {
	return githubAnswer(s, ctx, "DeleteAnIssueComment", req, &protocol.GitHubDeleteAnIssueCommentResponse{})
}

func (s *GitHubServer) PinAnIssueComment(ctx context.Context, req *protocol.GitHubPinAnIssueCommentRequest) (*protocol.GitHubGetAnIssueCommentResponse, error) {
	return githubAnswer(s, ctx, "PinAnIssueComment", req, &protocol.GitHubGetAnIssueCommentResponse{})
}

func (s *GitHubServer) UnpinAnIssueComment(ctx context.Context, req *protocol.GitHubUnpinAnIssueCommentRequest) (*protocol.GitHubUnpinAnIssueCommentResponse, error) {
	return githubAnswer(s, ctx, "UnpinAnIssueComment", req, &protocol.GitHubUnpinAnIssueCommentResponse{})
}

func (s *GitHubServer) ListIssueComments(ctx context.Context, req *protocol.GitHubListIssueCommentsRequest) (*protocol.GitHubListIssueCommentsForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "ListIssueComments", req, &protocol.GitHubListIssueCommentsForARepositoryResponse{})
}

func (s *GitHubServer) CreateAnIssueComment(ctx context.Context, req *protocol.GitHubCreateAnIssueCommentRequest) (*protocol.GitHubGetAnIssueCommentResponse, error) {
	return githubAnswer(s, ctx, "CreateAnIssueComment", req, &protocol.GitHubGetAnIssueCommentResponse{})
}

func (s *GitHubServer) ListFailedOrganizationInvitations(ctx context.Context, req *protocol.GitHubListFailedOrganizationInvitationsRequest) (*protocol.GitHubListFailedOrganizationInvitationsResponse, error) {
	return githubAnswer(s, ctx, "ListFailedOrganizationInvitations", req, &protocol.GitHubListFailedOrganizationInvitationsResponse{})
}

func (s *GitHubServer) ListPendingOrganizationInvitations(ctx context.Context, req *protocol.GitHubListPendingOrganizationInvitationsRequest) (*protocol.GitHubListFailedOrganizationInvitationsResponse, error) {
	return githubAnswer(s, ctx, "ListPendingOrganizationInvitations", req, &protocol.GitHubListFailedOrganizationInvitationsResponse{})
}

func (s *GitHubServer) CreateAnOrganizationInvitation(ctx context.Context, req *protocol.GitHubCreateAnOrganizationInvitationRequest) (*protocol.GitHubCreateAnOrganizationInvitationResponse, error) {
	return githubAnswer(s, ctx, "CreateAnOrganizationInvitation", req, &protocol.GitHubCreateAnOrganizationInvitationResponse{})
}

func (s *GitHubServer) CancelAnOrganizationInvitation(ctx context.Context, req *protocol.GitHubCancelAnOrganizationInvitationRequest) (*protocol.GitHubCancelAnOrganizationInvitationResponse, error) {
	return githubAnswer(s, ctx, "CancelAnOrganizationInvitation", req, &protocol.GitHubCancelAnOrganizationInvitationResponse{})
}

func (s *GitHubServer) ListOrganizationInvitationTeams(ctx context.Context, req *protocol.GitHubListOrganizationInvitationTeamsRequest) (*protocol.GitHubListOrganizationInvitationTeamsResponse, error) {
	return githubAnswer(s, ctx, "ListOrganizationInvitationTeams", req, &protocol.GitHubListOrganizationInvitationTeamsResponse{})
}

func (s *GitHubServer) ListOrganizationMembers(ctx context.Context, req *protocol.GitHubListOrganizationMembersRequest) (*protocol.GitHubListOrganizationMembersResponse, error) {
	return githubAnswer(s, ctx, "ListOrganizationMembers", req, &protocol.GitHubListOrganizationMembersResponse{})
}

func (s *GitHubServer) CheckOrganizationMembershipForAUser(ctx context.Context, req *protocol.GitHubCheckOrganizationMembershipForAUserRequest) (*protocol.GitHubCheckOrganizationMembershipForAUserResponse, error) {
	return githubAnswer(s, ctx, "CheckOrganizationMembershipForAUser", req, &protocol.GitHubCheckOrganizationMembershipForAUserResponse{})
}

func (s *GitHubServer) RemoveAnOrganizationMember(ctx context.Context, req *protocol.GitHubRemoveAnOrganizationMemberRequest) (*protocol.GitHubRemoveAnOrganizationMemberResponse, error) {
	return githubAnswer(s, ctx, "RemoveAnOrganizationMember", req, &protocol.GitHubRemoveAnOrganizationMemberResponse{})
}

func (s *GitHubServer) GetOrganizationMembershipForAUser(ctx context.Context, req *protocol.GitHubGetOrganizationMembershipForAUserRequest) (*protocol.GitHubGetOrganizationMembershipForAUserResponse, error) {
	return githubAnswer(s, ctx, "GetOrganizationMembershipForAUser", req, &protocol.GitHubGetOrganizationMembershipForAUserResponse{})
}

func (s *GitHubServer) SetOrganizationMembershipForAUser(ctx context.Context, req *protocol.GitHubSetOrganizationMembershipForAUserRequest) (*protocol.GitHubGetOrganizationMembershipForAUserResponse, error) {
	return githubAnswer(s, ctx, "SetOrganizationMembershipForAUser", req, &protocol.GitHubGetOrganizationMembershipForAUserResponse{})
}

func (s *GitHubServer) RemoveOrganizationMembershipForAUser(ctx context.Context, req *protocol.GitHubRemoveOrganizationMembershipForAUserRequest) (*protocol.GitHubRemoveOrganizationMembershipForAUserResponse, error) {
	return githubAnswer(s, ctx, "RemoveOrganizationMembershipForAUser", req, &protocol.GitHubRemoveOrganizationMembershipForAUserResponse{})
}

func (s *GitHubServer) ListPublicOrganizationMembers(ctx context.Context, req *protocol.GitHubListPublicOrganizationMembersRequest) (*protocol.GitHubListOrganizationMembersResponse, error) {
	return githubAnswer(s, ctx, "ListPublicOrganizationMembers", req, &protocol.GitHubListOrganizationMembersResponse{})
}

func (s *GitHubServer) CheckPublicOrganizationMembershipForAUser(ctx context.Context, req *protocol.GitHubCheckPublicOrganizationMembershipForAUserRequest) (*protocol.GitHubCheckPublicOrganizationMembershipForAUserResponse, error) {
	return githubAnswer(s, ctx, "CheckPublicOrganizationMembershipForAUser", req, &protocol.GitHubCheckPublicOrganizationMembershipForAUserResponse{})
}

func (s *GitHubServer) SetPublicOrganizationMembershipForTheAuthenticatedUser(ctx context.Context, req *protocol.GitHubSetPublicOrganizationMembershipForTheAuthenticatedUserRequest) (*protocol.GitHubSetPublicOrganizationMembershipForTheAuthenticatedUserResponse, error) {
	return githubAnswer(s, ctx, "SetPublicOrganizationMembershipForTheAuthenticatedUser", req, &protocol.GitHubSetPublicOrganizationMembershipForTheAuthenticatedUserResponse{})
}

func (s *GitHubServer) RemovePublicOrganizationMembershipForTheAuthenticatedUser(ctx context.Context, req *protocol.GitHubRemovePublicOrganizationMembershipForTheAuthenticatedUserRequest) (*protocol.GitHubRemovePublicOrganizationMembershipForTheAuthenticatedUserResponse, error) {
	return githubAnswer(s, ctx, "RemovePublicOrganizationMembershipForTheAuthenticatedUser", req, &protocol.GitHubRemovePublicOrganizationMembershipForTheAuthenticatedUserResponse{})
}

func (s *GitHubServer) ListOrganizationMembershipsForTheAuthenticatedUser(ctx context.Context, req *protocol.GitHubListOrganizationMembershipsForTheAuthenticatedUserRequest) (*protocol.GitHubListOrganizationMembershipsForTheAuthenticatedUserResponse, error) {
	return githubAnswer(s, ctx, "ListOrganizationMembershipsForTheAuthenticatedUser", req, &protocol.GitHubListOrganizationMembershipsForTheAuthenticatedUserResponse{})
}

func (s *GitHubServer) GetAnOrganizationMembershipForTheAuthenticatedUser(ctx context.Context, req *protocol.GitHubGetAnOrganizationMembershipForTheAuthenticatedUserRequest) (*protocol.GitHubGetOrganizationMembershipForAUserResponse, error) {
	return githubAnswer(s, ctx, "GetAnOrganizationMembershipForTheAuthenticatedUser", req, &protocol.GitHubGetOrganizationMembershipForAUserResponse{})
}

func (s *GitHubServer) UpdateAnOrganizationMembershipForTheAuthenticatedUser(ctx context.Context, req *protocol.GitHubUpdateAnOrganizationMembershipForTheAuthenticatedUserRequest) (*protocol.GitHubGetOrganizationMembershipForAUserResponse, error) {
	return githubAnswer(s, ctx, "UpdateAnOrganizationMembershipForTheAuthenticatedUser", req, &protocol.GitHubGetOrganizationMembershipForAUserResponse{})
}

func (s *GitHubServer) ListOrganizations(ctx context.Context, req *protocol.GitHubListOrganizationsRequest) (*protocol.GitHubListOrganizationsResponse, error) {
	return githubAnswer(s, ctx, "ListOrganizations", req, &protocol.GitHubListOrganizationsResponse{})
}

func (s *GitHubServer) GetAnOrganization(ctx context.Context, req *protocol.GitHubGetAnOrganizationRequest) (*protocol.GitHubGetAnOrganizationResponse, error) {
	return githubAnswer(s, ctx, "GetAnOrganization", req, &protocol.GitHubGetAnOrganizationResponse{})
}

func (s *GitHubServer) UpdateAnOrganization(ctx context.Context, req *protocol.GitHubUpdateAnOrganizationRequest) (*protocol.GitHubGetAnOrganizationResponse, error) {
	return githubAnswer(s, ctx, "UpdateAnOrganization", req, &protocol.GitHubGetAnOrganizationResponse{})
}

func (s *GitHubServer) DeleteAnOrganization(ctx context.Context, req *protocol.GitHubDeleteAnOrganizationRequest) (*protocol.GitHubDeleteAnOrganizationResponse, error) {
	return githubAnswer(s, ctx, "DeleteAnOrganization", req, &protocol.GitHubDeleteAnOrganizationResponse{})
}

func (s *GitHubServer) ListAppInstallationsForAnOrganization(ctx context.Context, req *protocol.GitHubListAppInstallationsForAnOrganizationRequest) (*protocol.GitHubListAppInstallationsForAnOrganizationResponse, error) {
	return githubAnswer(s, ctx, "ListAppInstallationsForAnOrganization", req, &protocol.GitHubListAppInstallationsForAnOrganizationResponse{})
}

func (s *GitHubServer) GetImmutableReleasesSettingsForAnOrganization(ctx context.Context, req *protocol.GitHubGetImmutableReleasesSettingsForAnOrganizationRequest) (*protocol.GitHubGetImmutableReleasesSettingsForAnOrganizationResponse, error) {
	return githubAnswer(s, ctx, "GetImmutableReleasesSettingsForAnOrganization", req, &protocol.GitHubGetImmutableReleasesSettingsForAnOrganizationResponse{})
}

func (s *GitHubServer) SetImmutableReleasesSettingsForAnOrganization(ctx context.Context, req *protocol.GitHubSetImmutableReleasesSettingsForAnOrganizationRequest) (*protocol.GitHubSetImmutableReleasesSettingsForAnOrganizationResponse, error) {
	return githubAnswer(s, ctx, "SetImmutableReleasesSettingsForAnOrganization", req, &protocol.GitHubSetImmutableReleasesSettingsForAnOrganizationResponse{})
}

func (s *GitHubServer) ListSelectedRepositoriesForImmutableReleasesEnforcement(ctx context.Context, req *protocol.GitHubListSelectedRepositoriesForImmutableReleasesEnforcementRequest) (*protocol.GitHubListSelectedRepositoriesForImmutableReleasesEnforcementResponse, error) {
	return githubAnswer(s, ctx, "ListSelectedRepositoriesForImmutableReleasesEnforcement", req, &protocol.GitHubListSelectedRepositoriesForImmutableReleasesEnforcementResponse{})
}

func (s *GitHubServer) SetSelectedRepositoriesForImmutableReleasesEnforcement(ctx context.Context, req *protocol.GitHubSetSelectedRepositoriesForImmutableReleasesEnforcementRequest) (*protocol.GitHubSetSelectedRepositoriesForImmutableReleasesEnforcementResponse, error) {
	return githubAnswer(s, ctx, "SetSelectedRepositoriesForImmutableReleasesEnforcement", req, &protocol.GitHubSetSelectedRepositoriesForImmutableReleasesEnforcementResponse{})
}

func (s *GitHubServer) EnableASelectedRepositoryForImmutableReleasesInAnOrganization(ctx context.Context, req *protocol.GitHubEnableASelectedRepositoryForImmutableReleasesInAnOrganizationRequest) (*protocol.GitHubEnableASelectedRepositoryForImmutableReleasesInAnOrganizationResponse, error) {
	return githubAnswer(s, ctx, "EnableASelectedRepositoryForImmutableReleasesInAnOrganization", req, &protocol.GitHubEnableASelectedRepositoryForImmutableReleasesInAnOrganizationResponse{})
}

func (s *GitHubServer) DisableASelectedRepositoryForImmutableReleasesInAnOrganization(ctx context.Context, req *protocol.GitHubDisableASelectedRepositoryForImmutableReleasesInAnOrganizationRequest) (*protocol.GitHubDisableASelectedRepositoryForImmutableReleasesInAnOrganizationResponse, error) {
	return githubAnswer(s, ctx, "DisableASelectedRepositoryForImmutableReleasesInAnOrganization", req, &protocol.GitHubDisableASelectedRepositoryForImmutableReleasesInAnOrganizationResponse{})
}

func (s *GitHubServer) EnableOrDisableASecurityFeatureForAnOrganization(ctx context.Context, req *protocol.GitHubEnableOrDisableASecurityFeatureForAnOrganizationRequest) (*protocol.GitHubEnableOrDisableASecurityFeatureForAnOrganizationResponse, error) {
	return githubAnswer(s, ctx, "EnableOrDisableASecurityFeatureForAnOrganization", req, &protocol.GitHubEnableOrDisableASecurityFeatureForAnOrganizationResponse{})
}

func (s *GitHubServer) ListOrganizationsForTheAuthenticatedUser(ctx context.Context, req *protocol.GitHubListOrganizationsForTheAuthenticatedUserRequest) (*protocol.GitHubListOrganizationsResponse, error) {
	return githubAnswer(s, ctx, "ListOrganizationsForTheAuthenticatedUser", req, &protocol.GitHubListOrganizationsResponse{})
}

func (s *GitHubServer) ListOrganizationsForAUser(ctx context.Context, req *protocol.GitHubListOrganizationsForAUserRequest) (*protocol.GitHubListOrganizationsResponse, error) {
	return githubAnswer(s, ctx, "ListOrganizationsForAUser", req, &protocol.GitHubListOrganizationsResponse{})
}

func (s *GitHubServer) ListIssuesAssignedToTheAuthenticatedUser(ctx context.Context, req *protocol.GitHubListIssuesAssignedToTheAuthenticatedUserRequest) (*protocol.GitHubListIssuesAssignedToTheAuthenticatedUserResponse, error) {
	return githubAnswer(s, ctx, "ListIssuesAssignedToTheAuthenticatedUser", req, &protocol.GitHubListIssuesAssignedToTheAuthenticatedUserResponse{})
}

func (s *GitHubServer) ListOrganizationIssuesAssignedToTheAuthenticatedUser(ctx context.Context, req *protocol.GitHubListOrganizationIssuesAssignedToTheAuthenticatedUserRequest) (*protocol.GitHubListIssuesAssignedToTheAuthenticatedUserResponse, error) {
	return githubAnswer(s, ctx, "ListOrganizationIssuesAssignedToTheAuthenticatedUser", req, &protocol.GitHubListIssuesAssignedToTheAuthenticatedUserResponse{})
}

func (s *GitHubServer) ListRepositoryIssues(ctx context.Context, req *protocol.GitHubListRepositoryIssuesRequest) (*protocol.GitHubListIssuesAssignedToTheAuthenticatedUserResponse, error) {
	return githubAnswer(s, ctx, "ListRepositoryIssues", req, &protocol.GitHubListIssuesAssignedToTheAuthenticatedUserResponse{})
}

func (s *GitHubServer) CreateAnIssue(ctx context.Context, req *protocol.GitHubCreateAnIssueRequest) (*protocol.GitHubCreateAnIssueResponse, error) {
	return githubAnswer(s, ctx, "CreateAnIssue", req, &protocol.GitHubCreateAnIssueResponse{})
}

func (s *GitHubServer) GetAnIssue(ctx context.Context, req *protocol.GitHubGetAnIssueRequest) (*protocol.GitHubCreateAnIssueResponse, error) {
	return githubAnswer(s, ctx, "GetAnIssue", req, &protocol.GitHubCreateAnIssueResponse{})
}

func (s *GitHubServer) UpdateAnIssue(ctx context.Context, req *protocol.GitHubUpdateAnIssueRequest) (*protocol.GitHubUpdateAnIssueResponse, error) {
	return githubAnswer(s, ctx, "UpdateAnIssue", req, &protocol.GitHubUpdateAnIssueResponse{})
}

func (s *GitHubServer) LockAnIssue(ctx context.Context, req *protocol.GitHubLockAnIssueRequest) (*protocol.GitHubLockAnIssueResponse, error) {
	return githubAnswer(s, ctx, "LockAnIssue", req, &protocol.GitHubLockAnIssueResponse{})
}

func (s *GitHubServer) UnlockAnIssue(ctx context.Context, req *protocol.GitHubUnlockAnIssueRequest) (*protocol.GitHubUnlockAnIssueResponse, error) {
	return githubAnswer(s, ctx, "UnlockAnIssue", req, &protocol.GitHubUnlockAnIssueResponse{})
}

func (s *GitHubServer) ListIssueSuggestions(ctx context.Context, req *protocol.GitHubListIssueSuggestionsRequest) (*protocol.GitHubListIssueSuggestionsResponse, error) {
	return githubAnswer(s, ctx, "ListIssueSuggestions", req, &protocol.GitHubListIssueSuggestionsResponse{})
}

func (s *GitHubServer) ApproveAnIssueSuggestion(ctx context.Context, req *protocol.GitHubApproveAnIssueSuggestionRequest) (*protocol.GitHubApproveAnIssueSuggestionResponse, error) {
	return githubAnswer(s, ctx, "ApproveAnIssueSuggestion", req, &protocol.GitHubApproveAnIssueSuggestionResponse{})
}

func (s *GitHubServer) DismissAnIssueSuggestion(ctx context.Context, req *protocol.GitHubDismissAnIssueSuggestionRequest) (*protocol.GitHubApproveAnIssueSuggestionResponse, error) {
	return githubAnswer(s, ctx, "DismissAnIssueSuggestion", req, &protocol.GitHubApproveAnIssueSuggestionResponse{})
}

func (s *GitHubServer) ListUserAccountIssuesAssignedToTheAuthenticatedUser(ctx context.Context, req *protocol.GitHubListUserAccountIssuesAssignedToTheAuthenticatedUserRequest) (*protocol.GitHubListIssuesAssignedToTheAuthenticatedUserResponse, error) {
	return githubAnswer(s, ctx, "ListUserAccountIssuesAssignedToTheAuthenticatedUser", req, &protocol.GitHubListIssuesAssignedToTheAuthenticatedUserResponse{})
}

func (s *GitHubServer) ListOrganizationRepositories(ctx context.Context, req *protocol.GitHubListOrganizationRepositoriesRequest) (*protocol.GitHubListOrganizationRepositoriesResponse, error) {
	return githubAnswer(s, ctx, "ListOrganizationRepositories", req, &protocol.GitHubListOrganizationRepositoriesResponse{})
}

func (s *GitHubServer) CreateAnOrganizationRepository(ctx context.Context, req *protocol.GitHubCreateAnOrganizationRepositoryRequest) (*protocol.GitHubCreateAnOrganizationRepositoryResponse, error) {
	return githubAnswer(s, ctx, "CreateAnOrganizationRepository", req, &protocol.GitHubCreateAnOrganizationRepositoryResponse{})
}

func (s *GitHubServer) GetARepository(ctx context.Context, req *protocol.GitHubGetARepositoryRequest) (*protocol.GitHubCreateAnOrganizationRepositoryResponse, error) {
	return githubAnswer(s, ctx, "GetARepository", req, &protocol.GitHubCreateAnOrganizationRepositoryResponse{})
}

func (s *GitHubServer) UpdateARepository(ctx context.Context, req *protocol.GitHubUpdateARepositoryRequest) (*protocol.GitHubCreateAnOrganizationRepositoryResponse, error) {
	return githubAnswer(s, ctx, "UpdateARepository", req, &protocol.GitHubCreateAnOrganizationRepositoryResponse{})
}

func (s *GitHubServer) DeleteARepository(ctx context.Context, req *protocol.GitHubDeleteARepositoryRequest) (*protocol.GitHubDeleteARepositoryResponse, error) {
	return githubAnswer(s, ctx, "DeleteARepository", req, &protocol.GitHubDeleteARepositoryResponse{})
}

func (s *GitHubServer) ListRepositoryActivities(ctx context.Context, req *protocol.GitHubListRepositoryActivitiesRequest) (*protocol.GitHubListRepositoryActivitiesResponse, error) {
	return githubAnswer(s, ctx, "ListRepositoryActivities", req, &protocol.GitHubListRepositoryActivitiesResponse{})
}

func (s *GitHubServer) CheckIfDependabotSecurityUpdatesAreEnabledForARepository(ctx context.Context, req *protocol.GitHubCheckIfDependabotSecurityUpdatesAreEnabledForARepositoryRequest) (*protocol.GitHubCheckIfDependabotSecurityUpdatesAreEnabledForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "CheckIfDependabotSecurityUpdatesAreEnabledForARepository", req, &protocol.GitHubCheckIfDependabotSecurityUpdatesAreEnabledForARepositoryResponse{})
}

func (s *GitHubServer) EnableDependabotSecurityUpdates(ctx context.Context, req *protocol.GitHubEnableDependabotSecurityUpdatesRequest) (*protocol.GitHubEnableDependabotSecurityUpdatesResponse, error) {
	return githubAnswer(s, ctx, "EnableDependabotSecurityUpdates", req, &protocol.GitHubEnableDependabotSecurityUpdatesResponse{})
}

func (s *GitHubServer) DisableDependabotSecurityUpdates(ctx context.Context, req *protocol.GitHubDisableDependabotSecurityUpdatesRequest) (*protocol.GitHubDisableDependabotSecurityUpdatesResponse, error) {
	return githubAnswer(s, ctx, "DisableDependabotSecurityUpdates", req, &protocol.GitHubDisableDependabotSecurityUpdatesResponse{})
}

func (s *GitHubServer) ListCODEOWNERSErrors(ctx context.Context, req *protocol.GitHubListCODEOWNERSErrorsRequest) (*protocol.GitHubListCODEOWNERSErrorsResponse, error) {
	return githubAnswer(s, ctx, "ListCODEOWNERSErrors", req, &protocol.GitHubListCODEOWNERSErrorsResponse{})
}

func (s *GitHubServer) ListRepositoryContributors(ctx context.Context, req *protocol.GitHubListRepositoryContributorsRequest) (*protocol.GitHubListRepositoryContributorsResponse, error) {
	return githubAnswer(s, ctx, "ListRepositoryContributors", req, &protocol.GitHubListRepositoryContributorsResponse{})
}

func (s *GitHubServer) CreateARepositoryDispatchEvent(ctx context.Context, req *protocol.GitHubCreateARepositoryDispatchEventRequest) (*protocol.GitHubCreateARepositoryDispatchEventResponse, error) {
	return githubAnswer(s, ctx, "CreateARepositoryDispatchEvent", req, &protocol.GitHubCreateARepositoryDispatchEventResponse{})
}

func (s *GitHubServer) GetTheHashAlgorithmForARepository(ctx context.Context, req *protocol.GitHubGetTheHashAlgorithmForARepositoryRequest) (*protocol.GitHubGetTheHashAlgorithmForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "GetTheHashAlgorithmForARepository", req, &protocol.GitHubGetTheHashAlgorithmForARepositoryResponse{})
}

func (s *GitHubServer) CheckIfImmutableReleasesAreEnabledForARepository(ctx context.Context, req *protocol.GitHubCheckIfImmutableReleasesAreEnabledForARepositoryRequest) (*protocol.GitHubCheckIfImmutableReleasesAreEnabledForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "CheckIfImmutableReleasesAreEnabledForARepository", req, &protocol.GitHubCheckIfImmutableReleasesAreEnabledForARepositoryResponse{})
}

func (s *GitHubServer) EnableImmutableReleases(ctx context.Context, req *protocol.GitHubEnableImmutableReleasesRequest) (*protocol.GitHubEnableImmutableReleasesResponse, error) {
	return githubAnswer(s, ctx, "EnableImmutableReleases", req, &protocol.GitHubEnableImmutableReleasesResponse{})
}

func (s *GitHubServer) DisableImmutableReleases(ctx context.Context, req *protocol.GitHubDisableImmutableReleasesRequest) (*protocol.GitHubDisableImmutableReleasesResponse, error) {
	return githubAnswer(s, ctx, "DisableImmutableReleases", req, &protocol.GitHubDisableImmutableReleasesResponse{})
}

func (s *GitHubServer) ListRepositoryLanguages(ctx context.Context, req *protocol.GitHubListRepositoryLanguagesRequest) (*protocol.GitHubListRepositoryLanguagesResponse, error) {
	return githubAnswer(s, ctx, "ListRepositoryLanguages", req, &protocol.GitHubListRepositoryLanguagesResponse{})
}

func (s *GitHubServer) CheckIfPrivateVulnerabilityReportingIsEnabledForARepository(ctx context.Context, req *protocol.GitHubCheckIfPrivateVulnerabilityReportingIsEnabledForARepositoryRequest) (*protocol.GitHubCheckIfPrivateVulnerabilityReportingIsEnabledForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "CheckIfPrivateVulnerabilityReportingIsEnabledForARepository", req, &protocol.GitHubCheckIfPrivateVulnerabilityReportingIsEnabledForARepositoryResponse{})
}

func (s *GitHubServer) EnablePrivateVulnerabilityReportingForARepository(ctx context.Context, req *protocol.GitHubEnablePrivateVulnerabilityReportingForARepositoryRequest) (*protocol.GitHubEnablePrivateVulnerabilityReportingForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "EnablePrivateVulnerabilityReportingForARepository", req, &protocol.GitHubEnablePrivateVulnerabilityReportingForARepositoryResponse{})
}

func (s *GitHubServer) DisablePrivateVulnerabilityReportingForARepository(ctx context.Context, req *protocol.GitHubDisablePrivateVulnerabilityReportingForARepositoryRequest) (*protocol.GitHubDisablePrivateVulnerabilityReportingForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "DisablePrivateVulnerabilityReportingForARepository", req, &protocol.GitHubDisablePrivateVulnerabilityReportingForARepositoryResponse{})
}

func (s *GitHubServer) ListRepositoryTags(ctx context.Context, req *protocol.GitHubListRepositoryTagsRequest) (*protocol.GitHubListRepositoryTagsResponse, error) {
	return githubAnswer(s, ctx, "ListRepositoryTags", req, &protocol.GitHubListRepositoryTagsResponse{})
}

func (s *GitHubServer) ListRepositoryTeams(ctx context.Context, req *protocol.GitHubListRepositoryTeamsRequest) (*protocol.GitHubListRepositoryTeamsResponse, error) {
	return githubAnswer(s, ctx, "ListRepositoryTeams", req, &protocol.GitHubListRepositoryTeamsResponse{})
}

func (s *GitHubServer) GetAllRepositoryTopics(ctx context.Context, req *protocol.GitHubGetAllRepositoryTopicsRequest) (*protocol.GitHubGetAllRepositoryTopicsResponse, error) {
	return githubAnswer(s, ctx, "GetAllRepositoryTopics", req, &protocol.GitHubGetAllRepositoryTopicsResponse{})
}

func (s *GitHubServer) ReplaceAllRepositoryTopics(ctx context.Context, req *protocol.GitHubReplaceAllRepositoryTopicsRequest) (*protocol.GitHubGetAllRepositoryTopicsResponse, error) {
	return githubAnswer(s, ctx, "ReplaceAllRepositoryTopics", req, &protocol.GitHubGetAllRepositoryTopicsResponse{})
}

func (s *GitHubServer) TransferARepository(ctx context.Context, req *protocol.GitHubTransferARepositoryRequest) (*protocol.GitHubTransferARepositoryResponse, error) {
	return githubAnswer(s, ctx, "TransferARepository", req, &protocol.GitHubTransferARepositoryResponse{})
}

func (s *GitHubServer) CheckIfVulnerabilityAlertsAreEnabledForARepository(ctx context.Context, req *protocol.GitHubCheckIfVulnerabilityAlertsAreEnabledForARepositoryRequest) (*protocol.GitHubCheckIfVulnerabilityAlertsAreEnabledForARepositoryResponse, error) {
	return githubAnswer(s, ctx, "CheckIfVulnerabilityAlertsAreEnabledForARepository", req, &protocol.GitHubCheckIfVulnerabilityAlertsAreEnabledForARepositoryResponse{})
}

func (s *GitHubServer) EnableVulnerabilityAlerts(ctx context.Context, req *protocol.GitHubEnableVulnerabilityAlertsRequest) (*protocol.GitHubEnableVulnerabilityAlertsResponse, error) {
	return githubAnswer(s, ctx, "EnableVulnerabilityAlerts", req, &protocol.GitHubEnableVulnerabilityAlertsResponse{})
}

func (s *GitHubServer) DisableVulnerabilityAlerts(ctx context.Context, req *protocol.GitHubDisableVulnerabilityAlertsRequest) (*protocol.GitHubDisableVulnerabilityAlertsResponse, error) {
	return githubAnswer(s, ctx, "DisableVulnerabilityAlerts", req, &protocol.GitHubDisableVulnerabilityAlertsResponse{})
}

func (s *GitHubServer) CreateARepositoryUsingATemplate(ctx context.Context, req *protocol.GitHubCreateARepositoryUsingATemplateRequest) (*protocol.GitHubCreateAnOrganizationRepositoryResponse, error) {
	return githubAnswer(s, ctx, "CreateARepositoryUsingATemplate", req, &protocol.GitHubCreateAnOrganizationRepositoryResponse{})
}

func (s *GitHubServer) ListPublicRepositories(ctx context.Context, req *protocol.GitHubListPublicRepositoriesRequest) (*protocol.GitHubListOrganizationRepositoriesResponse, error) {
	return githubAnswer(s, ctx, "ListPublicRepositories", req, &protocol.GitHubListOrganizationRepositoriesResponse{})
}

func (s *GitHubServer) ListRepositoriesForTheAuthenticatedUser(ctx context.Context, req *protocol.GitHubListRepositoriesForTheAuthenticatedUserRequest) (*protocol.GitHubListRepositoriesForTheAuthenticatedUserResponse, error) {
	return githubAnswer(s, ctx, "ListRepositoriesForTheAuthenticatedUser", req, &protocol.GitHubListRepositoriesForTheAuthenticatedUserResponse{})
}

func (s *GitHubServer) CreateARepositoryForTheAuthenticatedUser(ctx context.Context, req *protocol.GitHubCreateARepositoryForTheAuthenticatedUserRequest) (*protocol.GitHubCreateAnOrganizationRepositoryResponse, error) {
	return githubAnswer(s, ctx, "CreateARepositoryForTheAuthenticatedUser", req, &protocol.GitHubCreateAnOrganizationRepositoryResponse{})
}

func (s *GitHubServer) ListRepositoriesForAUser(ctx context.Context, req *protocol.GitHubListRepositoriesForAUserRequest) (*protocol.GitHubListOrganizationRepositoriesResponse, error) {
	return githubAnswer(s, ctx, "ListRepositoriesForAUser", req, &protocol.GitHubListOrganizationRepositoriesResponse{})
}
