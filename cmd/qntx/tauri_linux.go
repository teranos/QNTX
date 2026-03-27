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
	}
}

func platformTauriPaths() []string {
	var paths []string
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".local", "bin", "QNTX"))
	}
	paths = append(paths, "/usr/local/bin/QNTX", "/usr/bin/QNTX")
	return paths
}
