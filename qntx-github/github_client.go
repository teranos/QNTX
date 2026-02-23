package qntxgithub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

const githubAPIBase = "https://api.github.com"

// GitHubClient wraps GitHub API operations.
type GitHubClient struct {
	token      string
	httpClient *http.Client
	logger     *zap.SugaredLogger
}

// NewGitHubClient creates a new GitHub API client.
func NewGitHubClient(token string, logger *zap.SugaredLogger) *GitHubClient {
	return &GitHubClient{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// Event represents a GitHub repository event.
type Event struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Actor     Actor     `json:"actor"`
	Repo      Repo      `json:"repo"`
	Payload   Payload   `json:"payload"`
}

// Actor represents the user who triggered the event.
type Actor struct {
	Login string `json:"login"`
	URL   string `json:"url"`
}

// Repo represents the repository.
type Repo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Payload contains event-specific data.
type Payload struct {
	Action      string       `json:"action,omitempty"`
	PullRequest *PullRequest `json:"pull_request,omitempty"`
	Release     *Release     `json:"release,omitempty"`
	Issue       *Issue       `json:"issue,omitempty"`
	Ref         string       `json:"ref,omitempty"`         // For PushEvent
	Size        int          `json:"size,omitempty"`        // For PushEvent (commit count)
	Commits     []Commit     `json:"commits,omitempty"`     // For PushEvent
}

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	Number  int       `json:"number"`
	Title   string    `json:"title"`
	HTMLURL string    `json:"html_url"`
	Merged  bool      `json:"merged"`
	MergedAt *time.Time `json:"merged_at,omitempty"`
	User    Actor     `json:"user"`
	Base    Branch    `json:"base"`
	Head    Branch    `json:"head"`
}

// Release represents a GitHub release.
type Release struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	Author  Actor  `json:"author"`
}

// Issue represents a GitHub issue.
type Issue struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
	User    Actor  `json:"user"`
	State   string `json:"state"`
}

// Branch represents a git branch reference.
type Branch struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// Commit represents a git commit.
type Commit struct {
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Author  Author `json:"author"`
}

// Author represents a commit author.
type Author struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// GetEvents fetches recent events for a repository.
func (c *GitHubClient) GetEvents(ctx context.Context, owner, repo string) ([]Event, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/events", githubAPIBase, owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create request")
	}

	// Add authentication if token is available
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch events")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("GitHub API returned status %d", resp.StatusCode)
	}

	var events []Event
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, errors.Wrap(err, "failed to decode events")
	}

	return events, nil
}
