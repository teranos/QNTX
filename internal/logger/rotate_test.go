package logger

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func sizeOf(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Size()
}

// Past its most the file becomes its .1 and starts again; nothing written is lost.
func TestTheLogFileRotatesPastItsMost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qntx.log")
	r, err := openRotating(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	line := bytes.Repeat([]byte("x"), 30)
	for range 4 {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if got := sizeOf(t, path+".1"); got != 90 {
		t.Errorf("the rotated file holds %d bytes, want 90", got)
	}
	if got := sizeOf(t, path); got != 30 {
		t.Errorf("the log file holds %d bytes, want 30", got)
	}
}

// A file already past its most when the node starts is moved aside before anything is written.
func TestALogFileAlreadyPastItsMostIsMovedAsideAtOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qntx.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("o"), 500), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := openRotating(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.WriteString("new\n"); err != nil {
		t.Fatal(err)
	}
	if got := sizeOf(t, path+".1"); got != 500 {
		t.Errorf("the old file was not moved aside whole: %d bytes", got)
	}
	if got := sizeOf(t, path); got != 4 {
		t.Errorf("the log file holds %d bytes, want 4", got)
	}
}

// However much is written, the file and its .1 together stay within twice its most.
func TestTheLogFileStaysBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "qntx.log")
	r, err := openRotating(path, 1000)
	if err != nil {
		t.Fatal(err)
	}
	line := bytes.Repeat([]byte("y"), 99)
	for range 1000 {
		if _, err := r.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if total := sizeOf(t, path) + sizeOf(t, path+".1"); total > 2000 {
		t.Errorf("the log file and its .1 hold %d bytes, past twice its most", total)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("rotation left %d files, want the file and its .1", len(entries))
	}
}
