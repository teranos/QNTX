package server

import (
	"testing"
)

func TestConsoleFormatter_FormatMessage(t *testing.T) {
	tests := []struct {
		name      string
		verbosity int
		message   string
	}{
		{
			name:      "simple info message",
			verbosity: 0,
			message:   "Page loaded successfully",
		},
		{
			name:      "WS message level 0",
			verbosity: 0,
			message:   `WS message: {"nodes":[{"id":"1"},{"id":"2"}]}`,
		},
		{
			name:      "WS message level 2",
			verbosity: 2,
			message:   `WS message: {"nodes":[{"id":"1"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatter := NewConsoleFormatter(tt.verbosity)
			output := formatter.FormatMessage(tt.message)

			// Basic check - output should not be empty
			if len(output) == 0 {
				t.Error("Output is empty")
			}
		})
	}
}

func TestConsoleFormatter_SummarizeJSON_Verbosity(t *testing.T) {
	largeJSON := `{"nodes":[{"id":"1"},{"id":"2"}],"links":[{"source":"1","target":"2"}]}`

	tests := []struct {
		name      string
		verbosity int
	}{
		{
			name:      "level 0",
			verbosity: 0,
		},
		{
			name:      "level 1",
			verbosity: 1,
		},
		{
			name:      "level 2",
			verbosity: 2,
		},
		{
			name:      "level 3",
			verbosity: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatter := NewConsoleFormatter(tt.verbosity)
			summary := formatter.summarizeJSON(largeJSON)

			// Check that summary exists
			if len(summary) == 0 {
				t.Error("Summary is empty")
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int
		want  string
	}{
		{100, "100B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatBytes(tt.bytes)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}
