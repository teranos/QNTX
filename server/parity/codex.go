package parity

import (
    "embed"
    "encoding/json"
    "io/fs"
    "strings"

    errors "github.com/teranos/sacred-error"
)

//go:embed codex_*/manifest.json
var codexPin embed.FS

// Codex is the single pinned Codex CLI release and the SHA-256 of its
// upstream release archives per platform. These are archive hashes, not binary hashes.
func Codex() (version string, artifacts map[string]struct{ File, SHA256 string }, err error) {
    entries, err := fs.ReadDir(codexPin, ".")
    if err != nil {
        return "", nil, errors.Wrap(err, "the Codex pin did not read")
    }
    if len(entries) != 1 {
        return "", nil, errors.Newf("Codex is pinned %d times, and a build runs one", len(entries))
    }
    dir := entries[0].Name()
    raw, err := codexPin.ReadFile(dir + "/manifest.json")
    if err != nil {
        return "", nil, errors.Wrapf(err, "%s/manifest.json did not read", dir)
    }
    var pinned struct {
        Version string `json:"version"`
        Commit string `json:"commit"`
        Artifacts map[string]struct {
            File string `json:"file"`
            SHA256 string `json:"sha256"`
        } `json:"artifacts"`
    }
    if err := json.Unmarshal(raw, &pinned); err != nil {
        return "", nil, errors.Wrapf(err, "%s/manifest.json is not a manifest", dir)
    }
    rev, named := strings.CutPrefix(dir, "codex_"+pinned.Version+"_")
    if !named || rev == "" || !strings.HasPrefix(pinned.Commit, rev) {
        return "", nil, errors.Newf("%s does not name the pinned Codex version and commit", dir)
    }
    artifacts = make(map[string]struct{ File, SHA256 string }, len(pinned.Artifacts))
    for platform, artifact := range pinned.Artifacts {
        artifacts[platform] = struct{ File, SHA256 string }{artifact.File, artifact.SHA256}
    }
    return pinned.Version, artifacts, nil
}
