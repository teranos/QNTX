package config

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

// GlyphNames returns the declared glyph names, in the order they were declared.
func (c *Config) GlyphNames() []string {
	names := make([]string, 0, len(c.Glyph))
	for _, g := range c.Glyph {
		names = append(names, g.Name)
	}
	return names
}
