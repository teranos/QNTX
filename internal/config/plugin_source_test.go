package config

import "testing"

func TestParseEnabledEntry(t *testing.T) {
	tests := []struct {
		name     string
		entry    string
		wantName string
		wantRepo string
	}{
		{
			name:     "bare name has no source",
			entry:    "duif",
			wantName: "duif",
		},
		{
			name:     "repo URL derives name from last segment",
			entry:    "https://github.com/sbvh-nl/duif",
			wantName: "duif",
			wantRepo: "https://github.com/sbvh-nl/duif",
		},
		{
			name:     "trailing slash does not become the name",
			entry:    "https://github.com/sbvh-nl/duif/",
			wantName: "duif",
			wantRepo: "https://github.com/sbvh-nl/duif/",
		},
		{
			name:     "git suffix is stripped",
			entry:    "https://github.com/sbvh-nl/duif.git",
			wantName: "duif",
			wantRepo: "https://github.com/sbvh-nl/duif.git",
		},
		{
			name:     "hyphenated name survives",
			entry:    "https://github.com/teranos/pty-glyph",
			wantName: "pty-glyph",
			wantRepo: "https://github.com/teranos/pty-glyph",
		},
		{
			name:     "surrounding space is not part of the name",
			entry:    "  duif  ",
			wantName: "duif",
		},
		{
			name:     "a host with no path is not a plugin name",
			entry:    "https://github.com",
			wantName: "github.com",
			wantRepo: "https://github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseEnabledEntry(tt.entry)
			if got.Name != tt.wantName {
				t.Errorf("ParseEnabledEntry(%q).Name = %q, want %q", tt.entry, got.Name, tt.wantName)
			}
			if got.Repo != tt.wantRepo {
				t.Errorf("ParseEnabledEntry(%q).Repo = %q, want %q", tt.entry, got.Repo, tt.wantRepo)
			}
		})
	}
}

func TestEnabledFormsMixFreely(t *testing.T) {
	cfg := PluginConfig{Enabled: []string{
		"meili",
		"https://github.com/sbvh-nl/duif",
		"spindle",
	}}

	names := cfg.EnabledNames()
	want := []string{"meili", "duif", "spindle"}
	if len(names) != len(want) {
		t.Fatalf("EnabledNames() = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("EnabledNames()[%d] = %q, want %q", i, names[i], want[i])
		}
	}

	if repo := cfg.RepoFor("duif"); repo != "https://github.com/sbvh-nl/duif" {
		t.Errorf("RepoFor(duif) = %q, want the repo URL", repo)
	}

	// A bare name must never produce a source — that is what keeps it off the network.
	if repo := cfg.RepoFor("meili"); repo != "" {
		t.Errorf("RepoFor(meili) = %q, want \"\" for a bare-name entry", repo)
	}

	if repo := cfg.RepoFor("absent"); repo != "" {
		t.Errorf("RepoFor(absent) = %q, want \"\"", repo)
	}
}
