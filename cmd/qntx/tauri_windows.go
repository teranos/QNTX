package main

import (
	"os"
	"path/filepath"
)

func tauriBinaryName() string {
	return "QNTX.exe"
}

func tauriCandidates(dir string) []string {
	return []string{
		filepath.Join(dir, "QNTX.exe"),
	}
}

func platformTauriPaths() []string {
	var paths []string
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		paths = append(paths, filepath.Join(localAppData, "QNTX", "QNTX.exe"))
	}
	if programFiles := os.Getenv("PROGRAMFILES"); programFiles != "" {
		paths = append(paths, filepath.Join(programFiles, "QNTX", "QNTX.exe"))
	}
	return paths
}
