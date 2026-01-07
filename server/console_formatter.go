package server

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ConsoleFormatter formats browser console logs with JSON summarization
type ConsoleFormatter struct {
	verbosity int
}

// NewConsoleFormatter creates a new console formatter with verbosity-aware output
func NewConsoleFormatter(verbosity int) *ConsoleFormatter {
	return &ConsoleFormatter{
		verbosity: verbosity,
	}
}

// FormatMessage formats just the message content (for use with zap logging)
func (cf *ConsoleFormatter) FormatMessage(message string) string {
	return cf.formatMessage(message)
}

// formatMessage formats the log message with JSON detection and summarization
func (cf *ConsoleFormatter) formatMessage(msg string) string {
	// Detect if this is a WebSocket message
	if strings.Contains(msg, "WS message:") {
		parts := strings.SplitN(msg, "WS message:", 2)
		if len(parts) == 2 {
			jsonPart := strings.TrimSpace(parts[1])

			// Try to parse and summarize JSON
			summary := cf.summarizeJSON(jsonPart)
			return fmt.Sprintf("📨 WS message: %s", summary)
		}
	}

	// Default: return message as-is
	return msg
}

// summarizeJSON summarizes JSON payloads based on verbosity level
func (cf *ConsoleFormatter) summarizeJSON(jsonStr string) string {
	// Try to parse JSON first
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		// Not valid JSON - just return as-is
		return jsonStr
	}

	// Verbosity-based formatting
	switch {
	case cf.verbosity <= 1:
		// Level 0-1: Compact summary only
		return cf.compactSummary(data, len(jsonStr))
	case cf.verbosity == 2:
		// Level 2: Detailed summary with key names
		return cf.detailedSummary(data, len(jsonStr))
	default:
		// Level 3+: Full JSON, no truncation
		return jsonStr
	}
}

// compactSummary creates a very compact summary (level 0-1)
func (cf *ConsoleFormatter) compactSummary(data map[string]interface{}, totalBytes int) string {
	summary := "{"
	parts := []string{}

	for key, val := range data {
		switch v := val.(type) {
		case []interface{}:
			parts = append(parts, fmt.Sprintf("%s: [%d]", key, len(v)))
		case map[string]interface{}:
			parts = append(parts, fmt.Sprintf("%s: {...}", key))
		default:
			// Skip primitives in compact mode
		}
	}

	summary += strings.Join(parts, ", ")
	summary += "}"

	return fmt.Sprintf("%s (%s)", summary, formatBytes(totalBytes))
}

// detailedSummary creates a detailed summary showing all fields (level 2)
func (cf *ConsoleFormatter) detailedSummary(data map[string]interface{}, totalBytes int) string {
	parts := []string{}

	for key, val := range data {
		switch v := val.(type) {
		case []interface{}:
			parts = append(parts, fmt.Sprintf("%s: [%d items]", key, len(v)))
		case map[string]interface{}:
			// Count keys in nested object
			parts = append(parts, fmt.Sprintf("%s: {%d keys}", key, len(v)))
		case string:
			if len(v) > 30 {
				parts = append(parts, fmt.Sprintf("%s: \"%s...\"", key, v[:30]))
			} else {
				parts = append(parts, fmt.Sprintf("%s: \"%s\"", key, v))
			}
		case float64, int:
			parts = append(parts, fmt.Sprintf("%s: %v", key, v))
		}
	}

	return fmt.Sprintf("{%s} (%s)", strings.Join(parts, ", "), formatBytes(totalBytes))
}

// formatBytes formats byte count as human-readable string
func formatBytes(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
}
