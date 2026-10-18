package server

// "the feature i am imagining is called comet"

// "if QNTX is heaven"

// "and ground is here, as in earth"

// The node builds ground for one project, and the build lands where ground runs (ADR-050).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teranos/QNTX/internal/sacred"
	errors "github.com/teranos/sacred-error"
)

// cometGround is where ground's own sources and global controls come from.
var cometGround = buildSource{Owner: "teranos", Repo: "ground", Branch: "main"}

// cometOf is the repository a /comet/{owner}/{repo} path names.
func cometOf(path string) (string, string, error) {
	rest, ok := strings.CutPrefix(path, "/comet/")
	if !ok {
		return "", "", errors.Newf("%s is not a comet's path", path)
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		return "", "", errors.Newf("%s names no repository: a comet is /comet/{owner}/{repo}", path)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", "", errors.Newf("%s names no repository: a comet is /comet/{owner}/{repo}", path)
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
		digest, err := fileDigest(path)
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

// HandleComet serves a project's comet (GET), or builds it (POST) and says so when it is done.
func (s *QNTXServer) HandleComet(w http.ResponseWriter, r *http.Request) {
	owner, repo, err := cometOf(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	switch r.Method {
	case http.MethodGet:
		path, err := cometPath(owner, repo)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		digest, err := fileDigest(path)
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "no comet of "+owner+"/"+repo+" has been built: POST /comet/"+owner+"/"+repo+" builds it")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("X-Comet-Digest", digest)
		http.ServeFile(w, r, path)
	case http.MethodPost:
		source := buildSource{Owner: owner, Repo: repo}
		sacred.Go("comet.build."+owner+"/"+repo, func() {
			path, err := s.buildComet(s.ctx, source)
			if err != nil {
				s.logger.Errorw("the comet did not build", "comet", owner+"/"+repo, "error", err)
				return
			}
			s.logger.Infow("the comet is built", "comet", owner+"/"+repo, "path", path)
		})
		w.WriteHeader(http.StatusAccepted)
	default:
		writeError(w, http.StatusMethodNotAllowed, r.Method+" /comet/"+owner+"/"+repo+": a comet is read with GET and built with POST")
	}
}

// buildComet builds ground with its own controls and the project's, at both repositories' latest, and keeps it.
func (s *QNTXServer) buildComet(ctx context.Context, project buildSource) (string, error) {
	builds, err := buildsDir()
	if err != nil {
		return "", err
	}
	work, err := os.MkdirTemp(builds, "comet-"+project.Owner+"-"+project.Repo+"-")
	if err != nil {
		return "", errors.Wrapf(err, "failed to make a directory in %s to build the comet of %s in", builds, project.String())
	}
	defer func() {
		if removeErr := os.RemoveAll(work); removeErr != nil {
			s.logger.Errorw("the comet's build directory was not removed", "dir", work, "error", removeErr)
		}
	}()

	src := filepath.Join(work, "ground")
	checkout := filepath.Join(work, "project")
	for _, got := range []struct {
		source buildSource
		dir    string
	}{{cometGround, src}, {project, checkout}} {
		rev, err := s.buildRev(ctx, got.source)
		if err != nil {
			return "", errors.Wrapf(err, "the latest of %s was not read", got.source.String())
		}
		tarball := got.dir + ".tar.gz"
		if err := s.fetchCore(ctx, got.source, rev, tarball); err != nil {
			return "", err
		}
		if err := os.Mkdir(got.dir, 0o755); err != nil {
			return "", errors.Wrapf(err, "failed to make %s", got.dir)
		}
		if err := runBuild(ctx, work, nil, "tar", "-xzf", tarball, "--strip-components=1", "-C", got.dir); err != nil {
			return "", errors.Wrapf(err, "failed to unpack %s at %s", got.source.String(), rev)
		}
	}

	local := filepath.Join(src, "controls", "local")
	if err := os.MkdirAll(local, 0o755); err != nil {
		return "", errors.Wrapf(err, "failed to make %s", local)
	}
	if err := os.WriteFile(filepath.Join(local, "comet.pbt"), []byte(cometDeclaration(checkout)), 0o644); err != nil {
		return "", errors.Wrapf(err, "failed to write the comet's project block in %s", local)
	}
	if err := os.Mkdir(filepath.Join(work, "tmp"), 0o755); err != nil {
		return "", errors.Wrapf(err, "failed to make the comet's temp directory in %s", work)
	}

	env := []string{"PATH=" + nixBin + ":" + os.Getenv("PATH"), "TMPDIR=" + filepath.Join(work, "tmp")}
	nice, args := gentle(filepath.Join(nixBin, "nix"), []string{"shell", "--inputs-from", src, "nixpkgs#gnumake", "-c",
		filepath.Join(nixBin, "nix"), "develop", ".#default", "--command", "make", "build"})
	if err := runBuild(ctx, src, env, nice, args...); err != nil {
		return "", errors.Wrapf(err, "the comet of %s did not build", project.String())
	}

	kept, err := cometPath(project.Owner, project.Repo)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(kept), 0o755); err != nil {
		return "", errors.Wrapf(err, "failed to make %s", filepath.Dir(kept))
	}
	// Renamed into place, never written over: a GET reading the old one reads it whole.
	staged := kept + ".new"
	if err := copyFile(filepath.Join(src, "ground"), staged); err != nil {
		return "", err
	}
	if err := os.Rename(staged, kept); err != nil {
		return "", errors.Wrapf(err, "failed to put the comet of %s in place at %s", project.String(), kept)
	}
	return kept, nil
}

// fileDigest is a file's sha256, in hex.
func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", errors.Wrapf(err, "failed to open %s", path)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", errors.Wrapf(err, "failed to read %s", path)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyFile writes from's bytes to to, executable.
func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return errors.Wrapf(err, "the built comet at %s was not found", from)
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return errors.Wrapf(err, "failed to make %s", to)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return errors.Wrapf(err, "failed to copy %s to %s", from, to)
	}
	return errors.Wrapf(out.Close(), "failed to finish writing %s", to)
}
