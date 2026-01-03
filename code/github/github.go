package github

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pterm/pterm"
)

// FetchFixSuggestions fetches fix suggestions from PR comments using gh CLI
func FetchFixSuggestions(prNumber int) ([]FixSuggestion, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNumber), "--json", "comments", "--jq", ".comments[].body")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh command failed: %s", string(exitErr.Stderr))
		}
		return nil, err
	}

	// Parse the output to find qntx-fix blocks
	return ParseFixSuggestionsFromComments(string(output))
}

// ParseFixSuggestionsFromComments extracts fix suggestions from PR comment bodies
// Only uses the LAST qntx-fix block found (most recent review)
func ParseFixSuggestionsFromComments(comments string) ([]FixSuggestion, error) {
	// Find all qntx-fix code blocks
	re := regexp.MustCompile("(?s)```qntx-fix\\s*\\n(.*?)\\n```")
	matches := re.FindAllStringSubmatch(comments, -1)

	if len(matches) == 0 {
		return nil, nil
	}

	// Only use the LAST match (most recent review)
	lastMatch := matches[len(matches)-1]
	if len(lastMatch) < 2 {
		return nil, nil
	}

	jsonContent := lastMatch[1]
	var suggestions []FixSuggestion
	if err := json.Unmarshal([]byte(jsonContent), &suggestions); err != nil {
		return nil, fmt.Errorf("failed to parse fix suggestions JSON: %w", err)
	}

	return suggestions, nil
}

// CheckReviewStaleness checks if code was modified after the review was posted
// Returns detailed staleness information across file, package, and codebase levels
func CheckReviewStaleness(prNumber int, filePath string) (*StalenessInfo, error) {
	info := &StalenessInfo{}

	// Get the timestamp of the most recent qntx-fix comment
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNumber),
		"--json", "comments",
		"--jq", `.comments[] | select(.body | contains("qntx-fix")) | .createdAt | select(. != null)`)

	output, err := cmd.Output()
	if err != nil {
		return info, fmt.Errorf("failed to get comment timestamps: %w", err)
	}

	timestamps := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(timestamps) == 0 {
		return info, nil // No review comments found
	}

	// Get the last (most recent) review timestamp
	info.ReviewTime = timestamps[len(timestamps)-1]
	if info.ReviewTime == "" {
		return info, nil
	}

	// Get the last commit time that modified the specific file
	cmd = exec.Command("git", "log", "-1", "--format=%aI", "--", filePath)
	output, err = cmd.Output()
	if err != nil {
		return info, fmt.Errorf("failed to get file modification time: %w", err)
	}

	info.FileModTime = strings.TrimSpace(string(output))
	if info.FileModTime == "" {
		// File has never been committed, not stale
		return info, nil
	}

	// Compare timestamps (both are ISO 8601 format, can compare as strings)
	info.IsStale = info.FileModTime > info.ReviewTime

	// Count commits at different scopes since the review
	if info.IsStale {
		// 1. Count commits to this specific file
		cmd = exec.Command("git", "log", "--oneline", "--since="+info.ReviewTime, "--", filePath)
		output, err = cmd.Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) > 0 && lines[0] != "" {
				info.FileCommits = len(lines)
			}
		}

		// 2. Count commits to the same package (directory)
		// Extract filename from path
		parts := strings.Split(filePath, "/")
		fileName := filePath
		if len(parts) > 0 {
			fileName = parts[len(parts)-1]
		}
		packagePath := strings.TrimSuffix(filePath, "/"+fileName)
		if packagePath != "" {
			cmd = exec.Command("git", "log", "--oneline", "--since="+info.ReviewTime, "--", packagePath)
			output, err = cmd.Output()
			if err == nil {
				lines := strings.Split(strings.TrimSpace(string(output)), "\n")
				if len(lines) > 0 && lines[0] != "" {
					info.PackageCommits = len(lines)
				}
			}
		}

		// 3. Count total commits since review
		cmd = exec.Command("git", "log", "--oneline", "--since="+info.ReviewTime)
		output, err = cmd.Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			totalCommits := 0
			if len(lines) > 0 && lines[0] != "" {
				totalCommits = len(lines)
			}
			// Commits elsewhere = total - package commits
			info.ElsewhereCommits = totalCommits - info.PackageCommits
		}
	}

	return info, nil
}

// GetPRLatestCommitTime gets the timestamp of the latest commit in a PR
func GetPRLatestCommitTime(prNumber int) (time.Time, error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNumber), "--json", "commits")
	output, err := cmd.Output()
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get PR commits: %w", err)
	}

	var prData struct {
		Commits []struct {
			CommittedDate string `json:"committedDate"`
		} `json:"commits"`
	}

	if err := json.Unmarshal(output, &prData); err != nil {
		return time.Time{}, fmt.Errorf("failed to parse PR commits: %w", err)
	}

	if len(prData.Commits) == 0 {
		return time.Time{}, fmt.Errorf("PR has no commits")
	}

	// Get the latest commit (last in array)
	latestCommitStr := prData.Commits[len(prData.Commits)-1].CommittedDate
	latestCommit, err := time.Parse(time.RFC3339, latestCommitStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse commit time: %w", err)
	}

	return latestCommit, nil
}

// GetLatestWorkflowRunID gets the database ID of the most recent workflow run
func GetLatestWorkflowRunID(workflowFile string) (int, error) {
	listCmd := exec.Command("gh", "run", "list",
		"--workflow", workflowFile,
		"--limit", "1",
		"--json", "databaseId")

	output, err := listCmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to list workflow runs: %w", err)
	}

	var runs []map[string]interface{}
	if err := json.Unmarshal(output, &runs); err != nil {
		return 0, fmt.Errorf("failed to parse workflow runs: %w", err)
	}

	if len(runs) == 0 {
		return 0, fmt.Errorf("no workflow runs found")
	}

	runID := int(runs[0]["databaseId"].(float64))
	return runID, nil
}

// MonitorWorkflowCompletion polls workflow status until completion
func MonitorWorkflowCompletion(runID int) error {
	startTime := time.Now()
	spinner, _ := pterm.DefaultSpinner.Start("Waiting for workflow to complete...")

	for {
		// Check workflow status
		statusCmd := exec.Command("gh", "run", "view", strconv.Itoa(runID), "--json", "status,conclusion")
		output, err := statusCmd.Output()
		if err != nil {
			spinner.Fail("Failed to check workflow status")
			return fmt.Errorf("failed to check status: %w", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(output, &result); err != nil {
			spinner.Fail("Failed to parse workflow status")
			return fmt.Errorf("failed to parse status: %w", err)
		}

		status := result["status"].(string)
		elapsed := time.Since(startTime).Seconds()

		if status == "completed" {
			conclusion := result["conclusion"].(string)
			spinner.Success(fmt.Sprintf("Workflow completed in %.0fs", elapsed))

			if conclusion != "success" {
				pterm.Warning.Printf("⚠ Workflow concluded with status: %s\n", conclusion)
				pterm.Info.Printf("View details: gh run view %d\n", runID)
			}
			return nil
		}

		// Update spinner with elapsed time
		spinner.UpdateText(fmt.Sprintf("Waiting for workflow to complete... (%.0fs elapsed)", elapsed))

		// Wait before next poll
		time.Sleep(5 * time.Second)
	}
}

// CheckWorkflowStatus checks the status of recent workflow runs
func CheckWorkflowStatus(workflowFile string) error {
	pterm.Info.Printf("Checking status of Claude Code review workflows...\n\n")

	// List recent workflow runs
	listCmd := exec.Command("gh", "run", "list",
		"--workflow", workflowFile,
		"--limit", "5",
		"--json", "databaseId,status,conclusion,createdAt,headBranch,event,displayTitle")

	output, err := listCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("failed to check workflow status: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("failed to check workflow status: %w", err)
	}

	// Parse and display runs
	var runs []map[string]interface{}
	if err := json.Unmarshal(output, &runs); err != nil {
		return fmt.Errorf("failed to parse workflow runs: %w", err)
	}

	if len(runs) == 0 {
		pterm.Warning.Println("No recent workflow runs found")
		return nil
	}

	// Display runs in a table
	pterm.Printf("Recent Claude Code Review runs:\n\n")
	for i, run := range runs {
		status := run["status"].(string)
		conclusion := ""
		if run["conclusion"] != nil {
			conclusion = run["conclusion"].(string)
		}
		title := run["displayTitle"].(string)
		runID := run["databaseId"].(float64)

		// Color code by status
		statusStr := fmt.Sprintf("%-12s", status)
		switch status {
		case "completed":
			if conclusion == "success" {
				statusStr = pterm.Green(statusStr)
			} else if conclusion == "failure" {
				statusStr = pterm.Red(statusStr)
			} else {
				statusStr = pterm.Yellow(statusStr)
			}
		case "in_progress":
			statusStr = pterm.Cyan(statusStr)
		default:
			statusStr = pterm.Gray(statusStr)
		}

		pterm.Printf("%d. %s %s\n", i+1, statusStr, title)
		pterm.Printf("   Run ID: %.0f\n", runID)

		if i == 0 && status != "completed" {
			pterm.Info.Printf("   Watch: gh run watch %.0f\n", runID)
		}
	}

	return nil
}
