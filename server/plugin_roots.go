package server

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// A plugin the node builds loads from the Nix store: its program loader and
// libraries are store paths nothing else roots. The box collects its store at
// every deploy, and on 2026-10-05 that took datapunt's glibc from under it.

// storePrefix is where every store path starts.
const storePrefix = "/nix/store/"

// storeHashLen is how long a store path's hash is, in Nix's base32.
const storeHashLen = 32

func storeHashByte(c byte) bool {
	return strings.IndexByte("0123456789abcdfghijklmnpqrsvwxyz", c) >= 0
}

func storeNameByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.IndexByte("+-._?=", c) >= 0
}

// storePathsIn is every store path a file names, each once, as the directory
// directly under the store.
func storePathsIn(data []byte) []string {
	var paths []string
	seen := map[string]bool{}
	for from := 0; ; {
		at := bytes.Index(data[from:], []byte(storePrefix))
		if at < 0 {
			return paths
		}
		start := from + at
		hash := start + len(storePrefix)
		from = hash
		if hash+storeHashLen+1 >= len(data) || data[hash+storeHashLen] != '-' {
			continue
		}
		valid := true
		for _, c := range data[hash : hash+storeHashLen] {
			if !storeHashByte(c) {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		end := hash + storeHashLen + 1
		for end < len(data) && storeNameByte(data[end]) {
			end++
		}
		if end == hash+storeHashLen+1 {
			continue
		}
		path := string(data[start:end])
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
		from = end
	}
}

// rootStorePaths keeps every store path binary names alive with a root under
// roots, and fetches any a collection already took. It says which it fetched.
func rootStorePaths(ctx context.Context, nixStore, binary, roots string) ([]string, error) {
	data, err := os.ReadFile(binary)
	if err != nil {
		return nil, errors.Wrapf(err, "%s did not read", binary)
	}
	if err := os.MkdirAll(roots, 0o755); err != nil {
		return nil, errors.Wrapf(err, "could not create %s", roots)
	}
	var fetched []string
	for _, path := range storePathsIn(data) {
		_, missing := os.Stat(path)
		if _, err := runBuild(ctx, roots, nil, nixStore, "--realise", path, "--add-root", filepath.Join(roots, filepath.Base(path))); err != nil {
			return fetched, errors.Wrapf(err, "%s names %s, and it was not kept", binary, path)
		}
		if missing != nil {
			fetched = append(fetched, path)
		}
	}
	return fetched, nil
}

// storeRootsDir is beside a plugin's binary: the roots of what it loads from.
const storeRootsDir = "store-roots"

// keepStoreAlive roots what name's installed build loads from, and starts it
// again when something it loads from had been collected.
func (s *QNTXServer) keepStoreAlive(ctx context.Context, name string, logger *zap.SugaredLogger) {
	dir, err := grpcplugin.PluginInstallPath(name)
	if err != nil {
		logger.Warnw("A plugin's install path was not resolved, so what it loads from is not rooted", "plugin", name, "error", err)
		return
	}
	binary := filepath.Join(dir, grpcplugin.PluginBinaryName(name))
	if _, err := os.Stat(binary); err != nil {
		return
	}
	fetched, err := rootStorePaths(ctx, filepath.Join(nixBin, "nix-store"), binary, filepath.Join(dir, storeRootsDir))
	if err != nil {
		logger.Errorw("What a plugin loads from was not kept, so a collection of the store can stop it", "plugin", name, "error", err)
		return
	}
	if len(fetched) > 0 {
		logger.Infow("What a plugin loads from had been collected, and is fetched and kept again", "plugin", name, "fetched", fetched)
		s.buildLanded(name)
	}
}
