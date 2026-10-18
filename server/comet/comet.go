// Package comet is the node building ground for one project, and the build landing where ground runs (ADR-050).
package comet

// "the feature i am imagining is called comet"

// "if QNTX is heaven"

// "and ground is here, as in earth"

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	errors "github.com/teranos/sacred-error"
)

// "A comet is one sand for one user, one project and one platform."

// Platforms are the systems ground's flake names, as ground names the one it was built for.
var Platforms = map[string]bool{"x86_64-linux": true, "aarch64-linux": true, "x86_64-darwin": true, "aarch64-darwin": true}

// Name is one comet: a repository, for one platform.
type Name struct{ Owner, Repo, Platform string }

// Key is the comet as its path names it: owner/repo/platform.
func (n Name) Key() string { return n.Owner + "/" + n.Repo + "/" + n.Platform }

// Native is the system this node builds natively, as ground's flake names it.
func Native() (string, error) {
	arch, ok := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	platform := arch + "-" + runtime.GOOS
	if !ok || !Platforms[platform] {
		return "", errors.Newf("this node runs %s/%s, which builds no platform ground's flake names", runtime.GOOS, runtime.GOARCH)
	}
	return platform, nil
}

// Of is the comet a /comet/{owner}/{repo}/{platform} path names.
func Of(path string) (Name, error) {
	rest, ok := strings.CutPrefix(path, "/comet/")
	refused := errors.Newf("%s names no comet: a comet is /comet/{owner}/{repo}/{platform}", path)
	if !ok || strings.HasPrefix(rest, "/") || strings.HasSuffix(rest, "/") {
		return Name{}, refused
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return Name{}, refused
	}
	for _, part := range parts {
		// A name that is only dots climbs out of the comets directory.
		if strings.Trim(part, ".") != part {
			return Name{}, refused
		}
	}
	if !Platforms[parts[2]] {
		return Name{}, errors.Newf("%s names %s, which is not a platform ground's flake names: x86_64-linux, aarch64-linux, x86_64-darwin or aarch64-darwin", path, parts[2])
	}
	return Name{Owner: parts[0], Repo: parts[1], Platform: parts[2]}, nil
}

// Declaration is the one project block a comet's sand holds besides ground's own controls.
// The path is where wind reads the project's controls on the node; the origin is how ground
// knows the repository on the machine it lands on, wherever its checkout is (ground source/place.d).
func Declaration(checkout, owner, repo string) string {
	return "project {\n  path: \"" + checkout + "\"\n  origin: \"" + owner + "/" + repo + "\"\n}\n"
}

// Dir is where the node keeps every comet, one per repository and platform.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "the node's home was not found, so there is nowhere to keep a comet")
	}
	return filepath.Join(home, ".qntx", "comets"), nil
}

// Path is where the comet n is kept on the node.
func Path(n Name) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, n.Owner, n.Repo, n.Platform, "ground"), nil
}

// Held is each comet kept under dir, as am ground says it: its repository, platform, digest, size and when it was built.
func Held(dir string) ([]map[string]any, error) {
	found, err := filepath.Glob(filepath.Join(dir, "*", "*", "*", "ground"))
	if err != nil {
		return nil, errors.Wrapf(err, "the comets under %s were not listed", dir)
	}
	comets := make([]map[string]any, 0, len(found))
	for _, path := range found {
		info, err := os.Stat(path)
		if err != nil {
			return nil, errors.Wrapf(err, "the comet at %s was not read", path)
		}
		digest, err := Digest(path)
		if err != nil {
			return nil, err
		}
		platform := filepath.Dir(path)
		repo := filepath.Dir(platform)
		owner := filepath.Dir(repo)
		comets = append(comets, map[string]any{
			"repository": filepath.Base(owner) + "/" + filepath.Base(repo),
			"platform":   filepath.Base(platform),
			"digest":     digest,
			"size":       info.Size(),
			"built":      info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return comets, nil
}

// Kept is each comet kept under dir, by name.
func Kept(dir string) ([]Name, error) {
	found, err := filepath.Glob(filepath.Join(dir, "*", "*", "*", "ground"))
	if err != nil {
		return nil, errors.Wrapf(err, "the comets under %s were not listed", dir)
	}
	names := make([]Name, 0, len(found))
	for _, path := range found {
		platform := filepath.Dir(path)
		repo := filepath.Dir(platform)
		names = append(names, Name{Owner: filepath.Base(filepath.Dir(repo)), Repo: filepath.Base(repo), Platform: filepath.Base(platform)})
	}
	return names, nil
}

// MovedBy is each kept comet a push to repo's branch moves: the project's own default branch,
// or ground's main, which every comet is built from.
func MovedBy(kept []Name, repo, branch, defaultBranch string) []Name {
	moved := []Name{}
	groundMoved := strings.EqualFold(repo, Ground.Owner+"/"+Ground.Repo) && branch == Ground.Branch
	for _, n := range kept {
		projectMoved := strings.EqualFold(repo, n.Owner+"/"+n.Repo) && branch == defaultBranch
		if groundMoved || projectMoved {
			moved = append(moved, n)
		}
	}
	return moved
}

// KeepDigest writes the comet's sha256 beside it, once, when it is built.
func KeepDigest(path string) error {
	binary, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrapf(err, "the comet at %s was not read to digest", path)
	}
	sum := sha256.Sum256(binary)
	return errors.Wrapf(os.WriteFile(path+".digest", []byte(hex.EncodeToString(sum[:])), 0o644), "failed to write the digest beside %s", path)
}

// Digest is the digest kept beside a comet: asked every few seconds, never hashed again.
func Digest(path string) (string, error) {
	said, err := os.ReadFile(path + ".digest")
	if err != nil {
		return "", errors.Wrapf(err, "no digest is kept beside %s", path)
	}
	return string(said), nil
}
