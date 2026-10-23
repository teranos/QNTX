package parity

import (
	"embed"
	"encoding/json"
	"io/fs"
	"strings"

	errors "github.com/teranos/sacred-error"
)

//go:embed hermes_*/manifest.json
var hermesPin embed.FS

// Hermes names the one pinned Hermes Agent source revision.
// Hermes is distributed from source; this does not pin a built executable.
func Hermes() (version, commit string, err error) {
	entries, err := fs.ReadDir(hermesPin, ".")
	if err != nil {
		return "", "", errors.Wrap(err, "the Hermes pin did not read")
	}
	if len(entries) != 1 {
		return "", "", errors.Newf("Hermes is pinned %d times, and a build runs one", len(entries))
	}
	dir := entries[0].Name()
	raw, err := hermesPin.ReadFile(dir + "/manifest.json")
	if err != nil {
		return "", "", errors.Wrapf(err, "%s/manifest.json did not read", dir)
	}
	var pin struct {
		Version string `json:"version"`
		Tag string `json:"tag"`
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(raw, &pin); err != nil {
		return "", "", errors.Wrapf(err, "%s/manifest.json is not a manifest", dir)
	}
	rev, named := strings.CutPrefix(dir, "hermes_"+pin.Version+"_")
	if !named || len(rev) != 7 || pin.Tag != "v"+pin.Version || len(pin.Commit) != 40 || !strings.HasPrefix(pin.Commit, rev) {
		return "", "", errors.Newf("%s does not name the pinned Hermes version and commit", dir)
	}
	if strings.Trim(pin.Commit, "0123456789abcdef") != "" {
		return "", "", errors.Newf("%s has an invalid Hermes commit", dir)
	}
	return pin.Version, pin.Commit, nil
}
