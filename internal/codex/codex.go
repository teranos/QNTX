// Package codex begins "the Codex Agent runtime" beside Claude Code and Pi.
package codex

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/teranos/QNTX/server/parity"
	errors "github.com/teranos/sacred-error"
)

type Artifact struct {
	File, SHA256 string
	Beside       []Beside
}

// Beside is an archive whose one executable codex runs from beside itself, at Path.
type Beside struct {
	Artifact
	Path string
}
type Pin struct {
	Base, Version string
	Artifacts     map[string]Artifact
}

// PinnedVersion is the codex Parity pins, as what is said of a turn names it.
var PinnedVersion = func() string {
	version, _, err := parity.Codex()
	if err != nil {
		return ""
	}
	return version
}()

func Pinned() (Pin, error) {
	version, artifacts, err := parity.Codex()
	if err != nil {
		return Pin{}, err
	}
	p := Pin{Base: "https://github.com/openai/codex/releases/download", Version: version, Artifacts: map[string]Artifact{}}
	for platform, a := range artifacts {
		pinned := Artifact{File: a.File, SHA256: a.SHA256}
		for _, b := range a.Beside {
			pinned.Beside = append(pinned.Beside, Beside{Artifact: Artifact{File: b.File, SHA256: b.SHA256}, Path: b.Path})
		}
		p.Artifacts[platform] = pinned
	}
	return p, nil
}

func Platform() (string, error) {
	arch, known := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	if !known || (runtime.GOOS != "linux" && runtime.GOOS != "darwin") {
		return "", errors.Newf("Codex is not pinned for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return runtime.GOOS + "-" + arch, nil
}

// Ensure verifies the archive on every call and extracts a fresh executable.
// The pin authenticates the archive; a cached executable has no pinned hash.
func (p Pin) Ensure(ctx context.Context, dir string) (string, error) {
	platform, err := Platform()
	if err != nil {
		return "", err
	}
	a, ok := p.Artifacts[platform]
	if !ok {
		return "", errors.Newf("Codex %s has no archive for %s", p.Version, platform)
	}
	if filepath.Base(p.Version) != p.Version || p.Version == "." || p.Version == ".." || p.Version == "" || !strings.HasPrefix(a.File, "codex-") {
		return "", errors.Newf("invalid Codex version %q or archive %q", p.Version, a.File)
	}
	dest := filepath.Join(dir, p.Version)
	if err := os.MkdirAll(dest, 0700); err != nil {
		return "", errors.Wrapf(err, "could not create %s", dest)
	}
	target := filepath.Join(dest, "codex")
	if err := p.install(ctx, dest, a, target); err != nil {
		return "", err
	}
	for _, b := range a.Beside {
		if !filepath.IsLocal(b.Path) {
			return "", errors.Newf("Codex %s would install %s outside %s", p.Version, b.Path, dest)
		}
		at := filepath.Join(dest, b.Path)
		if err := os.MkdirAll(filepath.Dir(at), 0700); err != nil {
			return "", errors.Wrapf(err, "could not create %s", filepath.Dir(at))
		}
		if err := p.install(ctx, dest, b.Artifact, at); err != nil {
			return "", err
		}
	}
	return target, nil
}

// install verifies a's archive, fetching it when the cached one is not the
// pinned one, and extracts its one executable to target.
func (p Pin) install(ctx context.Context, dest string, a Artifact, target string) error {
	if filepath.Base(a.File) != a.File || !strings.HasSuffix(a.File, ".tar.gz") {
		return errors.Newf("invalid Codex %s archive %q", p.Version, a.File)
	}
	digest, err := hex.DecodeString(a.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return errors.Newf("Codex %s archive %s has invalid sha256 %q", p.Version, a.File, a.SHA256)
	}
	cached := filepath.Join(dest, a.File)
	f, err := os.Open(cached)
	if err == nil {
		err = verify(f, a.SHA256)
		if err == nil {
			_, err = f.Seek(0, io.SeekStart)
		}
		if err != nil {
			_ = f.Close()
		}
	}
	if err != nil {
		if err := download(ctx, p.Base+"/rust-v"+p.Version+"/"+a.File, cached, a.SHA256); err != nil {
			return err
		}
		f, err = os.Open(cached)
		if err != nil {
			return errors.Wrapf(err, "could not open verified archive %s", cached)
		}
	}
	defer f.Close()
	if err := extract(f, strings.TrimSuffix(a.File, ".tar.gz"), target); err != nil {
		return errors.Wrapf(err, "could not install Codex %s from %s", p.Version, cached)
	}
	return nil
}

func verify(f *os.File, want string) error {
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return errors.Wrapf(err, "could not hash %s", f.Name())
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return errors.Newf("%s has sha256 %s, pin is %s", f.Name(), got, want)
	}
	return nil
}

func download(ctx context.Context, url, target, want string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return errors.Wrapf(err, "could not request %s", url)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrapf(err, "could not fetch %s", url)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.Newf("%s answered %s", url, resp.Status)
	}
	f, err := os.CreateTemp(filepath.Dir(target), "codex-archive-*")
	if err != nil {
		return errors.Wrapf(err, "could not stage %s", target)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return errors.Wrapf(err, "download of %s was cut short", url)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return errors.Wrapf(err, "could not rewind %s", f.Name())
	}
	if err := verify(f, want); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return errors.Wrapf(err, "could not close %s", f.Name())
	}
	return errors.Wrapf(os.Rename(f.Name(), target), "could not publish archive %s", target)
}

func extract(src io.Reader, member, target string) error {
	gz, err := gzip.NewReader(src)
	if err != nil {
		return errors.Wrap(err, "invalid gzip archive")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	f, err := os.CreateTemp(filepath.Dir(target), "codex-executable-*")
	if err != nil {
		return errors.Wrapf(err, "could not stage %s", target)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	found := false
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.Wrap(err, "invalid Codex tar archive")
		}
		if h.Name != member {
			return errors.Newf("unexpected Codex archive member %q, want %q", h.Name, member)
		}
		if found || h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > 512<<20 {
			return errors.Newf("invalid Codex executable member %q (type %d, size %d)", h.Name, h.Typeflag, h.Size)
		}
		if _, err := io.Copy(f, tr); err != nil {
			return errors.Wrapf(err, "could not extract %s", member)
		}
		found = true
	}
	// Drain gzip as well: its checksum follows the tar end marker.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return errors.Wrap(err, "Codex gzip archive was cut short")
	}
	if !found {
		return errors.Newf("Codex archive has no %s", member)
	}
	if err := f.Chmod(0700); err != nil {
		return errors.Wrapf(err, "could not make %s executable", f.Name())
	}
	if err := f.Close(); err != nil {
		return errors.Wrapf(err, "could not close %s", f.Name())
	}
	return errors.Wrapf(os.Rename(f.Name(), target), "could not publish executable %s", target)
}
