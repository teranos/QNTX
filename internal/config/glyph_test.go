package config

import (
	"strings"
	"testing"
)

// A config that reaches Validate with nothing to say about anything but glyphs.
// Storage is set because Validate refuses an unknown backend before it gets
// this far, and that is a different test's subject.
func withGlyphs(glyphs []GlyphSource, enabled []string) *Config {
	cfg := &Config{Glyph: glyphs}
	cfg.Storage.Backend = "sqlite"
	cfg.Storage.Sqlite.BoundedStorage = BoundedStorageConfig{
		ActorContextLimit:  32,
		ActorContextsLimit: 64,
		EntityActorsLimit:  64,
	}
	cfg.Plugin.Enabled = enabled
	return cfg
}

func TestGlyphValidation(t *testing.T) {
	tests := []struct {
		name    string
		glyphs  []GlyphSource
		enabled []string
		// The substring the message must carry, or empty when it must pass.
		wantErr string
	}{
		{
			name:   "a declared glyph with a name and a module passes",
			glyphs: []GlyphSource{{Name: "chart", Module: "/srv/glyphs/chart.js"}},
		},
		{
			name:   "no glyphs at all passes",
			glyphs: nil,
		},
		{
			name:    "a glyph without a name has no route",
			glyphs:  []GlyphSource{{Module: "/srv/glyphs/chart.js"}},
			wantErr: "has no name",
		},
		{
			name:    "a glyph without a module has nothing to import",
			glyphs:  []GlyphSource{{Name: "chart"}},
			wantErr: "names no module",
		},
		{
			name:    "a separator in the name would put the module out of reach",
			glyphs:  []GlyphSource{{Name: "a/chart", Module: "/srv/glyphs/chart.js"}},
			wantErr: "one path segment",
		},
		{
			name:    "a relative module is read against whatever started the node",
			glyphs:  []GlyphSource{{Name: "chart", Module: "../glyphs/chart.js"}},
			wantErr: "absolute module path",
		},
		{
			name: "one name is one route",
			glyphs: []GlyphSource{
				{Name: "chart", Module: "/srv/glyphs/chart.js"},
				{Name: "chart", Module: "/srv/glyphs/other.js"},
			},
			wantErr: "declared twice",
		},
		{
			name:    "a glyph cannot take a name a plugin already answers on",
			glyphs:  []GlyphSource{{Name: "chart", Module: "/srv/glyphs/chart.js"}},
			enabled: []string{"chart"},
			wantErr: "also a plugin",
		},
		{
			name:    "the plugin it collides with may be named by its repo",
			glyphs:  []GlyphSource{{Name: "duif", Module: "/srv/glyphs/duif.js"}},
			enabled: []string{"https://github.com/teranos/duif"},
			wantErr: "also a plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := withGlyphs(tt.glyphs, tt.enabled).Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want no error", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error naming %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() = %q, want it to name %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestGlyphNames(t *testing.T) {
	cfg := &Config{Glyph: []GlyphSource{
		{Name: "chart", Module: "/srv/glyphs/chart.js"},
		{Name: "table", Module: "/srv/glyphs/table.js"},
	}}

	got := cfg.GlyphNames()
	want := []string{"chart", "table"}
	if len(got) != len(want) {
		t.Fatalf("GlyphNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GlyphNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
