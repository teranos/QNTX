package git

// Project file detection and dependency ingestion for QNTX.
// Automatically detects and ingests project manifests like go.mod, Cargo.toml, etc.

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats/storage"
	atstypes "github.com/teranos/QNTX/ats/types"
	id "github.com/teranos/vanity-id"
)

// ProjectFile represents a detected project file
type ProjectFile struct {
	Type string // e.g., "go.mod", "Cargo.toml", "package.json"
	Path string // Full path to the file
	Name string // Base name
}

// DepsIngestionResult holds the results of dependency ingestion
type DepsIngestionResult struct {
	FilesDetected     int                 `json:"files_detected"`
	FilesProcessed    int                 `json:"files_processed"`
	TotalAttestations int                 `json:"total_attestations"`
	ProjectFiles      []ProjectFileResult `json:"project_files,omitempty"`
}

// ProjectFileResult holds results for a single project file
type ProjectFileResult struct {
	Type             string   `json:"type"`
	Path             string   `json:"path"`
	AttestationCount int      `json:"attestation_count"`
	Attestations     []string `json:"attestations,omitempty"`
	Error            string   `json:"error,omitempty"`
}

// DepsIxProcessor handles project file detection and ingestion
type DepsIxProcessor struct {
	db        *sql.DB
	store     *storage.SQLStore
	repoPath  string
	dryRun    bool
	actor     string
	verbosity int
	logger    *zap.SugaredLogger
}

// NewDepsIxProcessor creates a new dependency ingestion processor
func NewDepsIxProcessor(db *sql.DB, repoPath string, dryRun bool, actor string, verbosity int, logger *zap.SugaredLogger) *DepsIxProcessor {
	return &DepsIxProcessor{
		db:        db,
		store:     storage.NewSQLStore(db, logger),
		repoPath:  repoPath,
		dryRun:    dryRun,
		actor:     actor,
		verbosity: verbosity,
		logger:    logger,
	}
}

// DetectProjectFiles scans the repository for known project files
func (p *DepsIxProcessor) DetectProjectFiles() []ProjectFile {
	var files []ProjectFile

	// Define project files to look for
	projectFiles := []string{
		"go.mod",
		"go.sum",
		"Cargo.toml",
		"Cargo.lock",
		"package.json",
		"package-lock.json",
		"bun.lockb",
		"bun.lock",
		"yarn.lock",
		"pnpm-lock.yaml",
		"flake.nix",
		"flake.lock",
		"pyproject.toml",
		"requirements.txt",
		"Pipfile",
		"Pipfile.lock",
		"pom.xml",
		"build.gradle",
		"build.gradle.kts",
		"Gemfile",
		"Gemfile.lock",
		"mix.exs",
		"mix.lock",
		"pubspec.yaml",
		"pubspec.lock",
		"composer.json",
		"composer.lock",
	}

	for _, filename := range projectFiles {
		fullPath := filepath.Join(p.repoPath, filename)
		if _, err := os.Stat(fullPath); err == nil {
			files = append(files, ProjectFile{
				Type: filename,
				Path: fullPath,
				Name: filename,
			})
		}
	}

	return files
}

// ProcessProjectFiles processes all detected project files and generates attestations
func (p *DepsIxProcessor) ProcessProjectFiles() (*DepsIngestionResult, error) {
	files := p.DetectProjectFiles()

	result := &DepsIngestionResult{
		FilesDetected: len(files),
	}

	for _, file := range files {
		fileResult := p.processProjectFile(file)
		result.ProjectFiles = append(result.ProjectFiles, fileResult)
		result.TotalAttestations += fileResult.AttestationCount
		if fileResult.Error == "" {
			result.FilesProcessed++
		}
	}

	return result, nil
}

// processProjectFile processes a single project file based on its type
func (p *DepsIxProcessor) processProjectFile(file ProjectFile) ProjectFileResult {
	result := ProjectFileResult{
		Type: file.Type,
		Path: file.Path,
	}

	var attestations []string
	var err error

	switch file.Type {
	case "go.mod":
		attestations, err = p.processGoMod(file.Path)
	case "go.sum":
		attestations, err = p.processGoSum(file.Path)
	case "Cargo.toml":
		attestations, err = p.processCargoToml(file.Path)
	case "Cargo.lock":
		attestations, err = p.processCargoLock(file.Path)
	case "package.json":
		attestations, err = p.processPackageJSON(file.Path)
	case "flake.nix":
		attestations, err = p.processFlakeNix(file.Path)
	case "flake.lock":
		attestations, err = p.processFlakeLock(file.Path)
	case "pyproject.toml":
		attestations, err = p.processPyprojectToml(file.Path)
	case "requirements.txt":
		attestations, err = p.processRequirementsTxt(file.Path)
	default:
		// For unhandled types, just create a presence attestation
		attestations, err = p.processGenericProjectFile(file)
	}

	if err != nil {
		result.Error = err.Error()
		p.logger.Warnw("Failed to process project file",
			"file", file.Path,
			"type", file.Type,
			"error", err,
		)
	}

	result.Attestations = attestations
	result.AttestationCount = len(attestations)

	return result
}

// processGoMod parses go.mod and generates attestations
func (p *DepsIxProcessor) processGoMod(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read go.mod: %w", err)
	}

	var attestations []string
	lines := strings.Split(string(content), "\n")

	var moduleName string
	var goVersion string
	var inRequireBlock bool
	var inReplaceBlock bool

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}

		// Module declaration
		if strings.HasPrefix(line, "module ") {
			moduleName = strings.TrimPrefix(line, "module ")
			moduleName = strings.TrimSpace(moduleName)
			attestation := fmt.Sprintf("go.mod declares_module %s", moduleName)
			attestations = append(attestations, attestation)
			if err := p.storeAttestation("go.mod", "declares_module", moduleName, nil); err != nil {
				return attestations, err
			}
			continue
		}

		// Go version
		if strings.HasPrefix(line, "go ") {
			goVersion = strings.TrimPrefix(line, "go ")
			goVersion = strings.TrimSpace(goVersion)
			attestation := fmt.Sprintf("go.mod requires_go %s", goVersion)
			attestations = append(attestations, attestation)
			if err := p.storeAttestation("go.mod", "requires_go", goVersion, nil); err != nil {
				return attestations, err
			}
			continue
		}

		// Track block state
		if line == "require (" {
			inRequireBlock = true
			continue
		}
		if line == "replace (" {
			inReplaceBlock = true
			continue
		}
		if line == ")" {
			inRequireBlock = false
			inReplaceBlock = false
			continue
		}

		// Single-line require
		if strings.HasPrefix(line, "require ") && !strings.HasSuffix(line, "(") {
			dep := strings.TrimPrefix(line, "require ")
			parts := strings.Fields(dep)
			if len(parts) >= 2 {
				depName := parts[0]
				depVersion := parts[1]
				attestation := fmt.Sprintf("go.mod requires %s version %s", depName, depVersion)
				attestations = append(attestations, attestation)
				attrs := map[string]interface{}{"version": depVersion}
				if err := p.storeAttestation("go.mod", "requires", depName, attrs); err != nil {
					return attestations, err
				}
			}
			continue
		}

		// Require block entries
		if inRequireBlock && !inReplaceBlock {
			parts := strings.Fields(line)
			if len(parts) >= 2 && !strings.HasPrefix(parts[0], "//") {
				depName := parts[0]
				depVersion := parts[1]
				attestation := fmt.Sprintf("go.mod requires %s version %s", depName, depVersion)
				attestations = append(attestations, attestation)
				attrs := map[string]interface{}{"version": depVersion}
				if err := p.storeAttestation("go.mod", "requires", depName, attrs); err != nil {
					return attestations, err
				}
			}
		}
	}

	return attestations, nil
}

// processGoSum parses go.sum and generates attestations for locked versions
func (p *DepsIxProcessor) processGoSum(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read go.sum: %w", err)
	}
	defer file.Close()

	var attestations []string
	seen := make(map[string]bool) // Deduplicate (go.sum has duplicate entries)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			depName := parts[0]
			depVersion := strings.TrimSuffix(parts[1], "/go.mod")

			key := depName + "@" + depVersion
			if seen[key] {
				continue
			}
			seen[key] = true

			attestation := fmt.Sprintf("go.sum locks %s at %s", depName, depVersion)
			attestations = append(attestations, attestation)
			attrs := map[string]interface{}{"version": depVersion}
			if err := p.storeAttestation("go.sum", "locks", depName, attrs); err != nil {
				return attestations, err
			}
		}
	}

	return attestations, scanner.Err()
}

// CargoToml represents a minimal Cargo.toml structure
type CargoToml struct {
	Package struct {
		Name    string `toml:"name"`
		Version string `toml:"version"`
	} `toml:"package"`
	Dependencies    map[string]interface{} `toml:"dependencies"`
	DevDependencies map[string]interface{} `toml:"dev-dependencies"`
}

// processCargoToml parses Cargo.toml and generates attestations
func (p *DepsIxProcessor) processCargoToml(path string) ([]string, error) {
	var cargo CargoToml
	if _, err := toml.DecodeFile(path, &cargo); err != nil {
		return nil, fmt.Errorf("failed to parse Cargo.toml: %w", err)
	}

	var attestations []string

	// Package declaration
	if cargo.Package.Name != "" {
		attestation := fmt.Sprintf("Cargo.toml declares_package %s", cargo.Package.Name)
		attestations = append(attestations, attestation)
		attrs := map[string]interface{}{"version": cargo.Package.Version}
		if err := p.storeAttestation("Cargo.toml", "declares_package", cargo.Package.Name, attrs); err != nil {
			return attestations, err
		}
	}

	// Dependencies
	for dep, val := range cargo.Dependencies {
		version := extractCargoVersion(val)
		attestation := fmt.Sprintf("Cargo.toml depends_on %s version %s", dep, version)
		attestations = append(attestations, attestation)
		attrs := map[string]interface{}{"version": version, "dev": false}
		if err := p.storeAttestation("Cargo.toml", "depends_on", dep, attrs); err != nil {
			return attestations, err
		}
	}

	// Dev dependencies
	for dep, val := range cargo.DevDependencies {
		version := extractCargoVersion(val)
		attestation := fmt.Sprintf("Cargo.toml dev_depends_on %s version %s", dep, version)
		attestations = append(attestations, attestation)
		attrs := map[string]interface{}{"version": version, "dev": true}
		if err := p.storeAttestation("Cargo.toml", "dev_depends_on", dep, attrs); err != nil {
			return attestations, err
		}
	}

	return attestations, nil
}

// extractCargoVersion extracts version from Cargo.toml dependency value
func extractCargoVersion(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case map[string]interface{}:
		if ver, ok := v["version"].(string); ok {
			return ver
		}
		if git, ok := v["git"].(string); ok {
			return "git:" + git
		}
		if path, ok := v["path"].(string); ok {
			return "path:" + path
		}
	}
	return "*"
}

// CargoLock represents a minimal Cargo.lock structure
type CargoLock struct {
	Package []struct {
		Name    string `toml:"name"`
		Version string `toml:"version"`
		Source  string `toml:"source"`
	} `toml:"package"`
}

// processCargoLock parses Cargo.lock and generates attestations
func (p *DepsIxProcessor) processCargoLock(path string) ([]string, error) {
	var lock CargoLock
	if _, err := toml.DecodeFile(path, &lock); err != nil {
		return nil, fmt.Errorf("failed to parse Cargo.lock: %w", err)
	}

	var attestations []string

	for _, pkg := range lock.Package {
		attestation := fmt.Sprintf("Cargo.lock locks %s at %s", pkg.Name, pkg.Version)
		attestations = append(attestations, attestation)
		attrs := map[string]interface{}{
			"version": pkg.Version,
			"source":  pkg.Source,
		}
		if err := p.storeAttestation("Cargo.lock", "locks", pkg.Name, attrs); err != nil {
			return attestations, err
		}
	}

	return attestations, nil
}

// PackageJSON represents a minimal package.json structure
type PackageJSON struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// processPackageJSON parses package.json and generates attestations
func (p *DepsIxProcessor) processPackageJSON(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read package.json: %w", err)
	}

	var pkg PackageJSON
	if err := json.Unmarshal(content, &pkg); err != nil {
		return nil, fmt.Errorf("failed to parse package.json: %w", err)
	}

	var attestations []string

	// Package declaration
	if pkg.Name != "" {
		attestation := fmt.Sprintf("package.json declares_package %s", pkg.Name)
		attestations = append(attestations, attestation)
		attrs := map[string]interface{}{"version": pkg.Version}
		if err := p.storeAttestation("package.json", "declares_package", pkg.Name, attrs); err != nil {
			return attestations, err
		}
	}

	// Dependencies
	for dep, version := range pkg.Dependencies {
		attestation := fmt.Sprintf("package.json depends_on %s version %s", dep, version)
		attestations = append(attestations, attestation)
		attrs := map[string]interface{}{"version": version, "dev": false}
		if err := p.storeAttestation("package.json", "depends_on", dep, attrs); err != nil {
			return attestations, err
		}
	}

	// Dev dependencies
	for dep, version := range pkg.DevDependencies {
		attestation := fmt.Sprintf("package.json dev_depends_on %s version %s", dep, version)
		attestations = append(attestations, attestation)
		attrs := map[string]interface{}{"version": version, "dev": true}
		if err := p.storeAttestation("package.json", "dev_depends_on", dep, attrs); err != nil {
			return attestations, err
		}
	}

	return attestations, nil
}

// processFlakeNix parses flake.nix and extracts inputs
func (p *DepsIxProcessor) processFlakeNix(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read flake.nix: %w", err)
	}

	var attestations []string

	// Extract inputs using regex (Nix is not easily parseable)
	// Look for patterns like: nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
	inputPattern := regexp.MustCompile(`(\w+)\.url\s*=\s*"([^"]+)"`)
	matches := inputPattern.FindAllStringSubmatch(string(content), -1)

	for _, match := range matches {
		if len(match) >= 3 {
			inputName := match[1]
			inputURL := match[2]
			attestation := fmt.Sprintf("flake.nix has_input %s from %s", inputName, inputURL)
			attestations = append(attestations, attestation)
			attrs := map[string]interface{}{"url": inputURL}
			if err := p.storeAttestation("flake.nix", "has_input", inputName, attrs); err != nil {
				return attestations, err
			}
		}
	}

	// Also look for simple input declarations: inputs.foo.url = "...";
	simplePattern := regexp.MustCompile(`inputs\.(\w+)\.url\s*=\s*"([^"]+)"`)
	matches = simplePattern.FindAllStringSubmatch(string(content), -1)

	for _, match := range matches {
		if len(match) >= 3 {
			inputName := match[1]
			inputURL := match[2]
			attestation := fmt.Sprintf("flake.nix has_input %s from %s", inputName, inputURL)
			attestations = append(attestations, attestation)
			attrs := map[string]interface{}{"url": inputURL}
			if err := p.storeAttestation("flake.nix", "has_input", inputName, attrs); err != nil {
				return attestations, err
			}
		}
	}

	return attestations, nil
}

// FlakeLock represents a minimal flake.lock structure
type FlakeLock struct {
	Nodes map[string]struct {
		Locked struct {
			Owner string `json:"owner"`
			Repo  string `json:"repo"`
			Rev   string `json:"rev"`
			Type  string `json:"type"`
		} `json:"locked"`
		Original struct {
			Owner string `json:"owner"`
			Repo  string `json:"repo"`
			Type  string `json:"type"`
		} `json:"original"`
	} `json:"nodes"`
}

// processFlakeLock parses flake.lock and generates attestations
func (p *DepsIxProcessor) processFlakeLock(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read flake.lock: %w", err)
	}

	var lock FlakeLock
	if err := json.Unmarshal(content, &lock); err != nil {
		return nil, fmt.Errorf("failed to parse flake.lock: %w", err)
	}

	var attestations []string

	for name, node := range lock.Nodes {
		if name == "root" {
			continue // Skip root node
		}

		if node.Locked.Rev != "" {
			source := fmt.Sprintf("%s/%s", node.Locked.Owner, node.Locked.Repo)
			attestation := fmt.Sprintf("flake.lock locks %s at %s", name, node.Locked.Rev[:7])
			attestations = append(attestations, attestation)
			attrs := map[string]interface{}{
				"rev":    node.Locked.Rev,
				"owner":  node.Locked.Owner,
				"repo":   node.Locked.Repo,
				"type":   node.Locked.Type,
				"source": source,
			}
			if err := p.storeAttestation("flake.lock", "locks", name, attrs); err != nil {
				return attestations, err
			}
		}
	}

	return attestations, nil
}

// PyprojectToml represents a minimal pyproject.toml structure
type PyprojectToml struct {
	Project struct {
		Name         string   `toml:"name"`
		Version      string   `toml:"version"`
		Dependencies []string `toml:"dependencies"`
	} `toml:"project"`
	Tool struct {
		Poetry struct {
			Name         string                 `toml:"name"`
			Version      string                 `toml:"version"`
			Dependencies map[string]interface{} `toml:"dependencies"`
		} `toml:"poetry"`
	} `toml:"tool"`
}

// processPyprojectToml parses pyproject.toml and generates attestations
func (p *DepsIxProcessor) processPyprojectToml(path string) ([]string, error) {
	var pyproject PyprojectToml
	if _, err := toml.DecodeFile(path, &pyproject); err != nil {
		return nil, fmt.Errorf("failed to parse pyproject.toml: %w", err)
	}

	var attestations []string

	// Check project section (PEP 621)
	if pyproject.Project.Name != "" {
		attestation := fmt.Sprintf("pyproject.toml declares_package %s", pyproject.Project.Name)
		attestations = append(attestations, attestation)
		attrs := map[string]interface{}{"version": pyproject.Project.Version}
		if err := p.storeAttestation("pyproject.toml", "declares_package", pyproject.Project.Name, attrs); err != nil {
			return attestations, err
		}

		for _, dep := range pyproject.Project.Dependencies {
			// Parse dependency string (e.g., "requests>=2.28.0")
			depName, version := parsePythonDep(dep)
			attestation := fmt.Sprintf("pyproject.toml depends_on %s version %s", depName, version)
			attestations = append(attestations, attestation)
			attrs := map[string]interface{}{"version": version}
			if err := p.storeAttestation("pyproject.toml", "depends_on", depName, attrs); err != nil {
				return attestations, err
			}
		}
	}

	// Check Poetry section
	if pyproject.Tool.Poetry.Name != "" {
		attestation := fmt.Sprintf("pyproject.toml declares_package %s", pyproject.Tool.Poetry.Name)
		attestations = append(attestations, attestation)
		attrs := map[string]interface{}{"version": pyproject.Tool.Poetry.Version}
		if err := p.storeAttestation("pyproject.toml", "declares_package", pyproject.Tool.Poetry.Name, attrs); err != nil {
			return attestations, err
		}

		for dep, val := range pyproject.Tool.Poetry.Dependencies {
			if dep == "python" {
				continue // Skip python version requirement
			}
			version := extractPoetryVersion(val)
			attestation := fmt.Sprintf("pyproject.toml depends_on %s version %s", dep, version)
			attestations = append(attestations, attestation)
			attrs := map[string]interface{}{"version": version}
			if err := p.storeAttestation("pyproject.toml", "depends_on", dep, attrs); err != nil {
				return attestations, err
			}
		}
	}

	return attestations, nil
}

// parsePythonDep parses a Python dependency string like "requests>=2.28.0"
func parsePythonDep(dep string) (name, version string) {
	// Common version specifiers: ==, >=, <=, ~=, !=, <, >
	specifiers := []string{"==", ">=", "<=", "~=", "!=", "<", ">"}
	for _, spec := range specifiers {
		if idx := strings.Index(dep, spec); idx != -1 {
			return strings.TrimSpace(dep[:idx]), strings.TrimSpace(dep[idx:])
		}
	}
	// Handle extras like "package[extra]"
	if idx := strings.Index(dep, "["); idx != -1 {
		return strings.TrimSpace(dep[:idx]), "*"
	}
	return strings.TrimSpace(dep), "*"
}

// extractPoetryVersion extracts version from Poetry dependency value
func extractPoetryVersion(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case map[string]interface{}:
		if ver, ok := v["version"].(string); ok {
			return ver
		}
		if git, ok := v["git"].(string); ok {
			return "git:" + git
		}
		if path, ok := v["path"].(string); ok {
			return "path:" + path
		}
	}
	return "*"
}

// processRequirementsTxt parses requirements.txt and generates attestations
func (p *DepsIxProcessor) processRequirementsTxt(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read requirements.txt: %w", err)
	}
	defer file.Close()

	var attestations []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}

		// Parse dependency
		depName, version := parsePythonDep(line)
		if depName != "" {
			attestation := fmt.Sprintf("requirements.txt requires %s version %s", depName, version)
			attestations = append(attestations, attestation)
			attrs := map[string]interface{}{"version": version}
			if err := p.storeAttestation("requirements.txt", "requires", depName, attrs); err != nil {
				return attestations, err
			}
		}
	}

	return attestations, scanner.Err()
}

// processGenericProjectFile creates a simple presence attestation for unhandled file types
func (p *DepsIxProcessor) processGenericProjectFile(file ProjectFile) ([]string, error) {
	attestation := fmt.Sprintf("repository has_project_file %s", file.Type)
	if err := p.storeAttestation("repository", "has_project_file", file.Type, nil); err != nil {
		return []string{attestation}, err
	}
	return []string{attestation}, nil
}

// storeAttestation stores a dependency attestation
func (p *DepsIxProcessor) storeAttestation(subject, predicate, object string, attrs map[string]interface{}) error {
	if p.dryRun {
		return nil
	}

	// Generate ASID
	asid, err := id.GenerateASIDWithVanity(subject, predicate, object, p.actor)
	if err != nil {
		return fmt.Errorf("failed to generate ASID: %w", err)
	}

	// Create attestation
	attestation := &atstypes.As{
		ID:         asid,
		Subjects:   []string{subject},
		Predicates: []string{predicate},
		Contexts:   []string{object},
		Actors:     []string{p.actor},
		Timestamp:  time.Now(),
		Source:     "ixgest-deps",
		Attributes: attrs,
	}

	// Store in database
	if err := p.store.CreateAttestation(attestation); err != nil {
		return fmt.Errorf("failed to store attestation: %w", err)
	}

	return nil
}
