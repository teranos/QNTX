// Package retired holds the ways QNTX no longer works, so none comes back.
//
// "sweep every repository and file for the old am.toml way of managing QNTX
// plugins, and point each at ADR-002"
package retired

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// skipped are directories that are built, fetched or vendored, not written.
var skipped = map[string]bool{
	".git": true, "target": true, "node_modules": true, "vendor": true,
	"bin": true, "tmp": true,
}

var (
	table   = regexp.MustCompile(`^\s*\[\[?\s*([^\]\s]+)\s*\]\]?\s*(#.*)?$`)
	enabled = regexp.MustCompile(`^\s*enabled\s*=`)
)

// A plugin is enabled in the plugin element, not in am.toml: ADR-002.
func TestNoFileEnablesAPluginInAmToml(t *testing.T) {
	root := repoRoot(t)

	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (skipped[d.Name()] || strings.HasPrefix(d.Name(), "result")) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".toml" && ext != ".md" && ext != ".go" && ext != ".nix" {
			return nil
		}
		found = append(found, enablesAPlugin(t, root, path)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	for _, f := range found {
		t.Errorf("%s enables a plugin in am.toml's [plugin] table; plugins are enabled in the plugin element, see docs/adr/ADR-002-plugin-configuration.md", f)
	}
}

// enablesAPlugin names each line of path that sets enabled inside [plugin].
func enablesAPlugin(t *testing.T, root, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()

	rel, _ := filepath.Rel(root, path)
	var found []string
	inPlugin := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if m := table.FindStringSubmatch(line); m != nil {
			inPlugin = m[1] == "plugin"
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inPlugin = false
			continue
		}
		if inPlugin && enabled.MatchString(line) {
			found = append(found, rel+":"+strconv.Itoa(n))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return found
}

// repoRoot is the directory holding go.mod, above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = parent
	}
}
