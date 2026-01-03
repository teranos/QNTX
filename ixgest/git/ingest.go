package git

// Git repository ingestion for QNTX attestation system.

import (
	"database/sql"
	"fmt"
	"go/parser"
	"go/token"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats/storage"
	atstypes "github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ixgest/types"
	"github.com/teranos/vanity-id"
)

const (
	// ProgressInterval defines how often to log progress during commit processing
	ProgressInterval = 100

	// ANSI color codes for terminal output
	colorReset   = "\033[0m"
	colorCyan    = "\033[36m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorGray    = "\033[90m"
	colorGreen   = "\033[32m"
	colorMagenta = "\033[35m"
)

// GitIxProcessor handles git repository processing for attestation generation
//
// TODO(#108): Add GitHub PR node support with API integration
// See: https://github.com/sbvh-nl/qntx/issues/108
// Capture: PR metadata, status, reviews, CI/CD pipeline results
// Actor strategy: Use per-PR actors like "pr-{number}@github"
type GitIxProcessor struct {
	db           *sql.DB
	store        *storage.SQLStore
	dryRun       bool
	defaultActor string // Used for repo-level attestations (branches)
	verbosity    int
	logger       *zap.SugaredLogger
	since        string // Filter: only process commits after this timestamp or hash
	sinceTime    *time.Time
	sinceHash    string
}

// GitProcessingResult represents the result of git processing
type GitProcessingResult struct {
	RepositoryPath    string            `json:"repository_path"`
	DryRun            bool              `json:"dry_run"`
	Actor             string            `json:"actor"`
	CommitsProcessed  int               `json:"commits_processed"`
	BranchesProcessed int               `json:"branches_processed"`
	TotalAttestations int               `json:"total_attestations"`
	Commits           []GitCommitResult `json:"commits,omitempty"`
	Branches          []GitBranchResult `json:"branches,omitempty"`
	Success           bool              `json:"success"`
	Message           string            `json:"message"`
	StartTime         time.Time         `json:"start_time"`
	EndTime           time.Time         `json:"end_time"`
}

// GitCommitResult represents the result of processing a single commit
type GitCommitResult struct {
	Hash             string   `json:"hash"`
	ShortHash        string   `json:"short_hash"`
	Message          string   `json:"message"`
	Author           string   `json:"author"`
	Timestamp        string   `json:"timestamp"`
	ParentCount      int      `json:"parent_count"`
	AttestationCount int      `json:"attestation_count"`
	Attestations     []string `json:"attestations,omitempty"`
}

// GitBranchResult represents the result of processing a single branch
type GitBranchResult struct {
	Name             string   `json:"name"`
	CommitHash       string   `json:"commit_hash"`
	AttestationCount int      `json:"attestation_count"`
	Attestations     []string `json:"attestations,omitempty"`
}

// NewGitIxProcessor creates a new git ix processor
func NewGitIxProcessor(db *sql.DB, dryRun bool, actor string, verbosity int, logger *zap.SugaredLogger) *GitIxProcessor {
	if actor == "" {
		actor = "ixgest-git@repo" // Used for repo-level attestations (branches)
	}
	return &GitIxProcessor{
		db:           db,
		store:        storage.NewSQLStore(db, logger),
		dryRun:       dryRun,
		defaultActor: actor,
		verbosity:    verbosity,
		logger:       logger,
	}
}

// SetSince configures the processor to only ingest commits after the given
// timestamp or commit hash. Supports:
//   - RFC3339 timestamps: "2025-01-01T00:00:00Z"
//   - Date strings: "2025-01-01"
//   - Commit hashes: "abc1234" (7+ chars)
func (p *GitIxProcessor) SetSince(since string) error {
	if since == "" {
		return nil
	}
	p.since = since

	// Try parsing as RFC3339 timestamp
	if t, err := time.Parse(time.RFC3339, since); err == nil {
		p.sinceTime = &t
		p.logger.Infow("Filtering commits since timestamp", "since", t.Format(time.RFC3339))
		return nil
	}

	// Try parsing as date (YYYY-MM-DD)
	if t, err := time.Parse("2006-01-02", since); err == nil {
		p.sinceTime = &t
		p.logger.Infow("Filtering commits since date", "since", t.Format("2006-01-02"))
		return nil
	}

	// Assume it's a commit hash (7+ characters)
	if len(since) >= 7 {
		p.sinceHash = since
		p.logger.Infow("Filtering commits since hash", "since", since)
		return nil
	}

	return fmt.Errorf("invalid --since value: %q (use RFC3339 timestamp, date, or commit hash)", since)
}

// ProcessGitRepository processes a git repository and generates attestations
func (p *GitIxProcessor) ProcessGitRepository(repoPath string) (*GitProcessingResult, error) {
	result := &GitProcessingResult{
		RepositoryPath: repoPath,
		DryRun:         p.dryRun,
		Actor:          p.defaultActor,
		StartTime:      time.Now(),
	}

	// Open the repository
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("Failed to open repository: %v", err)
		return result, fmt.Errorf("failed to open repository: %w", err)
	}

	// Ensure type definitions exist for git node types (skip in dry-run mode)
	// Non-fatal - will fall back to hardcoded types if this fails
	if !p.dryRun {
		if err := atstypes.EnsureTypes(p.store, "ixgest-git", types.Commit, types.Author, types.Branch); err != nil {
			p.logger.Warnw("Failed to create type definitions - falling back to hardcoded types",
				"error", err,
				"component", "git_ingestion",
				"source", "ixgest-git",
				"types_attempted", []string{"commit", "author", "branch"},
				"impact", "graph visualization may lack custom type metadata",
				"fallback", "using hardcoded NodeTypeColors map")
		}

		// Ensure relationship type definitions exist for git predicates
		// Non-fatal - frontend will fall back to default physics if this fails
		if err := atstypes.EnsureRelationshipTypes(p.store, "ixgest-git", types.IsChildOf, types.PointsTo); err != nil {
			p.logger.Warnw("Failed to create relationship type definitions - falling back to default physics",
				"error", err,
				"component", "git_ingestion",
				"source", "ixgest-git",
				"types_attempted", []string{"is_child_of", "points_to"},
				"impact", "graph physics will use default values",
				"fallback", "using frontend GRAPH_PHYSICS constants")
		}
	}

	// Process branches
	branchResults, err := p.processBranches(repo)
	if err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("Failed to process branches: %v", err)
		return result, fmt.Errorf("failed to process branches: %w", err)
	}
	result.Branches = branchResults
	result.BranchesProcessed = len(branchResults)

	// Process commits
	commitResults, err := p.processCommits(repo)
	if err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("Failed to process commits: %v", err)
		return result, fmt.Errorf("failed to process commits: %w", err)
	}
	result.Commits = commitResults
	result.CommitsProcessed = len(commitResults)

	// Calculate total attestations
	for _, commit := range commitResults {
		result.TotalAttestations += commit.AttestationCount
	}
	for _, branch := range branchResults {
		result.TotalAttestations += branch.AttestationCount
	}

	result.EndTime = time.Now()
	result.Success = true
	result.Message = fmt.Sprintf("Successfully processed %d commits and %d branches", result.CommitsProcessed, result.BranchesProcessed)

	return result, nil
}

// processBranches processes all branches in the repository
func (p *GitIxProcessor) processBranches(repo *git.Repository) ([]GitBranchResult, error) {
	var results []GitBranchResult

	// Get all branch references
	refs, err := repo.Branches()
	if err != nil {
		return nil, fmt.Errorf("failed to get branches: %w", err)
	}

	err = refs.ForEach(func(ref *plumbing.Reference) error {
		branchName := ref.Name().Short()
		commitHash := ref.Hash().String()
		shortHash := commitHash[:7]

		var attestations []string
		attestationCount := 0

		// Create attestation: branch points_to commit
		branchAttestation := fmt.Sprintf("as %s points_to %s", branchName, shortHash)
		attestations = append(attestations, branchAttestation)
		attestationCount++

		if !p.dryRun {
			err := p.storeAttestation([]string{branchName}, []string{"points_to"}, []string{shortHash}, nil, time.Now())
			if err != nil {
				return fmt.Errorf("failed to store branch attestation: %w", err)
			}
		}

		// Create attestation: branch node_type (explicit type declaration)
		branchTypeAttestation := fmt.Sprintf("as %s node_type branch", branchName)
		attestations = append(attestations, branchTypeAttestation)
		attestationCount++

		if !p.dryRun {
			err := p.storeAttestation([]string{branchName}, []string{"node_type"}, []string{types.Branch.Name}, nil, time.Now())
			if err != nil {
				return fmt.Errorf("failed to store branch node_type attestation: %w", err)
			}
		}

		results = append(results, GitBranchResult{
			Name:             branchName,
			CommitHash:       commitHash,
			AttestationCount: attestationCount,
			Attestations:     attestations,
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	return results, nil
}

// processCommits processes all commits in the repository
func (p *GitIxProcessor) processCommits(repo *git.Repository) ([]GitCommitResult, error) {
	var results []GitCommitResult

	// If filtering by hash, resolve to timestamp first
	if p.sinceHash != "" && p.sinceTime == nil {
		sinceCommit, err := repo.CommitObject(plumbing.NewHash(p.sinceHash))
		if err != nil {
			// Try as short hash by iterating
			commitIter, _ := repo.CommitObjects()
			_ = commitIter.ForEach(func(c *object.Commit) error {
				if strings.HasPrefix(c.Hash.String(), p.sinceHash) {
					t := c.Author.When
					p.sinceTime = &t
					return fmt.Errorf("found") // Break iteration
				}
				return nil
			})
			commitIter.Close()
		} else {
			t := sinceCommit.Author.When
			p.sinceTime = &t
		}
		if p.sinceTime != nil {
			p.logger.Infow("Resolved commit hash to timestamp",
				"hash", p.sinceHash,
				"timestamp", p.sinceTime.Format(time.RFC3339))
		}
	}

	// Get commit iterator
	commitIter, err := repo.CommitObjects()
	if err != nil {
		return nil, fmt.Errorf("failed to get commits: %w", err)
	}
	defer commitIter.Close()

	// Track progress with adaptive intervals based on verbosity
	processedCount := 0
	skippedCount := 0
	var progressInterval int
	switch {
	case p.verbosity >= 3:
		progressInterval = 1 // Show every commit at high verbosity
	case p.verbosity == 2:
		progressInterval = 10 // Show every 10 commits
	case p.verbosity == 1:
		progressInterval = 25 // Show every 25 commits
	default:
		progressInterval = 100 // Minimal progress at verbosity 0
	}

	// Process each commit
	err = commitIter.ForEach(func(commit *object.Commit) error {
		// Filter by --since if set
		if p.sinceTime != nil {
			if !commit.Author.When.After(*p.sinceTime) {
				skippedCount++
				return nil // Skip commits at or before the since time
			}
		}

		commitResult, err := p.processCommit(commit)
		if err != nil {
			return fmt.Errorf("failed to process commit %s: %w", commit.Hash.String()[:7], err)
		}

		results = append(results, *commitResult)
		processedCount++

		// Show progress based on verbosity level
		if p.verbosity > 0 && processedCount%progressInterval == 0 {
			// Verbosity 3+: Show full details with colors
			if p.verbosity >= 3 {
				fmt.Printf("  %s[%d]%s %s%s%s by %s%s%s: %s%s%s %s(+%d attestations)%s\n",
					colorCyan, processedCount, colorReset,
					colorYellow, commitResult.ShortHash, colorReset,
					colorBlue, commitResult.Author, colorReset,
					colorGray, commitResult.Message, colorReset,
					colorGreen, commitResult.AttestationCount, colorReset)
			} else if p.verbosity == 2 {
				// Verbosity 2: Show commit details
				fmt.Printf("  %s[%d]%s %s%s%s - %s%s%s\n",
					colorCyan, processedCount, colorReset,
					colorYellow, commitResult.ShortHash, colorReset,
					colorGray, commitResult.Message, colorReset)
			} else {
				// Verbosity 1: Just count with color
				fmt.Printf("  %sProcessed %d commits...%s\n",
					colorGreen, processedCount, colorReset)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if skippedCount > 0 {
		p.logger.Infow("Skipped commits before --since cutoff",
			"skipped", skippedCount,
			"processed", processedCount)
	}

	return results, nil
}

// processCommit processes a single commit and generates attestations
func (p *GitIxProcessor) processCommit(commit *object.Commit) (*GitCommitResult, error) {
	if commit == nil {
		return nil, fmt.Errorf("commit is nil")
	}

	hash := commit.Hash.String()
	shortHash := hash[:7]
	message := strings.TrimSpace(strings.Split(commit.Message, "\n")[0]) // First line only
	authorName := commit.Author.Name
	authorEmail := commit.Author.Email
	timestamp := commit.Author.When

	var attestations []string
	attestationCount := 0

	// Create commit attestation attributes
	attrs := map[string]interface{}{
		"full_hash":    hash,
		"message":      commit.Message,
		"author_email": authorEmail,
		"committer":    commit.Committer.Name,
		// Note: files_changed requires expensive Stats() calculation
		// Omitted for performance - can be added if needed
	}

	// Show attestation creation header at ultra-high verbosity
	if p.verbosity >= 5 {
		fmt.Printf("\n  %s✓%s Creating attestations for %s%s%s...\n",
			colorMagenta, colorReset,
			colorYellow, shortHash, colorReset)
	}

	// Attestation 1: commit is_commit (self-referential: commit attests to itself)
	commitAttestation := fmt.Sprintf("as %s is_commit %s", shortHash, shortHash)
	attestations = append(attestations, commitAttestation)
	attestationCount++

	if p.verbosity >= 5 {
		fmt.Printf("    %s✓%s %s%s%s %sis_commit%s %s%s%s\n",
			colorGreen, colorReset,
			colorYellow, shortHash, colorReset,
			colorCyan, colorReset,
			colorYellow, shortHash, colorReset)
	}

	if !p.dryRun {
		// Context "commit_metadata" groups all commit properties (is_commit, has_message, committed_at)
		err := p.storeAttestationWithActor(shortHash, []string{shortHash}, []string{"is_commit"}, []string{"commit_metadata"}, attrs, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to store commit attestation: %w", err)
		}
	}

	// Attestation 1b: commit node_type (explicit type declaration)
	nodeTypeAttestation := fmt.Sprintf("as %s node_type commit", shortHash)
	attestations = append(attestations, nodeTypeAttestation)
	attestationCount++

	if !p.dryRun {
		err := p.storeAttestationWithActor(shortHash, []string{shortHash}, []string{"node_type"}, []string{types.Commit.Name}, nil, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to store node_type attestation: %w", err)
		}
	}

	// Attestation 2: commit authored_by author
	authorAttestation := fmt.Sprintf("as %s authored_by %s", shortHash, authorName)
	attestations = append(attestations, authorAttestation)
	attestationCount++

	if p.verbosity >= 5 {
		fmt.Printf("    %s✓%s %s%s%s %s%s%s %s%s%s\n",
			colorGreen, colorReset,
			colorYellow, shortHash, colorReset,
			colorCyan, "authored_by", colorReset,
			colorBlue, authorName, colorReset)
	}

	if !p.dryRun {
		// Context "authorship" groups all author-related attestations
		err := p.storeAttestationWithActor(shortHash, []string{shortHash}, []string{"authored_by"}, []string{"authorship"}, nil, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to store author attestation: %w", err)
		}
	}

	// Attestation 2b: author node_type (explicit type for author node)
	// Use commit hash as actor to avoid bounded storage issues (many commits per author)
	authorTypeAttestation := fmt.Sprintf("as %s node_type author", authorName)
	attestations = append(attestations, authorTypeAttestation)
	attestationCount++

	if !p.dryRun {
		err := p.storeAttestationWithActor(shortHash, []string{authorName}, []string{"node_type"}, []string{types.Author.Name}, nil, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to store author node_type attestation: %w", err)
		}
	}

	// Attestation 3: commit has_message "message"
	messageAttestation := fmt.Sprintf("as %s has_message \"%s\"", shortHash, truncateMessage(message))
	attestations = append(attestations, messageAttestation)
	attestationCount++

	if p.verbosity >= 5 {
		fmt.Printf("    %s✓%s %s%s%s %s%s%s %s%s%s\n",
			colorGreen, colorReset,
			colorYellow, shortHash, colorReset,
			colorCyan, "has_message", colorReset,
			colorGray, truncateMessage(message), colorReset)
	}

	if !p.dryRun {
		// Context "commit_metadata" groups all commit properties
		err := p.storeAttestationWithActor(shortHash, []string{shortHash}, []string{"has_message"}, []string{"commit_metadata"}, nil, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to store message attestation: %w", err)
		}
	}

	// Attestation 3b: commit committed_at timestamp (enables temporal queries and evolution tracking)
	timestampStr := timestamp.Format(time.RFC3339)
	committedAtAttestation := fmt.Sprintf("as %s committed_at %s", shortHash, timestampStr)
	attestations = append(attestations, committedAtAttestation)
	attestationCount++

	if p.verbosity >= 5 {
		fmt.Printf("    %s✓%s %s%s%s %s%s%s %s%s%s\n",
			colorGreen, colorReset,
			colorYellow, shortHash, colorReset,
			colorCyan, "committed_at", colorReset,
			colorMagenta, timestamp.Format("2006-01-02 15:04:05"), colorReset)
	}

	if !p.dryRun {
		// Context "commit_metadata" groups all commit properties
		err := p.storeAttestationWithActor(shortHash, []string{shortHash}, []string{"committed_at"}, []string{"commit_metadata"}, nil, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to store committed_at attestation: %w", err)
		}
	}

	// Attestation 4+: parent relationships
	for _, parentHash := range commit.ParentHashes {
		shortParentHash := parentHash.String()[:7]
		parentAttestation := fmt.Sprintf("as %s is_child_of %s", shortHash, shortParentHash)
		attestations = append(attestations, parentAttestation)
		attestationCount++

		if !p.dryRun {
			// Context "lineage" groups all parent/child relationships
			err := p.storeAttestationWithActor(shortHash, []string{shortHash}, []string{"is_child_of"}, []string{"lineage"}, nil, timestamp)
			if err != nil {
				return nil, fmt.Errorf("failed to store parent attestation: %w", err)
			}
		}
	}

	// Attestation 5+: package relationships (which packages this commit modified)
	// Package-level tracking with semantic contexts to avoid bounded storage limits
	// Context "code_changes" is shared across all commits, preventing context explosion
	stats, err := commit.Stats()
	if err == nil { // Stats might fail for some commits (e.g., merge commits with conflicts)
		// Extract modified packages from file stats
		modifiedPackages := extractModifiedPackages(commit, stats)
		packageCount := len(modifiedPackages)

		if p.verbosity >= 4 && packageCount > 0 {
			fmt.Printf("    %s↳%s %sProcessing %d packages...%s\n",
				colorMagenta, colorReset,
				colorCyan, packageCount, colorReset)
		}

		for pkgIdx, pkgName := range modifiedPackages {
			packageAttestation := fmt.Sprintf("as %s modified_in %s", pkgName, shortHash)
			attestations = append(attestations, packageAttestation)
			attestationCount++

			// Show package progress at very high verbosity
			if p.verbosity >= 4 {
				fmt.Printf("      %s[%d/%d]%s %s%s%s %smodified_in%s %s%s%s\n",
					colorGray, pkgIdx+1, packageCount, colorReset,
					colorCyan, pkgName, colorReset,
					colorMagenta, colorReset,
					colorYellow, shortHash, colorReset)
			}

			if !p.dryRun {
				// Store package→commit modification relationship
				// Actor: commit hash, Subject: package, Predicate: modified_in
				// Context: "code_changes" (semantic context, not instance data)
				// This ensures all commits share the same context type
				err := p.storeAttestationWithActor(shortHash, []string{pkgName}, []string{"modified_in"}, []string{"code_changes"}, nil, timestamp)
				if err != nil {
					return nil, fmt.Errorf("failed to store package attestation: %w", err)
				}
			}
		}
	}

	return &GitCommitResult{
		Hash:             hash,
		ShortHash:        shortHash,
		Message:          message,
		Author:           authorName,
		Timestamp:        timestamp.Format(time.RFC3339),
		ParentCount:      len(commit.ParentHashes),
		AttestationCount: attestationCount,
		Attestations:     attestations,
	}, nil
}

// storeAttestation stores a git attestation with an explicit actor.
// The actor parameter allows context-aware actor selection (e.g., commit hash for commit-related attestations).
func (p *GitIxProcessor) storeAttestationWithActor(actor string, subjects []string, predicates []string, contexts []string, attrs map[string]interface{}, timestamp time.Time) error {
	// Generate attestation ID using first elements of each array (or empty string if array is empty)
	subject := ""
	if len(subjects) > 0 {
		subject = subjects[0]
	}
	predicate := ""
	if len(predicates) > 0 {
		predicate = predicates[0]
	}
	context := ""
	if len(contexts) > 0 {
		context = contexts[0]
	}

	// Generate ASID with provided actor
	asid, err := id.GenerateASIDWithVanity(subject, predicate, context, actor)
	if err != nil {
		return fmt.Errorf("failed to generate ASID: %w", err)
	}

	// Create attestation with specified actor
	// Bounded storage: Each commit is its own actor with ~10 contexts (is_commit, node_type, authored_by, etc.)
	// This fits well within the 64-contexts-per-actor limit
	attestation := &atstypes.As{
		ID:         asid,
		Subjects:   subjects,
		Predicates: predicates,
		Contexts:   contexts,
		Actors:     []string{actor},
		Timestamp:  timestamp,
		Source:     "ixgest-git",
		Attributes: attrs,
	}

	// Store in database
	err = p.store.CreateAttestation(attestation)
	if err != nil {
		return fmt.Errorf("failed to store attestation: %w", err)
	}

	return nil
}

// storeAttestation stores a git attestation using the default actor (backward compatibility wrapper)
func (p *GitIxProcessor) storeAttestation(subjects []string, predicates []string, contexts []string, attrs map[string]interface{}, timestamp time.Time) error {
	return p.storeAttestationWithActor(p.defaultActor, subjects, predicates, contexts, attrs, timestamp)
}

// truncateMessage truncates a commit message to a reasonable length for display
func truncateMessage(message string) string {
	if len(message) > 80 {
		return message[:77] + "..."
	}
	return message
}

// extractModifiedPackages extracts unique Go packages modified in a commit.
// It parses Go files to determine package names and deduplicates them.
// For non-Go files, uses directory-based heuristics.
func extractModifiedPackages(commit *object.Commit, stats object.FileStats) []string {
	packageSet := make(map[string]bool)
	var packages []string

	for _, fileStat := range stats {
		fileName := fileStat.Name

		// For Go files, parse to get actual package name
		if strings.HasSuffix(fileName, ".go") {
			pkgInfo := parseGoFileFromCommit(commit, fileName)
			if pkgInfo != nil && pkgInfo.PackageName != "" {
				// Use directory path as package identifier (more stable than package name)
				// e.g., "internal/ixgest/git/ingest.go" → "internal/ixgest/git"
				dir := extractPackageDir(fileName)
				if dir != "" && !packageSet[dir] {
					packageSet[dir] = true
					packages = append(packages, dir)
				}
			}
		} else {
			// For non-Go files, use directory as package identifier
			// e.g., "docs/README.md" → "docs"
			dir := extractPackageDir(fileName)
			if dir != "" && !packageSet[dir] {
				packageSet[dir] = true
				packages = append(packages, dir)
			}
		}
	}

	return packages
}

// extractPackageDir extracts the directory containing a file as package identifier.
// Example: "internal/ixgest/git/ingest.go" → "internal/ixgest/git"
//
//	"README.md" → "." (root)
func extractPackageDir(filePath string) string {
	parts := strings.Split(filePath, "/")
	if len(parts) == 1 {
		return "." // Root directory files
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

// GoPackageInfo contains parsed Go package information
type GoPackageInfo struct {
	PackageName string
	Imports     []string
}

// parseGoFileFromCommit parses a Go file from a commit and extracts package and import information
func parseGoFileFromCommit(commit *object.Commit, filePath string) *GoPackageInfo {
	// Get the file from the commit tree
	tree, err := commit.Tree()
	if err != nil {
		return nil
	}

	file, err := tree.File(filePath)
	if err != nil {
		return nil
	}

	// Read file contents
	contents, err := file.Contents()
	if err != nil {
		return nil
	}

	// Parse the Go source
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, contents, parser.ImportsOnly)
	if err != nil {
		// Parsing failed - file might be invalid Go, skip silently
		return nil
	}

	info := &GoPackageInfo{
		PackageName: f.Name.Name,
		Imports:     make([]string, 0),
	}

	// Extract import paths
	for _, imp := range f.Imports {
		// Remove quotes from import path
		importPath := strings.Trim(imp.Path.Value, `"`)
		info.Imports = append(info.Imports, importPath)
	}

	return info
}

// IsGitRepository checks if a path is a git repository
func IsGitRepository(path string) bool {
	_, err := git.PlainOpen(path)
	return err == nil
}
