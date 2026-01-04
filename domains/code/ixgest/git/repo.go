package git

// Repository source resolution for QNTX git ingestion.
// Handles both local paths and remote URLs with automatic cloning.

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"go.uber.org/zap"
)

// RepoSource represents a resolved repository source
type RepoSource struct {
	// LocalPath is the path to the local repository (either original or cloned)
	LocalPath string
	// OriginalInput is the original input (URL or path)
	OriginalInput string
	// IsCloned indicates if the repo was cloned from a remote URL
	IsCloned bool
	// TempDir is the temporary directory used for cloning (empty if not cloned)
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

// IsRepoURL checks if the input looks like a git repository URL.
// Supports:
//   - HTTPS URLs: https://github.com/user/repo, https://github.com/user/repo.git
//   - SSH URLs: git@github.com:user/repo.git
//   - Git protocol: git://github.com/user/repo.git
func IsRepoURL(input string) bool {
	// HTTPS URLs
	if strings.HasPrefix(input, "https://") || strings.HasPrefix(input, "http://") {
		return isGitHostURL(input)
	}

	// SSH URLs (git@host:path)
	if strings.HasPrefix(input, "git@") {
		return true
	}

	// Git protocol
	if strings.HasPrefix(input, "git://") {
		return true
	}

	return false
}

// isGitHostURL checks if an HTTP(S) URL is a known git host or ends with .git
func isGitHostURL(url string) bool {
	// Common git hosting providers
	knownHosts := []string{
		"github.com",
		"gitlab.com",
		"bitbucket.org",
		"codeberg.org",
		"sr.ht",
		"gitea.com",
	}

	lowered := strings.ToLower(url)
	for _, host := range knownHosts {
		if strings.Contains(lowered, host) {
			return true
		}
	}

	// Check for .git suffix
	if strings.HasSuffix(url, ".git") {
		return true
	}

	return false
}

// NormalizeRepoURL ensures the URL is in a format go-git can clone.
// Adds .git suffix if missing for GitHub/GitLab URLs.
func NormalizeRepoURL(url string) string {
	// Already has .git suffix
	if strings.HasSuffix(url, ".git") {
		return url
	}

	// Remove trailing slash
	url = strings.TrimSuffix(url, "/")

	// For HTTPS URLs to known hosts, append .git
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		// GitHub, GitLab, etc. work better with .git suffix
		if isGitHostURL(url) {
			return url + ".git"
		}
	}

	return url
}

// ExtractRepoName extracts a clean repository name from a URL or path.
// Used for display and temp directory naming.
func ExtractRepoName(input string) string {
	// Remove .git suffix
	input = strings.TrimSuffix(input, ".git")

	// Handle SSH URLs (git@github.com:user/repo)
	if strings.HasPrefix(input, "git@") {
		// Extract path after the colon
		if idx := strings.Index(input, ":"); idx != -1 {
			input = input[idx+1:]
		}
	}

	// Handle HTTPS/HTTP URLs
	if strings.HasPrefix(input, "https://") || strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "git://") {
		// Extract path from URL
		re := regexp.MustCompile(`^(?:https?|git)://[^/]+/(.+)$`)
		if matches := re.FindStringSubmatch(input); len(matches) > 1 {
			input = matches[1]
		}
	}

	// Get last path component as repo name
	parts := strings.Split(input, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return input
}

// ResolveRepository resolves an input (URL or local path) to a local repository.
// For URLs, performs a shallow clone to a temporary directory.
// Returns a RepoSource that must be cleaned up when done.
func ResolveRepository(input string, logger *zap.SugaredLogger) (*RepoSource, error) {
	source := &RepoSource{
		OriginalInput: input,
	}

	// Check if input is a URL
	if IsRepoURL(input) {
		return cloneRepository(input, logger)
	}

	// Local path - verify it's a git repository
	if !IsGitRepository(input) {
		return nil, fmt.Errorf("not a git repository: %s", input)
	}

	source.LocalPath = input
	source.IsCloned = false
	source.cleanup = func() {} // No cleanup needed for local repos

	return source, nil
}

// cloneRepository clones a remote repository to a temporary directory.
func cloneRepository(url string, logger *zap.SugaredLogger) (*RepoSource, error) {
	// Normalize the URL
	normalizedURL := NormalizeRepoURL(url)
	repoName := ExtractRepoName(url)

	logger.Infow("Cloning repository",
		"url", normalizedURL,
		"repo_name", repoName,
	)

	// Create temp directory
	tempDir, err := os.MkdirTemp("", fmt.Sprintf("qntx-ix-%s-*", repoName))
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Clone with shallow depth
	logger.Infow("Performing shallow clone",
		"destination", tempDir,
		"depth", 1,
	)

	_, err = git.PlainClone(tempDir, false, &git.CloneOptions{
		URL:   normalizedURL,
		Depth: 1, // Shallow clone
		// Note: For public repos only, no auth needed
	})

	if err != nil {
		// Cleanup temp dir on error
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	logger.Infow("Clone completed",
		"destination", tempDir,
	)

	return &RepoSource{
		LocalPath:     tempDir,
		OriginalInput: url,
		IsCloned:      true,
		TempDir:       tempDir,
		cleanup: func() {
			logger.Debugw("Cleaning up cloned repository", "path", tempDir)
			os.RemoveAll(tempDir)
		},
	}, nil
}
