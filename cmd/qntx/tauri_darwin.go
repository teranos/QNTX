package main

import (
	"os"
	"path/filepath"
)

func tauriBinaryName() string {
	return "QNTX"
}

func tauriCandidates(dir string) []string {
	return []string{
		filepath.Join(dir, "QNTX"),
		filepath.Join(dir, "QNTX.app", "Contents", "MacOS", "QNTX"),
	}
}

func platformTauriPaths() []string {
	paths := []string{
		"/Applications/QNTX.app/Contents/MacOS/QNTX",
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, "Applications", "QNTX.app", "Contents", "MacOS", "QNTX"))
	}
	return paths
}
