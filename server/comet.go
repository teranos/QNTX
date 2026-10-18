package server

// "the feature i am imagining is called comet"

// "if QNTX is heaven"

// "and ground is here, as in earth"

// The node builds ground for one project, and the build lands where ground runs (ADR-050).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/sacred"
	errors "github.com/teranos/sacred-error"
)

// cometGround is where ground's own sources and global controls come from.
var cometGround = buildSource{Owner: "teranos", Repo: "ground", Branch: "main"}

// cometBuilds is what each comet's last build came to, by owner/repo, until one is kept:
// building since when, or why it failed. A comet kept has no entry.
var cometBuilds sync.Map

// cometOf is the repository a /comet/{owner}/{repo} path names.
func cometOf(path string) (string, string, error) {
	rest, ok := strings.CutPrefix(path, "/comet/")
	refused := errors.Newf("%s names no repository: a comet is /comet/{owner}/{repo}", path)
	if !ok || strings.HasPrefix(rest, "/") || strings.HasSuffix(rest, "/") {
		return "", "", refused
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		return "", "", refused
	}
	for _, part := range parts {
		// A name that is only dots climbs out of the comets directory.
		if strings.Trim(part, ".") != part {
			return "", "", refused
		}
	}
	return parts[0], parts[1], nil
}

// cometDeclaration is the one project block a comet's sand holds besides ground's own controls.
func cometDeclaration(checkout string) string {
	return "project {\n  path: \"" + checkout + "\"\n}\n"
}

// cometsDir is where the node keeps every comet, one per repository.
func cometsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "the node's home was not found, so there is nowhere to keep a comet")
	}
	return filepath.Join(home, ".qntx", "comets"), nil
}

// cometPath is where a project's comet is kept on the node.
func cometPath(owner, repo string) (string, error) {
	dir, err := cometsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, owner, repo, "ground"), nil
}

// heldComets is each comet kept under dir, as am ground says it: its repository, digest, size and when it was built.
func heldComets(dir string) ([]map[string]any, error) {
	found, err := filepath.Glob(filepath.Join(dir, "*", "*", "ground"))
	if err != nil {
		return nil, errors.Wrapf(err, "the comets under %s were not listed", dir)
	}
	comets := make([]map[string]any, 0, len(found))
	for _, path := range found {
		info, err := os.Stat(path)
		if err != nil {
			return nil, errors.Wrapf(err, "the comet at %s was not read", path)
		}
		digest, err := cometDigest(path)
		if err != nil {
			return nil, err
		}
		repo := filepath.Base(filepath.Dir(path))
		owner := filepath.Base(filepath.Dir(filepath.Dir(path)))
		comets = append(comets, map[string]any{
			"repository": owner + "/" + repo,
			"digest":     digest,
			"size":       info.Size(),
			"built":      info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return comets, nil
}

// HandleComet serves a project's comet (GET), its digest (GET ?digest, or HEAD), or builds it (POST).
func (s *QNTXServer) HandleComet(w http.ResponseWriter, r *http.Request) {
	owner, repo, err := cometOf(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	key := owner + "/" + repo
	// HEAD is ground asking whether its comet moved: the digest, and none of the bytes.
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		path, err := cometPath(owner, repo)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		digest, err := cometDigest(path)
		if errors.Is(err, os.ErrNotExist) {
			said := "no comet of " + key + " is kept: POST /comet/" + key + " builds it"
			if last, building := cometBuilds.Load(key); building {
				said = "no comet of " + key + " is kept: " + last.(string)
			}
			writeError(w, http.StatusNotFound, said)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if r.URL.Query().Has("digest") {
			respond(w, s.logger, http.StatusOK, map[string]string{"digest": digest})
			return
		}
		w.Header().Set("X-Comet-Digest", digest)
		http.ServeFile(w, r, path)
		return
	}
	if r.Method == http.MethodPost {
		cometBuilds.Store(key, "building since "+time.Now().UTC().Format(time.RFC3339))
		source := buildSource{Owner: owner, Repo: repo}
		sacred.Go("comet.build."+key, func() {
			if err := s.buildComet(s.ctx, source); err != nil {
				cometBuilds.Store(key, "its last build failed: "+err.Error())
				return
			}
			cometBuilds.Delete(key)
		})
		respond(w, s.logger, http.StatusAccepted, map[string]string{"building": key})
		return
	}
	writeError(w, http.StatusMethodNotAllowed, r.Method+" /comet/"+key+": a comet is read with GET, its digest with HEAD or ?digest, and built with POST")
}

// buildComet builds ground with its own controls and the project's, at both repositories' latest, and keeps it.
// Each comet builds in a directory of its own, emptied when its next build begins: a failed build stays to be read until then.
func (s *QNTXServer) buildComet(ctx context.Context, project buildSource) error {
	builds, err := buildsDir()
	if err != nil {
		return err
	}
	work := filepath.Join(builds, "comet-"+project.Owner+"-"+project.Repo)
	if err := os.RemoveAll(work); err != nil {
		return errors.Wrapf(err, "the last build of the comet of %s in %s was not cleared", project.String(), work)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return errors.Wrapf(err, "failed to make %s to build the comet of %s in", work, project.String())
	}
	return s.buildCometIn(ctx, project, work)
}

// buildCometIn builds the comet in work and keeps it.
func (s *QNTXServer) buildCometIn(ctx context.Context, project buildSource, work string) error {
	src := filepath.Join(work, "ground")
	checkout := filepath.Join(work, "project")
	for _, got := range []struct {
		source buildSource
		dir    string
	}{{cometGround, src}, {project, checkout}} {
		rev, err := s.buildRev(ctx, got.source)
		if err != nil {
			return errors.Wrapf(err, "the latest of %s was not read", got.source.String())
		}
		tarball := got.dir + ".tar.gz"
		if err := s.fetchCore(ctx, got.source, rev, tarball); err != nil {
			return err
		}
		if err := os.Mkdir(got.dir, 0o755); err != nil {
			return errors.Wrapf(err, "failed to make %s", got.dir)
		}
		if err := runBuild(ctx, work, nil, "tar", "-xzf", tarball, "--strip-components=1", "-C", got.dir); err != nil {
			return errors.Wrapf(err, "failed to unpack %s at %s", got.source.String(), rev)
		}
	}

	local := filepath.Join(src, "controls", "local")
	if err := os.MkdirAll(local, 0o755); err != nil {
		return errors.Wrapf(err, "failed to make %s", local)
	}
	if err := os.WriteFile(filepath.Join(local, "comet.pbt"), []byte(cometDeclaration(checkout)), 0o644); err != nil {
		return errors.Wrapf(err, "failed to write the comet's project block in %s", local)
	}
	if err := os.Mkdir(filepath.Join(work, "tmp"), 0o755); err != nil {
		return errors.Wrapf(err, "failed to make the comet's temp directory in %s", work)
	}

	env := []string{"PATH=" + nixBin + ":" + os.Getenv("PATH"), "TMPDIR=" + filepath.Join(work, "tmp")}
	nice, args := gentle(filepath.Join(nixBin, "nix"), []string{"shell", "--inputs-from", src, "nixpkgs#gnumake", "-c",
		filepath.Join(nixBin, "nix"), "develop", ".#default", "--command", "make", "build"})
	if err := runBuild(ctx, src, env, nice, args...); err != nil {
		return errors.Wrapf(err, "the comet of %s did not build", project.String())
	}

	kept, err := cometPath(project.Owner, project.Repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(kept), 0o755); err != nil {
		return errors.Wrapf(err, "failed to make %s", filepath.Dir(kept))
	}
	binary, err := os.ReadFile(filepath.Join(src, "ground"))
	if err != nil {
		return errors.Wrapf(err, "the built comet of %s was not found in %s", project.String(), src)
	}
	// Renamed into place, never written over: a GET reading the old one reads it whole.
	staged := kept + ".new"
	if err := os.WriteFile(staged, binary, 0o755); err != nil {
		return errors.Wrapf(err, "failed to write %s", staged)
	}
	if err := os.Rename(staged, kept); err != nil {
		return errors.Wrapf(err, "failed to put the comet of %s in place at %s", project.String(), kept)
	}
	return keepDigest(kept)
}

// keepDigest writes the comet's sha256 beside it, once, when it is built.
func keepDigest(path string) error {
	binary, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrapf(err, "the comet at %s was not read to digest", path)
	}
	sum := sha256.Sum256(binary)
	return errors.Wrapf(os.WriteFile(path+".digest", []byte(hex.EncodeToString(sum[:])), 0o644), "failed to write the digest beside %s", path)
}

// cometDigest is the digest kept beside a comet: asked every few seconds, never hashed again.
func cometDigest(path string) (string, error) {
	said, err := os.ReadFile(path + ".digest")
	if err != nil {
		return "", errors.Wrapf(err, "no digest is kept beside %s", path)
	}
	return string(said), nil
}
