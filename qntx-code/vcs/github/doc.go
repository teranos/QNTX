// Package github provides GitHub integration for code review and PR workflows.
//
// This package enables QNTX to interact with GitHub pull requests:
// - Fetch fix suggestions from PR comments (qntx-fix blocks)
// - Auto-detect PR number from current git branch
// - Check review staleness (detect if code changed after review)
// - Monitor GitHub Actions workflow status
//
// Usage:
//
//	// Auto-detect PR and fetch fix suggestions
//	prNumber, err := github.DetectCurrentPR()
//	suggestions, err := github.FetchFixSuggestions(prNumber)
//
//	// Check if code was modified after review
//	staleness, err := github.CheckReviewStaleness(prNumber, "path/to/file.go")
//
// Configuration:
//
// GitHub token can be configured in am.toml for private repositories:
//
//	[code.github]
//	token = "ghp_..."
//
// Or via environment variable:
//
//	QNTX_CODE_GITHUB_TOKEN=ghp_...
//
// Integration with GitHub Actions:
//
// The claude-code-review.yml workflow uses Claude Code to analyze PRs
// and post machine-readable fix suggestions in qntx-fix blocks:
//
//	```qntx-fix
//	[
//	  {
//	    "id": "FIX-1",
//	    "title": "Add nil check",
//	    "file": "path/to/file.go",
//	    "start_line": 10,
//	    "end_line": 15,
//	    "issue": "Missing nil check",
//	    "severity": "high",
//	    "patch": ""
//	  }
//	]
//	```
package github
