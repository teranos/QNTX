// Package claudecode is the Claude Code a build of QNTX runs its agents on
// (ADR-048): which one, and getting it onto the node that needs it.
package claudecode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/QNTX/server/parity"
	errors "github.com/teranos/sacred-error"
)

// releases is where Anthropic serves Claude Code, a binary per version and
// platform. Nothing of it is carried in a QNTX release.
const releases = "https://downloads.claude.ai/claude-code-releases"

// A Pin names one Claude Code exactly: the version, where it is served, and
// the hash of its binary per platform.
type Pin struct {
	Base      string
	Version   string
	Checksums map[string]string
}

// Pinned is the pin compiled into this build: the one parity holds.
func Pinned() (Pin, error) {
	version, checksums, err := parity.ClaudeCode()
	if err != nil {
		return Pin{}, err
	}
	return Pin{Base: releases, Version: version, Checksums: checksums}, nil
}

// Platform is this node's platform as the release host names it.
func Platform() (string, error) {
	arch, known := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	if !known || (runtime.GOOS != "linux" && runtime.GOOS != "darwin") {
		return "", errors.Newf("Claude Code is not pinned for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return runtime.GOOS + "-" + arch, nil
}

// Ensure is the pinned binary on this node, under dir: the one already there
// when its hash is the pin's, and fetched otherwise. A binary whose hash is
// not the pin's is never left where a caller would run it.
func (p Pin) Ensure(ctx context.Context, dir string) (string, error) {
	platform, err := Platform()
	if err != nil {
		return "", err
	}
	want, pinned := p.Checksums[platform]
	if !pinned {
		return "", errors.Newf("Claude Code %s has no pinned sha256 for %s", p.Version, platform)
	}

	target := filepath.Join(dir, p.Version, "claude")
	if have, err := sha256File(target); err == nil && have == want {
		return target, nil
	}

	url := p.Base + "/" + p.Version + "/" + platform + "/claude"
	if err := fetch(ctx, url, want, target); err != nil {
		return "", err
	}
	return target, nil
}

// fetch downloads url to target, and keeps it only when its hash is want.
func fetch(ctx context.Context, url, want, target string) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return errors.Wrapf(err, "could not ask for %s", url)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrapf(err, "could not fetch %s", url)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = errors.Wrapf(closeErr, "the response from %s did not close", url)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return errors.Newf("%s answered %s", url, resp.Status)
	}

	// Downloaded beside where it will live and renamed, so a download cut
	// short is never a binary at the path a caller runs.
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return errors.Wrapf(err, "could not create %s", filepath.Dir(target))
	}
	partial, err := os.CreateTemp(filepath.Dir(target), "claude.partial-*")
	if err != nil {
		return errors.Wrapf(err, "could not create a download file in %s", filepath.Dir(target))
	}
	kept := false
	defer func() {
		if kept {
			return
		}
		if rmErr := os.Remove(partial.Name()); rmErr != nil && err == nil {
			err = errors.Wrapf(rmErr, "could not remove %s", partial.Name())
		}
		// The version directory was made for this download and holds nothing else.
		if rmErr := os.Remove(filepath.Dir(target)); rmErr != nil && !os.IsExist(rmErr) && !os.IsNotExist(rmErr) && err == nil {
			err = errors.Wrapf(rmErr, "could not remove %s", filepath.Dir(target))
		}
	}()

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(partial, hash), resp.Body); err != nil {
		return sqlclose.With(errors.Wrapf(err, "the download of %s was cut short", url), partial.Close(), partial.Name())
	}
	if err := partial.Close(); err != nil {
		return errors.Wrapf(err, "could not close %s", partial.Name())
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return errors.Newf("%s has sha256 %s, and the pin is %s", url, got, want)
	}
	if err := os.Chmod(partial.Name(), 0o755); err != nil {
		return errors.Wrapf(err, "could not make %s executable", partial.Name())
	}
	if err := os.Rename(partial.Name(), target); err != nil {
		return errors.Wrapf(err, "could not move %s into place at %s", partial.Name(), target)
	}
	kept = true
	return nil
}

// sha256File is the hash of the file at path.
func sha256File(path string) (_ string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
