package codex

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
	body := "#!/bin/sh\necho codex-cli test\n"
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
	data := archive(t, "codex-test", tar.TypeReg)
	requests := 0
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/rust-vtest/codex-test.tar.gz" {
			t.Errorf("path: %s", r.URL.Path)
		}
		w.Write(data)
	}))
	defer host.Close()
	p := Pin{Base: host.URL, Version: "test", Artifacts: map[string]Artifact{platform: {File: "codex-test.tar.gz", SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}}}
	dir := t.TempDir()
	binary, err := p.Ensure(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
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
	if !bytes.Contains(body, []byte("codex-cli test")) || requests != 1 {
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

// "bubblewrap is unavailable: no system bwrap was found on PATH and no bundled
// codex-resources/bwrap binary was found next to the Codex executable"
// "failed to spawn code-mode host /home/kapteyn/.qntx/codex/0.163.0/codex-code-mode-host: No such file or directory (os error 2)"
func TestEnsureInstallsWhatCodexRunsBesideIt(t *testing.T) {
	platform, err := Platform()
	if err != nil {
		t.Fatal(err)
	}
	served := map[string][]byte{
		"/rust-vtest/codex-test.tar.gz": archive(t, "codex-test", tar.TypeReg),
		"/rust-vtest/bwrap-test.tar.gz": archive(t, "bwrap-test", tar.TypeReg),
		"/rust-vtest/host-test.tar.gz":  archive(t, "host-test", tar.TypeReg),
	}
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(served[r.URL.Path]) }))
	defer host.Close()
	sum := func(path string) string { return fmt.Sprintf("%x", sha256.Sum256(served[path])) }
	p := Pin{Base: host.URL, Version: "test", Artifacts: map[string]Artifact{platform: {
		File: "codex-test.tar.gz", SHA256: sum("/rust-vtest/codex-test.tar.gz"),
		Beside: []Beside{
			{Artifact: Artifact{File: "bwrap-test.tar.gz", SHA256: sum("/rust-vtest/bwrap-test.tar.gz")}, Path: "codex-resources/bwrap"},
			{Artifact: Artifact{File: "host-test.tar.gz", SHA256: sum("/rust-vtest/host-test.tar.gz")}, Path: "codex-code-mode-host"},
		},
	}}}
	binary, err := p.Ensure(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"codex-resources/bwrap", "codex-code-mode-host"} {
		info, err := os.Stat(filepath.Join(filepath.Dir(binary), path))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&0111 == 0 {
			t.Fatalf("%s is not executable", path)
		}
	}
}

// What the pin installs stays beside codex.
func TestEnsureRefusesToInstallOutsideItsDirectory(t *testing.T) {
	platform, err := Platform()
	if err != nil {
		t.Fatal(err)
	}
	data := archive(t, "codex-test", tar.TypeReg)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	defer host.Close()
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	p := Pin{Base: host.URL, Version: "test", Artifacts: map[string]Artifact{platform: {
		File: "codex-test.tar.gz", SHA256: sum,
		Beside: []Beside{{Artifact: Artifact{File: "codex-test.tar.gz", SHA256: sum}, Path: "../escaped"}},
	}}}
	dir := t.TempDir()
	if _, err := p.Ensure(context.Background(), dir); err == nil {
		t.Fatal("installed outside its directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("wrote outside: %v", err)
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
		{"digest", "codex-test", tar.TypeReg, true},
		{"traversal", "../codex-test", tar.TypeReg, false},
		{"symlink", "codex-test", tar.TypeSymlink, false},
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
			p := Pin{Base: host.URL, Version: "test", Artifacts: map[string]Artifact{platform: {File: "codex-test.tar.gz", SHA256: hash}}}
			dir := t.TempDir()
			if _, err := p.Ensure(context.Background(), dir); err == nil {
				t.Fatal("accepted archive")
			}
			if _, err := os.Stat(filepath.Join(dir, "test", "codex")); !os.IsNotExist(err) {
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
	if p.Version != "0.163.0" || len(p.Artifacts) != 4 {
		t.Fatalf("pin: %+v", p)
	}
}

// Opt in with a downloaded release archive; no model credentials are used.
func TestReleaseArchive(t *testing.T) {
	archivePath := os.Getenv("QNTX_CODEX_ARCHIVE")
	if archivePath == "" {
		t.Skip("set QNTX_CODEX_ARCHIVE to validate the downloaded platform archive")
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
	output, err := exec.Command(binary, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("version: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) != "codex-cli "+p.Version {
		t.Fatalf("version: %s", output)
	}
	// Clap must accept both invocation shapes without reaching model inference.
	for _, suffix := range [][]string{{"--help"}, {"resume", "--help"}} {
		args := append([]string{"exec", "--json", "--model", "test", "--sandbox", "read-only", "--skip-git-repo-check"}, suffix...)
		if output, err := exec.Command(binary, args...).CombinedOutput(); err != nil {
			t.Fatalf("args %q: %v: %s", args, err, output)
		}
	}
}
