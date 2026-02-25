package qntxgithub

import (
	"context"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// HandlePulseJob handles Pulse job execution for GitHub event polling.
// Returns the number of attestations created.
func (p *Plugin) HandlePulseJob(ctx context.Context, jobID string) (int, error) {
	logger := p.Services().Logger("github")

	// Skip if paused
	if p.IsPaused() {
		logger.Debug("GitHub event polling skipped (plugin paused)")
		return 0, nil
	}

	config := p.Services().Config("github")
	repos := config.GetStringSlice("repos")
	if len(repos) == 0 {
		logger.Debug("No repositories configured, skipping event poll")
		return 0, nil
	}

	logger.Infow("Polling GitHub events", "repos", repos)

	// Poll each configured repository
	totalEvents := 0
	var errs []error
	for _, repoSpec := range repos {
		parts := strings.Split(repoSpec, "/")
		if len(parts) != 2 {
			logger.Warnw("Invalid repository format (expected owner/repo)", "repo", repoSpec)
			continue
		}
		owner, repo := parts[0], parts[1]

		events, err := p.pollRepository(ctx, owner, repo)
		if err != nil {
			logger.Warnw("Failed to poll repository", "repo", repoSpec, "error", err)
			errs = append(errs, err)
			continue
		}

		totalEvents += events
	}

	logger.Infow("GitHub event poll complete", "total_events", totalEvents, "errors", len(errs))

	if len(errs) > 0 && totalEvents == 0 {
		return 0, errors.Newf("all repository polls failed (%d errors)", len(errs))
	}

	return totalEvents, nil
}

// pollRepository polls a single repository for events and creates attestations.
func (p *Plugin) pollRepository(ctx context.Context, owner, repo string) (int, error) {
	logger := p.Services().Logger("github")

	// Fetch events from GitHub API
	events, err := p.client.GetEvents(ctx, owner, repo)
	if err != nil {
		return 0, err
	}

	store := p.Services().ATSStore()
	if store == nil {
		return 0, errors.New("ATS store not available")
	}

	// Process events and create attestations
	attestationCount := 0
	for _, event := range events {
		if err := p.createAttestationForEvent(ctx, store, event); err != nil {
			logger.Warnw("Failed to create attestation for event",
				"event_type", event.Type,
				"event_id", event.ID,
				"error", err)
		} else {
			attestationCount++
		}
	}

	return attestationCount, nil
}

// createAttestationForEvent creates an attestation for a GitHub event.
func (p *Plugin) createAttestationForEvent(ctx context.Context, store ats.AttestationStore, event Event) error {
	// Only create attestations for interesting event types
	switch event.Type {
	case "PullRequestEvent":
		return p.attestPullRequestEvent(ctx, store, event)
	case "ReleaseEvent":
		return p.attestReleaseEvent(ctx, store, event)
	case "IssuesEvent":
		return p.attestIssueEvent(ctx, store, event)
	case "PushEvent":
		return p.attestPushEvent(ctx, store, event)
	default:
		// Skip other event types
		return nil
	}
}

// attestPullRequestEvent creates an attestation for a pull request event.
func (p *Plugin) attestPullRequestEvent(ctx context.Context, store ats.AttestationStore, event Event) error {
	if event.Payload.PullRequest == nil {
		return nil
	}

	pr := event.Payload.PullRequest
	action := event.Payload.Action

	// Only attest for merged PRs
	if action != "closed" || !pr.Merged {
		return nil
	}

	attrs := map[string]interface{}{
		"pr_number":  pr.Number,
		"pr_title":   pr.Title,
		"pr_url":     pr.HTMLURL,
		"author":     pr.User.Login,
		"base_ref":   pr.Base.Ref,
		"head_ref":   pr.Head.Ref,
		"event_id":   event.ID,
		"created_at": event.CreatedAt.Format(time.RFC3339),
	}
	if pr.MergedAt != nil {
		attrs["merged_at"] = pr.MergedAt.Format(time.RFC3339)
	}

	cmd := &types.AsCommand{
		Subjects:   []string{pr.HTMLURL},
		Predicates: []string{"pr-merged"},
		Contexts:   []string{"github"},
		Source:     "github",
		Attributes: attrs,
	}

	_, err := store.GenerateAndCreateAttestation(ctx, cmd)
	return err
}

// attestReleaseEvent creates an attestation for a release event.
func (p *Plugin) attestReleaseEvent(ctx context.Context, store ats.AttestationStore, event Event) error {
	if event.Payload.Release == nil {
		return nil
	}

	release := event.Payload.Release
	action := event.Payload.Action

	// Only attest for published releases
	if action != "published" {
		return nil
	}

	attrs := map[string]interface{}{
		"tag":        release.TagName,
		"name":       release.Name,
		"url":        release.HTMLURL,
		"author":     release.Author.Login,
		"body":       release.Body,
		"event_id":   event.ID,
		"created_at": event.CreatedAt.Format(time.RFC3339),
	}

	cmd := &types.AsCommand{
		Subjects:   []string{release.HTMLURL},
		Predicates: []string{"released"},
		Contexts:   []string{"github"},
		Source:     "github",
		Attributes: attrs,
	}

	_, err := store.GenerateAndCreateAttestation(ctx, cmd)
	return err
}

// attestIssueEvent creates an attestation for an issue event.
//
// TODO(#607): Fetch full issue body via separate API call to include description in attestation.
func (p *Plugin) attestIssueEvent(ctx context.Context, store ats.AttestationStore, event Event) error {
	if event.Payload.Issue == nil {
		return nil
	}

	issue := event.Payload.Issue
	action := event.Payload.Action

	// Only attest for opened and closed issues
	if action != "opened" && action != "closed" {
		return nil
	}

	predicate := "issue-opened"
	if action == "closed" {
		predicate = "issue-closed"
	}

	attrs := map[string]interface{}{
		"issue_number": issue.Number,
		"title":        issue.Title,
		"url":          issue.HTMLURL,
		"author":       issue.User.Login,
		"state":        issue.State,
		"event_id":     event.ID,
		"created_at":   event.CreatedAt.Format(time.RFC3339),
	}

	cmd := &types.AsCommand{
		Subjects:   []string{issue.HTMLURL},
		Predicates: []string{predicate},
		Contexts:   []string{"github"},
		Source:     "github",
		Attributes: attrs,
	}

	_, err := store.GenerateAndCreateAttestation(ctx, cmd)
	return err
}

// attestPushEvent creates an attestation for a push event.
func (p *Plugin) attestPushEvent(ctx context.Context, store ats.AttestationStore, event Event) error {
	// Only attest pushes to main/master branches
	ref := event.Payload.Ref
	if ref != "refs/heads/main" && ref != "refs/heads/master" {
		return nil
	}

	// Extract branch name from ref
	branch := strings.TrimPrefix(ref, "refs/heads/")

	attrs := map[string]interface{}{
		"ref":          ref,
		"branch":       branch,
		"commit_count": event.Payload.Size,
		"actor":        event.Actor.Login,
		"event_id":     event.ID,
		"created_at":   event.CreatedAt.Format(time.RFC3339),
	}

	// Include commit details if available
	if len(event.Payload.Commits) > 0 {
		commits := make([]map[string]interface{}, 0, len(event.Payload.Commits))
		for _, commit := range event.Payload.Commits {
			commits = append(commits, map[string]interface{}{
				"sha":     commit.SHA,
				"message": commit.Message,
				"author":  commit.Author.Name,
			})
		}
		attrs["commits"] = commits
	}

	cmd := &types.AsCommand{
		Subjects:   []string{event.Repo.URL + "/tree/" + branch},
		Predicates: []string{"pushed"},
		Contexts:   []string{"github"},
		Source:     "github",
		Attributes: attrs,
	}

	_, err := store.GenerateAndCreateAttestation(ctx, cmd)
	return err
}
