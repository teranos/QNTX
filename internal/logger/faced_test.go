package logger

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// "spamming error will never wor"
// 「[Opus 5.5] Nothing outside this package raises an issue by Sentry's own hand: the」
// 「[Opus 5.5] next one that does fails here, and Faced is the way.」
func TestNothingButFacedRaisesAnIssue(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	captures := []string{"CaptureException(", "CaptureMessage(", "CaptureEvent("}
	var found []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "target", "vendor", "web":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.Dir(path) == filepath.Join(root, "internal", "logger") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, capture := range captures {
			if strings.Contains(string(body), capture) {
				rel, _ := filepath.Rel(root, path)
				found = append(found, rel+": "+capture)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Errorf("%s raises an issue around logger.Faced", f)
	}
}
