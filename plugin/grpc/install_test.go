package grpc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func testLogger(t *testing.T) *zap.SugaredLogger {
	t.Helper()
	return zap.NewNop().Sugar()
}

func TestPluginBinaryName(t *testing.T) {
	if got := PluginBinaryName("duif"); got != "qntx-duif-plugin" {
		t.Errorf("PluginBinaryName(duif) = %q, want qntx-duif-plugin", got)
	}
}

func TestExtractArchive(t *testing.T) {
	want := []byte("\x7fELF pretend this is a plugin")
	archive := tarGz(t, map[string][]byte{
		"README.md":                  []byte("docs"),
		"dist/qntx-duif-plugin":      want,
		"dist/qntx-duif-plugin.dSYM": []byte("debug symbols"),
	})

	dir := t.TempDir()
	binary, files, err := extractArchive(archive, dir, "qntx-duif-plugin")
	if err != nil {
		t.Fatalf("extractArchive = %v", err)
	}

	if got := filepath.Join(dir, "dist", "qntx-duif-plugin"); binary != got {
		t.Errorf("extractArchive returned %q, want %q", binary, got)
	}
	if files != 3 {
		t.Errorf("extractArchive wrote %d files, want 3", files)
	}

	got, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("extracted binary = %q, want %q", got, want)
	}
}

// The layout is what makes an $ORIGIN-relative RPATH resolve, so a library
// shipped beside the binary has to land beside the binary.
func TestExtractArchivePreservesLayout(t *testing.T) {
	archive := tarGz(t, map[string][]byte{
		"qntx-duif-plugin":  []byte("\x7fELF"),
		"lib/libvmime.so.1": []byte("shared object"),
	})

	dir := t.TempDir()
	binary, _, err := extractArchive(archive, dir, "qntx-duif-plugin")
	if err != nil {
		t.Fatalf("extractArchive = %v", err)
	}

	lib := filepath.Join(filepath.Dir(binary), "lib", "libvmime.so.1")
	if _, err := os.Stat(lib); err != nil {
		t.Errorf("library did not land beside the binary: %v", err)
	}
}

func TestExtractArchiveBinaryIsExecutable(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("\x7fELF")})

	binary, _, err := extractArchive(archive, t.TempDir(), "qntx-duif-plugin")
	if err != nil {
		t.Fatalf("extractArchive = %v", err)
	}

	info, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("stat extracted binary: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("extracted binary is not executable: mode %v", info.Mode())
	}
}

func TestExtractArchiveMissing(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"README.md": []byte("docs")})

	_, _, err := extractArchive(archive, t.TempDir(), "qntx-duif-plugin")
	if err == nil {
		t.Fatal("extractArchive accepted an archive with no plugin binary")
	}
	// The error lists what was there, so the operator can see what shipped.
	if !strings.Contains(err.Error(), "README.md") {
		t.Errorf("error does not list the archive contents: %v", err)
	}
}

// Entry paths arrive from a downloaded archive. Nothing may be written outside
// the plugin's own directory.
func TestExtractArchiveRejectsEscapingPaths(t *testing.T) {
	for _, name := range []string{"../evil", "dist/../../evil", "/etc/evil"} {
		t.Run(name, func(t *testing.T) {
			archive := tarGz(t, map[string][]byte{
				name:               []byte("owned"),
				"qntx-duif-plugin": []byte("\x7fELF"),
			})

			dir := t.TempDir()
			if _, _, err := extractArchive(archive, dir, "qntx-duif-plugin"); err == nil {
				t.Fatalf("extractArchive accepted entry %q", name)
			}

			outside := filepath.Join(filepath.Dir(dir), "evil")
			if _, err := os.Stat(outside); err == nil {
				t.Errorf("entry %q wrote outside the plugin directory", name)
			}
		})
	}
}

func TestInstallIsExecutableAndReplaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugins", "duif")

	first := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("first")})
	binary, _, err := install(first, dir, "qntx-duif-plugin")
	if err != nil {
		t.Fatalf("install = %v", err)
	}

	info, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("stat %s = %v", binary, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("installed binary %s is not executable (mode %v)", binary, info.Mode())
	}

	// A read-only previous install must not block a replacement.
	if err := os.Chmod(binary, 0o555); err != nil {
		t.Fatalf("chmod = %v", err)
	}
	second := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("second")})
	binary, _, err = install(second, dir, "qntx-duif-plugin")
	if err != nil {
		t.Fatalf("install over an existing read-only binary = %v", err)
	}

	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("read %s = %v", binary, err)
	}
	if string(data) != "second" {
		t.Errorf("installed content = %q, want %q", data, "second")
	}

	// Nothing partial may be left behind.
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatalf("readdir = %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".partial-") {
			t.Errorf("install left a partial directory behind: %s", entry.Name())
		}
	}
}

// A replacement must not leave the previous install's files behind: a stale
// library beside a new binary is the failure this whole layout exists to avoid.
func TestInstallReplacesWholeTree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugins", "duif")

	withLib := tarGz(t, map[string][]byte{
		"qntx-duif-plugin": []byte("\x7fELF"),
		"lib/libold.so":    []byte("old"),
	})
	if _, _, err := install(withLib, dir, "qntx-duif-plugin"); err != nil {
		t.Fatalf("install = %v", err)
	}

	withoutLib := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("\x7fELF")})
	if _, _, err := install(withoutLib, dir, "qntx-duif-plugin"); err != nil {
		t.Fatalf("install = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "lib", "libold.so")); err == nil {
		t.Error("a library from the previous install survived the replacement")
	}
}

// An install records what it installed. Without this an install is permanent:
// nothing can tell a current plugin from a stale one.
func TestInstallDigestIsRecordedAndRead(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugins", "duif")
	archive := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("\x7fELF")})

	if _, _, err := install(archive, dir, "qntx-duif-plugin"); err != nil {
		t.Fatalf("install = %v", err)
	}

	if _, ok := installedDigest(dir); ok {
		t.Fatal("installedDigest read a digest that install had not recorded yet")
	}

	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	if err := recordInstalledDigest(dir, digest); err != nil {
		t.Fatalf("recordInstalledDigest = %v", err)
	}

	got, ok := installedDigest(dir)
	if !ok {
		t.Fatal("installedDigest found no digest after one was recorded")
	}
	if got != digest {
		t.Errorf("installedDigest = %q, want %q", got, digest)
	}
}

// A replacement rewrites the record. Reading a previous install's digest would
// make an up-to-date plugin look stale on every start.
func TestInstallDigestDoesNotSurviveReplacement(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugins", "duif")

	first := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("first")})
	if _, _, err := install(first, dir, "qntx-duif-plugin"); err != nil {
		t.Fatalf("install = %v", err)
	}
	if err := recordInstalledDigest(dir, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("recordInstalledDigest = %v", err)
	}

	second := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("second")})
	if _, _, err := install(second, dir, "qntx-duif-plugin"); err != nil {
		t.Fatalf("install = %v", err)
	}

	if got, ok := installedDigest(dir); ok {
		t.Errorf("a replacement kept the previous digest %q", got)
	}
}

// Anything that is not a full-length hex digest is treated as no record at all,
// so a truncated write cannot be compared against a published digest.
func TestInstalledDigestRejectsMalformedRecord(t *testing.T) {
	for _, content := range []string{"", "  ", "not-a-digest", strings.Repeat("a", 63)} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, installedDigestFile), []byte(content), 0o644); err != nil {
			t.Fatalf("write digest file: %v", err)
		}

		if got, ok := installedDigest(dir); ok {
			t.Errorf("installedDigest accepted %q as %q", content, got)
		}
	}
}

// tarGz builds a .tar.gz holding the given paths.
func tarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, content := range files {
		header := &tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("write tar header for %s: %v", name, err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("write tar body for %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	return buf.Bytes()
}
