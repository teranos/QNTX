// Package pinned fetches the release archive a pin names, checks it against the
// pinned sha256, and extracts its one executable. product is the name the
// errors say, and lowercased the prefix of the files staged beside the target.
package pinned

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
	"strings"

	errors "github.com/teranos/sacred-error"
)

// Open is the archive at cached once it is the pinned one, fetched from url
// when what is cached is not.
func Open(ctx context.Context, product, url, cached, want string) (*os.File, error) {
	f, err := os.Open(cached)
	if err == nil {
		err = Verify(f, want)
		if err == nil {
			_, err = f.Seek(0, io.SeekStart)
		}
		if err != nil {
			_ = f.Close()
		}
	}
	if err != nil {
		if err := Download(ctx, product, url, cached, want); err != nil {
			return nil, err
		}
		f, err = os.Open(cached)
		if err != nil {
			return nil, errors.Wrapf(err, "could not open verified archive %s", cached)
		}
	}
	return f, nil
}

// Verify says whether f hashes to want.
func Verify(f *os.File, want string) error {
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return errors.Wrapf(err, "could not hash %s", f.Name())
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return errors.Newf("%s has sha256 %s, pin is %s", f.Name(), got, want)
	}
	return nil
}

// Download fetches url to target, which it publishes only once it hashes to want.
func Download(ctx context.Context, product, url, target, want string) error {
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
	f, err := os.CreateTemp(filepath.Dir(target), strings.ToLower(product)+"-archive-*")
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
	if err := Verify(f, want); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return errors.Wrapf(err, "could not close %s", f.Name())
	}
	return errors.Wrapf(os.Rename(f.Name(), target), "could not publish archive %s", target)
}

// Extract writes the one regular file the gzipped tar src holds, named member,
// to target as an executable.
func Extract(src io.Reader, product, member, target string) error {
	gz, err := gzip.NewReader(src)
	if err != nil {
		return errors.Wrap(err, "invalid gzip archive")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	f, err := os.CreateTemp(filepath.Dir(target), strings.ToLower(product)+"-executable-*")
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
			return errors.Wrapf(err, "invalid %s tar archive", product)
		}
		if h.Name != member {
			return errors.Newf("unexpected %s archive member %q, want %q", product, h.Name, member)
		}
		if found || h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > 512<<20 {
			return errors.Newf("invalid %s executable member %q (type %d, size %d)", product, h.Name, h.Typeflag, h.Size)
		}
		if _, err := io.Copy(f, tr); err != nil {
			return errors.Wrapf(err, "could not extract %s", member)
		}
		found = true
	}
	// Drain gzip as well: its checksum follows the tar end marker.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return errors.Wrapf(err, "%s gzip archive was cut short", product)
	}
	if !found {
		return errors.Newf("%s archive has no %s", product, member)
	}
	if err := f.Chmod(0700); err != nil {
		return errors.Wrapf(err, "could not make %s executable", f.Name())
	}
	if err := f.Close(); err != nil {
		return errors.Wrapf(err, "could not close %s", f.Name())
	}
	return errors.Wrapf(os.Rename(f.Name(), target), "could not publish executable %s", target)
}
