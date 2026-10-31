package parity

import (
	"embed"
	"encoding/json"
	"io/fs"
	"strings"

	errors "github.com/teranos/sacred-error"
)

//go:embed agy_*/manifest.json
var agyPin embed.FS

// Agy is the single pinned Antigravity CLI release and the SHA-256 of its
// upstream release archives per platform. These are archive hashes, not binary hashes.
func Agy() (version string, artifacts map[string]struct{ File, SHA256 string }, err error) {
	entries, err := fs.ReadDir(agyPin, ".")
	if err != nil {
		return "", nil, errors.Wrap(err, "the agy pin did not read")
	}
	if len(entries) != 1 {
		return "", nil, errors.Newf("agy is pinned %d times, and a build runs one", len(entries))
	}
	dir := entries[0].Name()
	raw, err := agyPin.ReadFile(dir + "/manifest.json")
	if err != nil {
		return "", nil, errors.Wrapf(err, "%s/manifest.json did not read", dir)
	}
	var pinned struct {
		Version   string `json:"version"`
		Commit    string `json:"commit"`
		Artifacts map[string]struct {
			File   string `json:"file"`
			SHA256 string `json:"sha256"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(raw, &pinned); err != nil {
		return "", nil, errors.Wrapf(err, "%s/manifest.json is not a manifest", dir)
	}
	rev, named := strings.CutPrefix(dir, "agy_"+pinned.Version+"_")
	if !named || rev == "" || !strings.HasPrefix(pinned.Commit, rev) {
		return "", nil, errors.Newf("%s does not name the pinned agy version and commit", dir)
	}
	artifacts = make(map[string]struct{ File, SHA256 string }, len(pinned.Artifacts))
	for platform, artifact := range pinned.Artifacts {
		artifacts[platform] = struct{ File, SHA256 string }{artifact.File, artifact.SHA256}
	}
	return pinned.Version, artifacts, nil
}
