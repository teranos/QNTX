package server

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/errors"
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

// rootStorePaths keeps every store path binary (read as data) names alive with
// a root under roots, and fetches any a collection already took. It says which
// it fetched, and whether a collection had taken any.
func rootStorePaths(ctx context.Context, nixStore, binary string, data []byte, roots string) ([]string, bool, error) {
	if err := os.MkdirAll(roots, 0o755); err != nil {
		return nil, false, errors.Wrapf(err, "could not create %s", roots)
	}
	var fetched []string
	collected := false
	for _, path := range storePathsIn(data) {
		// A path a collection took is not there; one that does not stat for
		// another reason is not known to be either.
		_, statErr := os.Stat(path)
		taken := errors.Is(statErr, fs.ErrNotExist)
		if statErr != nil && !taken {
			return fetched, collected, errors.Wrapf(statErr, "%s names %s, and whether it is there did not read", binary, path)
		}
		if err := runBuild(ctx, roots, nil, nixStore, "--realise", path, "--add-root", filepath.Join(roots, filepath.Base(path))); err != nil {
			return fetched, collected, errors.Wrapf(err, "%s names %s, and it was not kept", binary, path)
		}
		if taken {
			fetched = append(fetched, path)
			collected = true
		}
	}
	return fetched, collected, nil
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
	data, err := os.ReadFile(binary)
	if errors.Is(err, fs.ErrNotExist) {
		// No build is installed, so nothing it loads from is there to root.
		return
	}
	if err != nil {
		logger.Errorw("A plugin's binary did not read, so what it loads from is not rooted", "plugin", name, "binary", binary, "error", err)
		return
	}
	fetched, collected, err := rootStorePaths(ctx, filepath.Join(nixBin, "nix-store"), binary, data, filepath.Join(dir, storeRootsDir))
	if err != nil {
		logger.Errorw("What a plugin loads from was not kept, so a collection of the store can stop it", "plugin", name, "error", err)
		return
	}
	if collected {
		logger.Infow("What a plugin loads from had been collected, and is fetched and kept again", "plugin", name, "fetched", fetched)
		s.buildLanded(name)
	}
}
