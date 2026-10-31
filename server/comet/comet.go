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

// Name is one comet: a repository's sand, for one platform and one engine ABI, carrying one machine's controls.
// Machine is the owner/repo whose controls/ hold that machine's pbt: each machine's are in its own repository.
// ABI is the engine the sand was built for (ground source/sandview.d SAND_ABI); a build is asked for without one.
type Name struct{ Owner, Repo, Platform, ABI, Machine string }

// MachineOf is the owner/repo a person named as the machine a comet carries the controls of.
func MachineOf(named string) (string, error) {
	refused := errors.Newf("%q names no machine: a machine is the owner/repo whose controls/ hold its pbt, as machine={owner}/{repo}", named)
	if strings.Count(named, "/") != 1 || strings.HasPrefix(named, "/") || strings.HasSuffix(named, "/") || strings.ContainsAny(named, " ?#") {
		return "", refused
	}
	for _, part := range strings.Split(named, "/") {
		// A name that is only dots climbs out of where the machine is unpacked.
		if strings.Trim(part, ".") != part {
			return "", refused
		}
	}
	return named, nil
}

// Key is the comet as its path names it: owner/repo/platform, and its ABI when it names one.
func (n Name) Key() string {
	if n.ABI == "" {
		return n.Base()
	}
	return n.Base() + "/" + n.ABI
}

// Base is what a build is for: owner/repo/platform, of whichever ABI ground's main builds.
func (n Name) Base() string { return n.Owner + "/" + n.Repo + "/" + n.Platform }

// SandFile is the file a sand is for platform, as ground's make sand names it.
func SandFile(platform string) string {
	if strings.HasSuffix(platform, "-darwin") {
		return "libground_sand.dylib"
	}
	return "libground_sand.so"
}

// digits is whether s is a decimal number, as ground prints its ABI.
func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Native is the system this node builds natively, as ground's flake names it.
func Native() (string, error) {
	arch, ok := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	platform := arch + "-" + runtime.GOOS
	if !ok || !Platforms[platform] {
		return "", errors.Newf("this node runs %s/%s, which builds no platform ground's flake names", runtime.GOOS, runtime.GOARCH)
	}
	return platform, nil
}

// Of is the comet a /comet/{owner}/{repo}/{platform}[/{abi}] path names.
func Of(path string) (Name, error) {
	rest, ok := strings.CutPrefix(path, "/comet/")
	refused := errors.Newf("%s names no comet: a comet is /comet/{owner}/{repo}/{platform}/{abi}, and built at /comet/{owner}/{repo}/{platform}", path)
	if !ok || strings.HasPrefix(rest, "/") || strings.HasSuffix(rest, "/") {
		return Name{}, refused
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 && len(parts) != 4 {
		return Name{}, refused
	}
	abi := ""
	if len(parts) == 4 {
		if !digits(parts[3]) {
			return Name{}, errors.Newf("%s names %s, which is no engine ABI: ground names its ABI in decimal digits", path, parts[3])
		}
		abi = parts[3]
		parts = parts[:3]
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
	return Name{Owner: parts[0], Repo: parts[1], Platform: parts[2], ABI: abi}, nil
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

// Path is where the comet n is kept on the node: the sand of its platform and ABI.
func Path(n Name) (string, error) {
	if n.ABI == "" {
		return "", errors.Newf("the comet of %s names no engine ABI, and a sand is kept by the ABI it was built for", n.Key())
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, n.Owner, n.Repo, n.Platform, n.ABI, SandFile(n.Platform)), nil
}

// BuiltABI is the engine ABI of the sand built in src, as ground's make sand wrote it beside the library.
func BuiltABI(src string) (string, error) {
	path := filepath.Join(src, "build", "libground_sand.abi")
	said, err := os.ReadFile(path)
	if err != nil {
		return "", errors.Wrapf(err, "the sand built in %s does not say its engine ABI", src)
	}
	abi := strings.TrimSpace(string(said))
	if !digits(abi) {
		return "", errors.Newf("%s says %q, which is not an engine ABI", path, abi)
	}
	return abi, nil
}

// Repository is one repository as a comet, as the Ground element draws it (web/ts/ground-scene.ts Comet):
// the grounds built from it, the heaviest's size in MB, and how often its default branch moved in the last week.
type Repository struct {
	Repo   string  `json:"repo"`
	Built  bool    `json:"built"`
	Size   float64 `json:"size"`
	Moved  int     `json:"moved"`
	State  string  `json:"state"`
	Builds []Built `json:"builds"`
}

// Built is one platform's sand built from a repository: the engine ABI it is for, its sha256 digest, its bytes and when it was built.
type Built struct {
	Platform string `json:"platform"`
	ABI      string `json:"abi"`
	Digest   string `json:"digest"`
	Bytes    int64  `json:"bytes"`
	At       string `json:"at"`
}

// "a comet can be in the observed state"

// observed is a comet the node keeps: a push that moves it rebuilds it, so when main moves, ground moves.
const observed = "observed"

// Held is each repository with a comet kept under dir, as am ground says it, its builds by platform.
// moved is how often the repository's default branch moved in the last week, asked of GitHub.
func Held(dir string, moved func(owner, repo string) (int, error)) ([]Repository, error) {
	names, err := Names(dir)
	if err != nil {
		return nil, err
	}
	repos := []Repository{}
	at := map[string]int{}
	for _, n := range names {
		path := filepath.Join(dir, n.Owner, n.Repo, n.Platform, n.ABI, SandFile(n.Platform))
		info, err := os.Stat(path)
		if err != nil {
			return nil, errors.Wrapf(err, "the comet at %s was not read", path)
		}
		digest, err := Digest(path)
		if err != nil {
			return nil, err
		}
		key := n.Owner + "/" + n.Repo
		i, seen := at[key]
		if !seen {
			count, err := moved(n.Owner, n.Repo)
			if err != nil {
				return nil, errors.Wrapf(err, "how often %s moved was not read", key)
			}
			i = len(repos)
			at[key] = i
			repos = append(repos, Repository{Repo: key, Built: true, Moved: count, State: observed, Builds: []Built{}})
		}
		repos[i].Builds = append(repos[i].Builds, Built{Platform: n.Platform, ABI: n.ABI, Digest: digest, Bytes: info.Size(), At: info.ModTime().UTC().Format(time.RFC3339)})
		repos[i].Size = max(repos[i].Size, float64(info.Size())/(1<<20))
	}
	return repos, nil
}

// Names is each comet kept under dir, by name.
func Names(dir string) ([]Name, error) {
	found := []string{}
	for _, file := range []string{"libground_sand.dylib", "libground_sand.so"} {
		kept, err := filepath.Glob(filepath.Join(dir, "*", "*", "*", "*", file))
		if err != nil {
			return nil, errors.Wrapf(err, "the comets under %s were not listed", dir)
		}
		found = append(found, kept...)
	}
	names := make([]Name, 0, len(found))
	for _, path := range found {
		abi := filepath.Dir(path)
		platform := filepath.Dir(abi)
		repo := filepath.Dir(platform)
		n := Name{Owner: filepath.Base(filepath.Dir(repo)), Repo: filepath.Base(repo), Platform: filepath.Base(platform), ABI: filepath.Base(abi)}
		machine, err := os.ReadFile(path + ".machine")
		if err != nil {
			return nil, errors.Wrapf(err, "the comet at %s does not say which machine's controls it carries", path)
		}
		n.Machine = string(machine)
		names = append(names, n)
	}
	return names, nil
}

// MovedBy is each kept comet a push to repo's branch moves: the project's own default branch,
// or ground's main, which every comet is built from.
func MovedBy(kept []Name, repo, branch, defaultBranch string) []Name {
	moved := []Name{}
	// One build per repository and platform: it builds whichever ABI ground's main is, so the
	// sands kept for older ABIs are not each built again.
	building := map[string]bool{}
	groundMoved := strings.EqualFold(repo, Ground.Owner+"/"+Ground.Repo) && branch == Ground.Branch
	for _, n := range kept {
		projectMoved := strings.EqualFold(repo, n.Owner+"/"+n.Repo) && branch == defaultBranch
		if (groundMoved || projectMoved) && !building[n.Base()] {
			building[n.Base()] = true
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
