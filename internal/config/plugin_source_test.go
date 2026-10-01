package config

import "testing"

func TestPluginNameFromRepo(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		wantName string
	}{
		{
			name:     "repo URL derives name from last segment",
			repo:     "https://github.com/abcd-nl/duif",
			wantName: "duif",
		},
		{
			name:     "trailing slash does not become the name",
			repo:     "https://github.com/abcd-nl/duif/",
			wantName: "duif",
		},
		{
			name:     "git suffix is stripped",
			repo:     "https://github.com/abcd-nl/duif.git",
			wantName: "duif",
		},
		{
			name:     "hyphenated name survives",
			repo:     "https://github.com/teranos/pty-element",
			wantName: "pty-element",
		},
		{
			name:     "a host with no path is not a plugin name",
			repo:     "https://github.com",
			wantName: "github.com",
		},
		{
			name:     "a path inside a repo names the plugin",
			repo:     "https://github.com/teranos/QNTX/tree/main/qntx-plugins/loom",
			wantName: "loom",
		},
		{
			name:     "a ref with no path falls back to the repo",
			repo:     "https://github.com/teranos/pyre/tree/main/",
			wantName: "pyre",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PluginNameFromRepo(tt.repo); got != tt.wantName {
				t.Errorf("PluginNameFromRepo(%q) = %q, want %q", tt.repo, got, tt.wantName)
			}
		})
	}
}
