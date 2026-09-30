package grpc

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestPackageBuildIsTheSameArchiveForTheSameBinary(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "out")
	if err := os.WriteFile(binary, []byte("a binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := PackageBuild("demo", binary)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PackageBuild("demo", binary)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("the same binary packaged twice gave two archives")
	}
}

func TestInstallBuildInstallsOnceAndSaysSo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logger := zap.NewNop().Sugar()
	binary := filepath.Join(t.TempDir(), "out")
	if err := os.WriteFile(binary, []byte("build one"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive, err := PackageBuild("demo", binary)
	if err != nil {
		t.Fatal(err)
	}

	changed, digest, err := InstallBuild("demo", archive, logger)
	if err != nil || !changed || digest == "" {
		t.Fatalf("first install: changed=%v digest=%q err=%v", changed, digest, err)
	}
	dir, err := PluginInstallPath("demo")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, PluginBinaryName("demo")))
	if err != nil || string(got) != "build one" {
		t.Fatalf("installed binary: %q, %v", got, err)
	}

	changed, again, err := InstallBuild("demo", archive, logger)
	if err != nil || changed || again != digest {
		t.Fatalf("the same build again: changed=%v digest=%q err=%v", changed, again, err)
	}

	if err := os.WriteFile(binary, []byte("build two"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive, err = PackageBuild("demo", binary)
	if err != nil {
		t.Fatal(err)
	}
	changed, _, err = InstallBuild("demo", archive, logger)
	if err != nil || !changed {
		t.Fatalf("a different build: changed=%v err=%v", changed, err)
	}
}
