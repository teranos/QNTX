package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LLMInfo contains information about the detected LLM environment
type LLMInfo struct {
	IsLLM    bool
	Tool     string
	ModelID  string
	Provider string
}

// IsLLMEnvironment returns true if running in a detected LLM environment
func IsLLMEnvironment() bool {
	// Check explicit LLM caller
	if os.Getenv("QNTX_CALLER") == "llm" {
		return true
	}

	// Check for known LLM tools
	return detectKnownLLMTools()
}

// GetLLMInfo returns detailed information about the LLM environment
func GetLLMInfo() LLMInfo {
	// Check explicit LLM caller first
	if os.Getenv("QNTX_CALLER") == "llm" {
		return LLMInfo{
			IsLLM:    true,
			Tool:     "generic-llm",
			ModelID:  os.Getenv("QNTX_LLM_MODEL"), // Allow explicit model specification
			Provider: os.Getenv("QNTX_LLM_PROVIDER"),
		}
	}

	// Detect specific LLM tools and their characteristics
	if os.Getenv("CLAUDECODE") != "" || os.Getenv("CLAUDE_CODE_ENTRYPOINT") != "" {
		return LLMInfo{
			IsLLM:    true,
			Tool:     "claude-code",
			ModelID:  detectClaudeModel(),
			Provider: "anthropic",
		}
	}

	if os.Getenv("CURSOR") != "" {
		return LLMInfo{
			IsLLM:    true,
			Tool:     "cursor",
			ModelID:  os.Getenv("CURSOR_MODEL"), // If Cursor exposes this
			Provider: "cursor-ai",
		}
	}

	if os.Getenv("GITHUB_COPILOT") != "" {
		return LLMInfo{
			IsLLM:    true,
			Tool:     "github-copilot",
			ModelID:  "copilot-model", // GitHub Copilot doesn't typically expose model details
			Provider: "github",
		}
	}

	return LLMInfo{IsLLM: false}
}

// detectKnownLLMTools checks for environment variables set by known LLM tools
func detectKnownLLMTools() bool {
	// Claude Code
	if os.Getenv("CLAUDECODE") != "" || os.Getenv("CLAUDE_CODE_ENTRYPOINT") != "" {
		return true
	}

	// Cursor
	if os.Getenv("CURSOR") != "" {
		return true
	}

	// GitHub Copilot (if it sets identifying vars)
	if os.Getenv("GITHUB_COPILOT") != "" {
		return true
	}

	return false
}

// detectClaudeModel attempts to detect the specific Claude model being used
func detectClaudeModel() string {
	// Check for explicit model environment variable
	if model := os.Getenv("CLAUDE_MODEL"); model != "" {
		return model
	}

	// Try to detect model from Claude session files
	if os.Getenv("CLAUDECODE") != "" {
		if model := detectModelFromClaudeSession(); model != "" {
			return model
		}

		// Fallback to Claude settings.json
		if model := detectModelFromClaudeSettings(); model != "" {
			return "claude-" + model + "-unknown" // e.g., "claude-sonnet-unknown"
		}

		// Last resort fallback
		return "claude-unknown"
	}

	return "claude-unknown"
}

// detectModelFromClaudeSession tries to extract model from current Claude session
func detectModelFromClaudeSession() string {
	// Get current working directory to find the right project
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Convert path to Claude project format (replace / with - and . with -)
	projectPath := strings.ReplaceAll(cwd, "/", "-")
	projectPath = strings.ReplaceAll(projectPath, ".", "-")

	// Look for Claude project directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	claudeProjectDir := filepath.Join(homeDir, ".claude", "projects", projectPath)

	// Find the most recent session file
	sessionFiles, err := filepath.Glob(filepath.Join(claudeProjectDir, "*.jsonl"))
	if err != nil || len(sessionFiles) == 0 {
		return ""
	}

	// Sort by modification time (most recent first)
	sort.Slice(sessionFiles, func(i, j int) bool {
		infoI, errI := os.Stat(sessionFiles[i])
		infoJ, errJ := os.Stat(sessionFiles[j])
		if errI != nil || errJ != nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	// Try to extract model from the most recent session file
	return extractModelFromSessionFile(sessionFiles[0])
}

// extractModelFromSessionFile parses a Claude session file to find the model
func extractModelFromSessionFile(filename string) string {
	file, err := os.Open(filename)
	if err != nil {
		return ""
	}
	defer file.Close()

	// Read the file and find the last line with model information
	content, err := os.ReadFile(filename)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(content), "\n")

	// Parse lines in reverse order to find the most recent model reference
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		var sessionEntry struct {
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}

		if err := json.Unmarshal([]byte(line), &sessionEntry); err == nil {
			if sessionEntry.Message.Model != "" {
				return sessionEntry.Message.Model
			}
		}
	}

	return ""
}

// detectModelFromClaudeSettings tries to get model family from Claude settings
func detectModelFromClaudeSettings() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	settingsFile := filepath.Join(homeDir, ".claude", "settings.json")
	content, err := os.ReadFile(settingsFile)
	if err != nil {
		return ""
	}

	var settings struct {
		Model string `json:"model"`
	}

	if err := json.Unmarshal(content, &settings); err == nil {
		return settings.Model // e.g., "sonnet"
	}

	return ""
}

// FormatLLMForActor returns a formatted string suitable for use as an actor in attestations
func (info LLMInfo) FormatLLMForActor() string {
	if !info.IsLLM {
		return ""
	}

	// Format: tool+model@provider
	if info.ModelID != "" && info.Provider != "" {
		return info.Tool + "+" + info.ModelID + "@" + info.Provider
	} else if info.Tool != "" {
		return info.Tool + "+unknown@unknown"
	}

	return "llm+unknown@unknown"
}

// ShouldDisableColor returns true if color should be disabled in LLM environments
func ShouldDisableColor() bool {
	// Check QNTX_CALLER=llm
	if os.Getenv("QNTX_CALLER") == "llm" {
		return true
	}

	// Check for known LLM tools
	return detectKnownLLMTools()
}
