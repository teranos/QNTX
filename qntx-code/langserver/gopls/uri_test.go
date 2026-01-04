package gopls

import (
	"path/filepath"
	"testing"
)

func TestFileToURI(t *testing.T) {
	tests := []struct {
		name          string
		workspaceRoot string
		file          string
		want          string
	}{
		{
			name:          "simple file in root",
			workspaceRoot: "/home/user/project",
			file:          "main.go",
			want:          "file:///home/user/project/main.go",
		},
		{
			name:          "nested file",
			workspaceRoot: "/home/user/project",
			file:          "cmd/app/main.go",
			want:          "file:///home/user/project/cmd/app/main.go",
		},
		{
			name:          "deeply nested file",
			workspaceRoot: "/home/user/project",
			file:          "internal/server/handlers/http.go",
			want:          "file:///home/user/project/internal/server/handlers/http.go",
		},
		{
			name:          "workspace with spaces",
			workspaceRoot: "/home/user/my project",
			file:          "main.go",
			want:          "file:///home/user/my project/main.go",
		},
		{
			name:          "file with spaces",
			workspaceRoot: "/home/user/project",
			file:          "my file.go",
			want:          "file:///home/user/project/my file.go",
		},
		{
			name:          "empty file path",
			workspaceRoot: "/home/user/project",
			file:          "",
			want:          "file:///home/user/project",
		},
		{
			name:          "windows-style workspace",
			workspaceRoot: "C:/Users/developer/project",
			file:          "main.go",
			want:          "file://C:/Users/developer/project/main.go",
		},
		{
			name:          "special characters in filename",
			workspaceRoot: "/home/user/project",
			file:          "test-file_v2.go",
			want:          "file:///home/user/project/test-file_v2.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &MCPServer{
				workspaceRoot: tt.workspaceRoot,
			}
			got := server.fileToURI(tt.file)
			if got != tt.want {
				t.Errorf("fileToURI() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestURIToFile(t *testing.T) {
	tests := []struct {
		name          string
		workspaceRoot string
		uri           string
		want          string
	}{
		{
			name:          "file URI in workspace root",
			workspaceRoot: "/home/user/project",
			uri:           "file:///home/user/project/main.go",
			want:          "main.go",
		},
		{
			name:          "nested file URI",
			workspaceRoot: "/home/user/project",
			uri:           "file:///home/user/project/cmd/app/main.go",
			want:          "cmd/app/main.go",
		},
		{
			name:          "deeply nested file URI",
			workspaceRoot: "/home/user/project",
			uri:           "file:///home/user/project/internal/server/handlers/http.go",
			want:          "internal/server/handlers/http.go",
		},
		{
			name:          "file outside workspace",
			workspaceRoot: "/home/user/project",
			uri:           "file:///other/path/file.go",
			want:          filepath.Join("..", "..", "..", "other", "path", "file.go"),
		},
		{
			name:          "non-file URI (http)",
			workspaceRoot: "/home/user/project",
			uri:           "http://example.com/file.go",
			want:          "http://example.com/file.go",
		},
		{
			name:          "non-file URI (https)",
			workspaceRoot: "/home/user/project",
			uri:           "https://example.com/path/to/file.go",
			want:          "https://example.com/path/to/file.go",
		},
		{
			name:          "malformed URI without prefix",
			workspaceRoot: "/home/user/project",
			uri:           "/absolute/path/file.go",
			want:          "/absolute/path/file.go",
		},
		{
			name:          "workspace with spaces",
			workspaceRoot: "/home/user/my project",
			uri:           "file:///home/user/my project/main.go",
			want:          "main.go",
		},
		{
			name:          "file with spaces",
			workspaceRoot: "/home/user/project",
			uri:           "file:///home/user/project/my file.go",
			want:          "my file.go",
		},
		{
			name:          "empty URI",
			workspaceRoot: "/home/user/project",
			uri:           "",
			want:          "",
		},
		{
			name:          "file URI equals workspace root",
			workspaceRoot: "/home/user/project",
			uri:           "file:///home/user/project",
			want:          ".",
		},
		{
			name:          "windows-style file URI",
			workspaceRoot: "C:/Users/developer/project",
			uri:           "file://C:/Users/developer/project/main.go",
			want:          "main.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &MCPServer{
				workspaceRoot: tt.workspaceRoot,
			}
			got := server.uriToFile(tt.uri)
			if got != tt.want {
				t.Errorf("uriToFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestURIRoundTrip verifies that converting file->URI->file produces the original file
func TestURIRoundTrip(t *testing.T) {
	tests := []struct {
		name          string
		workspaceRoot string
		file          string
	}{
		{
			name:          "simple file",
			workspaceRoot: "/home/user/project",
			file:          "main.go",
		},
		{
			name:          "nested file",
			workspaceRoot: "/home/user/project",
			file:          "cmd/app/main.go",
		},
		{
			name:          "deeply nested",
			workspaceRoot: "/home/user/project",
			file:          "internal/server/handlers/http.go",
		},
		{
			name:          "with spaces",
			workspaceRoot: "/home/user/my project",
			file:          "my file.go",
		},
		{
			name:          "special chars",
			workspaceRoot: "/home/user/project",
			file:          "test-file_v2.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &MCPServer{
				workspaceRoot: tt.workspaceRoot,
			}
			uri := server.fileToURI(tt.file)
			got := server.uriToFile(uri)
			if got != tt.file {
				t.Errorf("round trip failed: original=%v, uri=%v, back=%v", tt.file, uri, got)
			}
		})
	}
}
