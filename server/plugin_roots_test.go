package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	glibcPath = "/nix/store/5m9amsvvh2z8sl7jrnc87hzy21glw6k1-glibc-2.40-66"
	gccPath   = "/nix/store/0aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-gcc-14.2.1-lib"
)

// A binary names its loader and its libraries by store path, among other
// bytes, and each is read once as the directory directly under the store.
func TestStorePathsInAreReadAsTheDirectoryUnderTheStore(t *testing.T) {
	binary := []byte("\x7fELF\x00" + glibcPath + "/lib/ld-linux-x86-64.so.2\x00" +
		gccPath + "/lib\x00" + glibcPath + "/lib\x00" +
		"/nix/store/tooshort-x\x00/nix/store/\x00")
	assert.Equal(t, []string{glibcPath, gccPath}, storePathsIn(binary))
}

// nixStoreStandIn stands in for nix-store: it writes down each run, and makes
// the path it realises exist, as substituting it would.
func nixStoreStandIn(t *testing.T) (nixStore, ran string) {
	t.Helper()
	dir := t.TempDir()
	ran = filepath.Join(dir, "ran")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + ran + "'\n"
	nixStore = filepath.Join(dir, "nix-store")
	require.NoError(t, os.WriteFile(nixStore, []byte(script), 0o755))
	return nixStore, ran
}

// Every store path a plugin loads from gets a root beside it, and one a
// collection took is fetched again and said to have been.
func TestWhatAPluginLoadsFromIsRootedAndFetchedAgain(t *testing.T) {
	nixStore, ran := nixStoreStandIn(t)
	binary := filepath.Join(t.TempDir(), "qntx-datapunt-plugin")
	require.NoError(t, os.WriteFile(binary, []byte("\x7fELF"+glibcPath+"/lib/ld-linux-x86-64.so.2\x00"), 0o755))
	roots := filepath.Join(t.TempDir(), storeRootsDir)

	fetched, err := rootStorePaths(context.Background(), nixStore, binary, roots)
	require.NoError(t, err)
	assert.Equal(t, []string{glibcPath}, fetched, "the collected glibc is not said to be fetched")

	raw, err := os.ReadFile(ran)
	require.NoError(t, err)
	said := strings.TrimSpace(string(raw))
	assert.Equal(t, "--realise "+glibcPath+" --add-root "+filepath.Join(roots, filepath.Base(glibcPath)), said)
}
