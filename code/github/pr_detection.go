package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"

	"github.com/teranos/QNTX/am"
)

// GitHubPR represents a minimal GitHub PR response
type GitHubPR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

// DetectCurrentPR attempts to auto-detect the PR number for the current branch
// Returns PR number or 0 if not found
//
// TODO(experimental): This feature is experimental and needs testing with:
// - Public repositories (should work without token)
// - Private repositories (requires GitHub token in config)
// - Different GitHub remote URL formats (ssh, https)
// - Edge cases: multiple PRs for same branch, forks, etc.
func DetectCurrentPR() (int, error) {
	// Get current branch
	branch, err := getCurrentBranch()
	if err != nil {
		return 0, fmt.Errorf("failed to get current branch: %w", err)
	}

	// Get repository owner/name from remote
	owner, repo, err := getRepoInfo()
	if err != nil {
		return 0, fmt.Errorf("failed to get repository info: %w", err)
	}

	// Query GitHub API for open PRs on this branch
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls?head=%s:%s&state=open",
		owner, repo, owner, branch)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Add GitHub token if available (required for private repos)
	cfg, err := am.Load()
	if err == nil && cfg.Code.GitHub.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Code.GitHub.Token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to query GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		// Check if this is a 404 and we don't have a token (likely private repo)
		if resp.StatusCode == 404 {
			cfg, _ := am.Load()
			if cfg == nil || cfg.Code.GitHub.Token == "" {
				return 0, fmt.Errorf("GitHub API returned 404 - this is likely a private repository.\n\nNOTE: PR auto-detection is experimental and needs testing.\n\nTo access private repositories, configure a GitHub token:\n  1. Generate token at: https://github.com/settings/tokens\n  2. Add to am.toml: [code.github]\n     token = \"your_token\"\n     OR set environment variable: QNTX_CODE_GITHUB_TOKEN=your_token\n\nRequired scopes: 'repo' (private repos) or 'public_repo' (public only)")
			}
		}

		return 0, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	var prs []GitHubPR
	if err := json.Unmarshal(body, &prs); err != nil {
		return 0, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(prs) == 0 {
		return 0, fmt.Errorf("no open PR found for branch '%s'", branch)
	}

	return prs[0].Number, nil
}

// getCurrentBranch returns the current git branch name
func getCurrentBranch() (string, error) {
	cmd := exec.Command("git", "branch", "--show-current")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// getRepoInfo extracts owner and repo name from git remote
// Returns owner, repo, error
func getRepoInfo() (string, string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return "", "", err
	}

	remoteURL := strings.TrimSpace(string(output))

	// Match patterns like:
	// - git@github.com:owner/repo.git
	// - https://github.com/owner/repo.git
	// - https://github.com/owner/repo
	re := regexp.MustCompile(`[:/]([^/]+)/([^/]+?)(\.git)?$`)
	matches := re.FindStringSubmatch(remoteURL)

	if len(matches) < 3 {
		return "", "", fmt.Errorf("could not parse repository from remote URL: %s", remoteURL)
	}

	owner := matches[1]
	repo := strings.TrimSuffix(matches[2], ".git")

	return owner, repo, nil
}
