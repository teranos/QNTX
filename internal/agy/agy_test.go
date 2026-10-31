package agy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func archive(t *testing.T, name string, kind byte) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	body := "#!/bin/sh\necho agy test\n"
	h := &tar.Header{Name: name, Mode: 0755, Typeflag: kind}
	if kind == tar.TypeReg {
		h.Size = int64(len(body))
	}
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if h.Size > 0 {
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestEnsureVerifiesArchiveAndRepairsExecutable(t *testing.T) {
	platform, err := Platform()
	if err != nil {
		t.Fatal(err)
	}
	data := archive(t, "antigravity", tar.TypeReg)
	requests := 0
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/test/agy_cli_test.tar.gz" {
			t.Errorf("path: %s", r.URL.Path)
		}
		w.Write(data)
	}))
	defer host.Close()
	p := Pin{Base: host.URL, Version: "test", Artifacts: map[string]Artifact{platform: {File: "agy_cli_test.tar.gz", SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}}}
	dir := t.TempDir()
	binary, err := p.Ensure(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if binary != filepath.Join(dir, "test", "agy") {
		t.Fatalf("installed at %s", binary)
	}
	if err := os.WriteFile(binary, []byte("tampered"), 0700); err != nil {
		t.Fatal(err)
	}
	again, err := p.Ensure(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("agy test")) || requests != 1 {
		t.Fatalf("body=%q requests=%d", body, requests)
	}
	info, err := os.Stat(again)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0111 == 0 {
		t.Fatal("not executable")
	}
}

func TestEnsureRejectsUntrustedArchives(t *testing.T) {
	platform, err := Platform()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, entry string
		kind        byte
		badHash     bool
	}{
		{"digest", "antigravity", tar.TypeReg, true},
		{"traversal", "../antigravity", tar.TypeReg, false},
		{"symlink", "antigravity", tar.TypeSymlink, false},
		{"missing", "other", tar.TypeReg, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := archive(t, tc.entry, tc.kind)
			host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
			defer host.Close()
			hash := fmt.Sprintf("%x", sha256.Sum256(data))
			if tc.badHash {
				hash = strings.Repeat("0", 64)
			}
			p := Pin{Base: host.URL, Version: "test", Artifacts: map[string]Artifact{platform: {File: "agy_cli_test.tar.gz", SHA256: hash}}}
			dir := t.TempDir()
			if _, err := p.Ensure(context.Background(), dir); err == nil {
				t.Fatal("accepted archive")
			}
			if _, err := os.Stat(filepath.Join(dir, "test", "agy")); !os.IsNotExist(err) {
				t.Fatalf("published executable: %v", err)
			}
		})
	}
}

func TestPinnedUsesParity(t *testing.T) {
	p, err := Pinned()
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "1.4.4" || len(p.Artifacts) != 4 {
		t.Fatalf("pin: %+v", p)
	}
}

// Opt in with a downloaded release archive; no model credentials are used.
func TestReleaseArchive(t *testing.T) {
	archivePath := os.Getenv("QNTX_AGY_ARCHIVE")
	if archivePath == "" {
		t.Skip("set QNTX_AGY_ARCHIVE to validate the downloaded platform archive")
	}
	p, err := Pinned()
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, archivePath) }))
	defer host.Close()
	p.Base = host.URL
	binary, err := p.Ensure(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "--version")
	cmd.Env = []string{"HOME=" + t.TempDir(), updates}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) != p.Version {
		t.Fatalf("version: %s", output)
	}
}
