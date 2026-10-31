package parity

import (
	"embed"
	"encoding/json"
	"io/fs"
	"strings"

	errors "github.com/teranos/sacred-error"
)

//go:embed dsh_*/manifest.json
var dshPin embed.FS

// Dsh is the single pinned DeepSeek Harness release: its npm tarball, one for
// every platform, by the SHA-256 of its bytes and the registry's integrity.
func Dsh() (version string, tarball struct{ File, SHA256, Integrity string }, err error) {
	entries, err := fs.ReadDir(dshPin, ".")
	if err != nil {
		return "", tarball, errors.Wrap(err, "the dsh pin did not read")
	}
	if len(entries) != 1 {
		return "", tarball, errors.Newf("dsh is pinned %d times, and a build runs one", len(entries))
	}
	dir := entries[0].Name()
	raw, err := dshPin.ReadFile(dir + "/manifest.json")
	if err != nil {
		return "", tarball, errors.Wrapf(err, "%s/manifest.json did not read", dir)
	}
	var pinned struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Tarball struct {
			File      string `json:"file"`
			SHA256    string `json:"sha256"`
			Integrity string `json:"integrity"`
		} `json:"tarball"`
	}
	if err := json.Unmarshal(raw, &pinned); err != nil {
		return "", tarball, errors.Wrapf(err, "%s/manifest.json is not a manifest", dir)
	}
	rev, named := strings.CutPrefix(dir, "dsh_"+pinned.Version+"_")
	if !named || rev == "" || !strings.HasPrefix(pinned.Commit, rev) {
		return "", tarball, errors.Newf("%s does not name the pinned dsh version and commit", dir)
	}
	tarball.File, tarball.SHA256, tarball.Integrity = pinned.Tarball.File, pinned.Tarball.SHA256, pinned.Tarball.Integrity
	return pinned.Version, tarball, nil
}
