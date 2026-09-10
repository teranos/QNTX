package config

import (
	"path/filepath"
	"strings"

	"github.com/teranos/errors"
)

// GlyphSource is one canvas glyph the node serves itself.
//
// A glyph reaches the canvas as a browser module: the frontend imports it,
// calls the render it exports, and reads what it is from the glyphDef beside
// it. None of that needs a process. Declaring one here is the whole of putting
// it on the canvas — no plugin binary, no gRPC, no release to fetch, and no
// restart to replace it, because the file is read when it is asked for.
//
// The name is the identity, the way a plugin's is: the registry key, the log
// field, and the path segment the module is served under. It shares that
// namespace with [plugin] enabled, so the two are checked against each other
// rather than racing for the same route.
type GlyphSource struct {
	Name   string `mapstructure:"name"`   // Identity: registry key, log field, and the {name} in /api/{name}/glyph-module.js
	Module string `mapstructure:"module"` // Path on disk to the ES module. Read when it is asked for, so replacing the file replaces what is served.
}

// CheckGlyphs refuses a declaration that cannot reach the canvas.
//
// Called where the glyphs are registered, not only from Validate: Load does not
// validate, so a rule stated only there runs when somebody types am validate.
func (c *Config) CheckGlyphs() error {
	seen := make(map[string]bool, len(c.Glyph))
	enabledPlugin := make(map[string]bool, len(c.Plugin.Enabled))
	for _, name := range c.Plugin.EnabledNames() {
		enabledPlugin[name] = true
	}

	for i, g := range c.Glyph {
		if g.Name == "" {
			return errors.Newf("glyph[%d] has no name, and the name is the path its module is served under", i)
		}
		// The name becomes one path segment. A separator in it would put the
		// module somewhere no route reaches.
		if strings.ContainsAny(g.Name, "/\\") {
			return errors.Newf("glyph %q must be one path segment — it is the {name} in /api/{name}/glyph-module.js", g.Name)
		}
		if g.Module == "" {
			return errors.Newf("glyph %q names no module, so there is nothing for the canvas to import", g.Name)
		}
		// A relative path is read against the node's working directory, which is
		// whatever started it, and the same line would then mean two files.
		if !filepath.IsAbs(g.Module) {
			return errors.Newf("glyph %q needs an absolute module path, got %q — a relative one is read against whatever directory the node was started in", g.Name, g.Module)
		}
		if seen[g.Name] {
			return errors.Newf("glyph %q is declared twice, and one name is one route", g.Name)
		}
		if enabledPlugin[g.Name] {
			return errors.Newf("glyph %q is also a plugin in [plugin] enabled, and both answer on /api/%s", g.Name, g.Name)
		}
		seen[g.Name] = true
	}
	return nil
}

// GlyphNames returns the declared glyph names, in the order they were declared.
func (c *Config) GlyphNames() []string {
	names := make([]string, 0, len(c.Glyph))
	for _, g := range c.Glyph {
		names = append(names, g.Name)
	}
	return names
}
