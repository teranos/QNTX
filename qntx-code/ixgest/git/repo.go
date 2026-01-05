package git

// Repository source resolution for QNTX git ingestion.
// Uses hashicorp/go-getter for flexible source handling including:
//   - Local paths
//   - Git URLs (https, ssh, git://)
//   - GitHub/GitLab shorthand (github.com/user/repo)
//   - Archives (zip, tar.gz) with auto-extraction

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-getter"
	"go.uber.org/zap"
)

// RepoSource represents a resolved repository source
type RepoSource struct {
	// LocalPath is the path to the local repository (either original or fetched)
	LocalPath string
	// OriginalInput is the original input (URL or path)
	OriginalInput string
	// IsCloned indicates if the repo was fetched from a remote source
	IsCloned bool
	// TempDir is the temporary directory used for fetching (empty if local)
	TempDir string
	// cleanup function to call when done with the repo
	cleanup func()
}

// Cleanup removes any temporary resources created for this repo source.
// Safe to call multiple times.
func (r *RepoSource) Cleanup() {
	if r.cleanup != nil {
		r.cleanup()
		r.cleanup = nil
	}
}

// ResolveRepository resolves an input to a local repository using go-getter.
// Supports:
//   - Local paths: /path/to/repo, ./relative/path, ~/home/path
//   - Git URLs: https://github.com/user/repo, git@github.com:user/repo.git
//   - GitHub shorthand: github.com/user/repo (auto-detected by go-getter)
//   - Archives: https://example.com/repo.tar.gz (auto-extracted)
//
// Returns a RepoSource that must be cleaned up when done.
func ResolveRepository(input string, logger *zap.SugaredLogger) (*RepoSource, error) {
	pwd, err := os.Getwd()
	if err != nil {
		pwd = "."
	}

	// Use go-getter's detection to identify source type
	detected, err := getter.Detect(input, pwd, getter.Detectors)
	if err != nil {
		return nil, fmt.Errorf("failed to detect source type: %w", err)
	}

	logger.Debugw("go-getter detected source",
		"input", input,
		"detected", detected,
	)

	// Parse the detected URL to determine if it's local or remote
	parsedURL, err := url.Parse(detected)
	if err != nil {
		return nil, fmt.Errorf("failed to parse detected URL: %w", err)
	}

	// For file:// URLs or local paths, use directly
	if parsedURL.Scheme == "file" || parsedURL.Scheme == "" {
		localPath := input
		if parsedURL.Scheme == "file" {
			localPath = parsedURL.Path
		}

		// Handle tilde expansion
		if strings.HasPrefix(localPath, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to expand home directory: %w", err)
			}
			localPath = filepath.Join(home, localPath[2:])
		}

		// Make absolute
		if !filepath.IsAbs(localPath) {
			localPath = filepath.Join(pwd, localPath)
		}

		// Verify it's a git repository
		if !IsGitRepository(localPath) {
			return nil, fmt.Errorf("not a git repository: %s", localPath)
		}

		return &RepoSource{
			LocalPath:     localPath,
			OriginalInput: input,
			IsCloned:      false,
			cleanup:       func() {}, // No cleanup needed for local repos
		}, nil
	}

	// Remote source - fetch to temp directory
	return fetchRepository(input, detected, logger)
}

// fetchRepository fetches a remote repository using go-getter.
func fetchRepository(input, detected string, logger *zap.SugaredLogger) (*RepoSource, error) {
	// Extract repo name for temp directory naming
	repoName := extractRepoName(input)

	logger.Infow("Fetching repository",
		"input", input,
		"detected", detected,
		"repo_name", repoName,
	)

	// Create temp directory
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("qntx-ix-%s-*", repoName))
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Configure go-getter client
	client := &getter.Client{
		Ctx:  context.Background(),
		Src:  detected,
		Dst:  tempDir,
		Mode: getter.ClientModeDir,
		// Use default getters which include git, http, s3, gcs, etc.
		Getters: getter.Getters,
	}

	logger.Infow("Fetching with go-getter",
		"destination", tempDir,
	)

	if err := client.Get(); err != nil {
		// Cleanup temp dir on error
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to fetch repository: %w", err)
	}

	// Verify the result is a git repository
	if !IsGitRepository(tempDir) {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("fetched content is not a git repository")
	}

	logger.Infow("Fetch completed",
		"destination", tempDir,
	)

	return &RepoSource{
		LocalPath:     tempDir,
		OriginalInput: input,
		IsCloned:      true,
		TempDir:       tempDir,
		cleanup: func() {
			logger.Debugw("Cleaning up fetched repository", "path", tempDir)
			os.RemoveAll(tempDir)
		},
	}, nil
}

// extractRepoName extracts a clean repository name from a URL or path.
// Used for temp directory naming.
func extractRepoName(input string) string {
	// Remove common suffixes
	input = strings.TrimSuffix(input, ".git")
	input = strings.TrimSuffix(input, "/")

	// Handle various URL formats
	if strings.Contains(input, "/") {
		parts := strings.Split(input, "/")
		// Return last non-empty component
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" {
				return sanitizeRepoName(parts[i])
			}
		}
	}

	return sanitizeRepoName(input)
}

// sanitizeRepoName removes or replaces characters not safe for directory names.
func sanitizeRepoName(name string) string {
	// Remove common prefixes that aren't part of the repo name
	name = strings.TrimPrefix(name, "git@")

	// Replace unsafe characters
	replacer := strings.NewReplacer(
		":", "-",
		"@", "-",
		" ", "-",
	)
	name = replacer.Replace(name)

	// Limit length
	if len(name) > 50 {
		name = name[:50]
	}

	if name == "" {
		name = "repo"
	}

	return name
}

// IsRepoURL checks if the input looks like a remote git repository URL.
// Uses go-getter's detection to identify remote sources.
func IsRepoURL(input string) bool {
	pwd, err := os.Getwd()
	if err != nil {
		pwd = "."
	}

	detected, err := getter.Detect(input, pwd, getter.Detectors)
	if err != nil {
		return false
	}

	parsedURL, err := url.Parse(detected)
	if err != nil {
		return false
	}

	// Remote if scheme is not file:// or empty (local path)
	return parsedURL.Scheme != "" && parsedURL.Scheme != "file"
}
