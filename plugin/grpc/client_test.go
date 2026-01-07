package grpc

import (
	"strings"
	"testing"
)

func TestPathStripping(t *testing.T) {
	tests := []struct {
		name       string
		pluginName string
		inputPath  string
		wantPath   string
	}{
		{
			name:       "strips namespace prefix",
			pluginName: "python",
			inputPath:  "/api/python/execute",
			wantPath:   "/execute",
		},
		{
			name:       "handles nested paths",
			pluginName: "python",
			inputPath:  "/api/python/pip/install",
			wantPath:   "/pip/install",
		},
		{
			name:       "handles exact namespace match",
			pluginName: "python",
			inputPath:  "/api/python",
			wantPath:   "/",
		},
		{
			name:       "preserves leading slash",
			pluginName: "code",
			inputPath:  "/api/code/completions",
			wantPath:   "/completions",
		},
		{
			name:       "handles root path",
			pluginName: "python",
			inputPath:  "/api/python/",
			wantPath:   "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This mirrors the logic in client.go:proxyHTTPRequest
			namespace := "/api/" + tt.pluginName + "/"
			path := strings.TrimPrefix(tt.inputPath, namespace)

			// Ensure path starts with / (TrimPrefix removes it)
			if path != "" && !strings.HasPrefix(path, "/") {
				path = "/" + path
			}

			// Handle exact namespace match (e.g., /api/python -> /)
			if path == "" || path == tt.inputPath {
				path = "/"
			}

			if path != tt.wantPath {
				t.Errorf("got %q, want %q", path, tt.wantPath)
			}
		})
	}
}
