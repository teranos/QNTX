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

	atstypes "github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/storage"
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
			err := p.storeAttestation([]string{branchName}, []string{"node_type"}, []string{"branch"}, nil, time.Now())
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

	// Get commit iterator
	commitIter, err := repo.CommitObjects()
	if err != nil {
		return nil, fmt.Errorf("failed to get commits: %w", err)
	}
	defer commitIter.Close()

	// Track progress with adaptive intervals based on verbosity
	processedCount := 0
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
		err := p.storeAttestation([]string{shortHash}, []string{"is_commit"}, []string{shortHash}, attrs, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to store commit attestation: %w", err)
		}
	}

	// Attestation 1b: commit node_type (explicit type declaration)
	nodeTypeAttestation := fmt.Sprintf("as %s node_type commit", shortHash)
	attestations = append(attestations, nodeTypeAttestation)
	attestationCount++

	if !p.dryRun {
		err := p.storeAttestation([]string{shortHash}, []string{"node_type"}, []string{"commit"}, nil, timestamp)
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
		err := p.storeAttestation([]string{shortHash}, []string{"authored_by"}, []string{authorName}, nil, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to store author attestation: %w", err)
		}
	}

	// Attestation 2b: author node_type (explicit type for author node)
	authorTypeAttestation := fmt.Sprintf("as %s node_type author", authorName)
	attestations = append(attestations, authorTypeAttestation)
	attestationCount++

	if !p.dryRun {
		err := p.storeAttestation([]string{authorName}, []string{"node_type"}, []string{"author"}, nil, timestamp)
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
		err := p.storeAttestation([]string{shortHash}, []string{"has_message"}, []string{message}, nil, timestamp)
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
		err := p.storeAttestation([]string{shortHash}, []string{"committed_at"}, []string{timestampStr}, nil, timestamp)
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
			err := p.storeAttestation([]string{shortHash}, []string{"is_child_of"}, []string{shortParentHash}, nil, timestamp)
			if err != nil {
				return nil, fmt.Errorf("failed to store parent attestation: %w", err)
			}
		}
	}

	// Attestation 5+: file relationships (which files this commit modified)
	// FIXED: Inverted to file→commit to avoid bounded storage limits (64 contexts/actor)
	// Old: commit modifies file (breaks on commits with 65+ files)
	// New: file modified_in commit (commit is unique context, no limit)
	stats, err := commit.Stats()
	if err == nil { // Stats might fail for some commits (e.g., merge commits with conflicts)
		fileCount := len(stats)
		if p.verbosity >= 4 && fileCount > 0 {
			fmt.Printf("    %s↳%s %sProcessing %d files...%s\n",
				colorMagenta, colorReset,
				colorCyan, fileCount, colorReset)
		}

		for fileIdx, fileStat := range stats {
			fileName := fileStat.Name
			fileAttestation := fmt.Sprintf("as %s modified_in %s", fileName, shortHash)
			attestations = append(attestations, fileAttestation)
			attestationCount++

			// Show file progress at very high verbosity
			if p.verbosity >= 4 {
				fmt.Printf("      %s[%d/%d]%s %s%s%s %smodified_in%s %s%s%s\n",
					colorGray, fileIdx+1, fileCount, colorReset,
					colorCyan, fileName, colorReset,
					colorMagenta, colorReset,
					colorYellow, shortHash, colorReset)
			}

			if !p.dryRun {
				// Store file→commit modification relationship
				// Subject: file, Predicate: modified_in, Context: commit
				// This avoids bounded storage: each commit is a unique context
				err := p.storeAttestation([]string{fileName}, []string{"modified_in"}, []string{shortHash}, nil, timestamp)
				if err != nil {
					return nil, fmt.Errorf("failed to store file attestation: %w", err)
				}

				// Store directory hierarchy: file → directories
				// e.g., "internal/ixgest/git/ingest.go" → "internal/ixgest/git/" → "internal/ixgest/" → "internal/"
				dirs := extractDirectories(fileName)
				for i, dir := range dirs {
					if i == 0 {
						// Link file to immediate parent directory
						err := p.storeAttestation([]string{fileName}, []string{"in_directory"}, []string{dir}, nil, timestamp)
						if err != nil {
							return nil, fmt.Errorf("failed to store file→directory attestation: %w", err)
						}
					} else {
						// Link parent directory to grandparent directory
						err := p.storeAttestation([]string{dirs[i-1]}, []string{"in_directory"}, []string{dir}, nil, timestamp)
						if err != nil {
							return nil, fmt.Errorf("failed to store directory→directory attestation: %w", err)
						}
					}
				}

				// Parse Go files to extract package and import relationships
				if strings.HasSuffix(fileName, ".go") && !strings.Contains(fileName, "_test.go") {
					pkgInfo := p.parseGoFile(commit, fileName)
					if pkgInfo != nil {
						// Attestation: file declares_package pkg
						if pkgInfo.PackageName != "" {
							err := p.storeAttestation([]string{fileName}, []string{"declares_package"}, []string{pkgInfo.PackageName}, nil, timestamp)
							if err != nil {
								return nil, fmt.Errorf("failed to store package attestation: %w", err)
							}
						}

						// Attestation: file imports pkg (for each import)
						for _, importPath := range pkgInfo.Imports {
							err := p.storeAttestation([]string{fileName}, []string{"imports"}, []string{importPath}, nil, timestamp)
							if err != nil {
								return nil, fmt.Errorf("failed to store import attestation: %w", err)
							}
						}
					}
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

// storeAttestation stores a git attestation using ASID-as-actor pattern (self-certifying).
// For custom actors (e.g., type definitions), use storeTypeDefinition() or call store.CreateAttestation() directly.
func (p *GitIxProcessor) storeAttestation(subjects []string, predicates []string, contexts []string, attrs map[string]interface{}, timestamp time.Time) error {
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

	// Generate ASID with empty actor seed for self-certification
	asid, err := id.GenerateASIDWithVanity(subject, predicate, context, "")
	if err != nil {
		return fmt.Errorf("failed to generate ASID: %w", err)
	}

	// Create attestation with self-certifying actor (ASID vouches for itself)
	// This avoids bounded storage actor limits (64 contexts per actor)
	attestation := &atstypes.As{
		ID:         asid,
		Subjects:   subjects,
		Predicates: predicates,
		Contexts:   contexts,
		Actors:     []string{asid}, // Self-referential: attestation IS its own actor
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

// truncateMessage truncates a commit message to a reasonable length for display
func truncateMessage(message string) string {
	if len(message) > 80 {
		return message[:77] + "..."
	}
	return message
}

// extractDirectories extracts the directory hierarchy from a file path.
// Example: "internal/ixgest/git/ingest.go" → ["internal/ixgest/git/", "internal/ixgest/", "internal/"]
func extractDirectories(filePath string) []string {
	var dirs []string
	parts := strings.Split(filePath, "/")

	// Build directory paths from most specific to most general
	for i := len(parts) - 1; i > 0; i-- {
		dir := strings.Join(parts[:i], "/") + "/"
		dirs = append(dirs, dir)
	}

	return dirs
}

// GoPackageInfo contains parsed Go package information
type GoPackageInfo struct {
	PackageName string
	Imports     []string
}

// parseGoFile parses a Go file from a commit and extracts package and import information
func (p *GitIxProcessor) parseGoFile(commit *object.Commit, filePath string) *GoPackageInfo {
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
