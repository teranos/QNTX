package parity

import (
	"embed"
	"encoding/json"
	"io/fs"
	"strings"

	"github.com/teranos/errors"
)

// manifest.json is Anthropic's own for the pinned version: the one place the
// sha256 of its binaries is written.
//
//go:embed claudecode_*/manifest.json
var claudeCodePin embed.FS

// ClaudeCode is the Claude Code pinned here: its version, and the sha256 of
// its binary on each platform as the manifest gives them.
func ClaudeCode() (version string, checksums map[string]string, err error) {
	entries, err := fs.ReadDir(claudeCodePin, ".")
	if err != nil {
		return "", nil, errors.Wrap(err, "the Claude Code pin did not read")
	}
	if len(entries) != 1 {
		return "", nil, errors.Newf("Claude Code is pinned %d times, and a build runs one", len(entries))
	}
	dir := entries[0].Name()
	raw, err := claudeCodePin.ReadFile(dir + "/manifest.json")
	if err != nil {
		return "", nil, errors.Wrapf(err, "%s/manifest.json did not read", dir)
	}
	var manifest struct {
		Version   string `json:"version"`
		Commit    string `json:"commit"`
		Platforms map[string]struct {
			Checksum string `json:"checksum"`
		} `json:"platforms"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return "", nil, errors.Wrapf(err, "%s/manifest.json is not a manifest", dir)
	}
	// The directory says which version and commit, and the manifest is held to it.
	rev, named := strings.CutPrefix(dir, "claudecode_"+manifest.Version+"_")
	if !named {
		return "", nil, errors.Newf("%s holds the manifest of %s", dir, manifest.Version)
	}
	if rev == "" || !strings.HasPrefix(manifest.Commit, rev) {
		return "", nil, errors.Newf("%s holds the manifest of commit %s", dir, manifest.Commit)
	}
	checksums = map[string]string{}
	for platform, binary := range manifest.Platforms {
		checksums[platform] = binary.Checksum
	}
	return manifest.Version, checksums, nil
}
