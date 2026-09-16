package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "thats an issue, fix now"
func TestEndingANamespaceRemovesWhatItsLandingFileKept(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "qntx-operational.db")
	path := landingPath(dbPath, "Pond")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	kept := []string{path, path + "-wal", path + "-shm", path + ".taken-in", path + ".flight.ThreadId(1)"}
	for _, file := range kept {
		require.NoError(t, os.WriteFile(file, []byte("x"), 0o640))
	}
	other := landingPath(dbPath, "harbour")
	require.NoError(t, os.WriteFile(other, []byte("x"), 0o640))

	require.NoError(t, removeLanding(dbPath, "pond"))

	for _, file := range kept {
		_, err := os.Stat(file)
		assert.True(t, os.IsNotExist(err), "%s is still there", file)
	}
	_, err := os.Stat(other)
	assert.NoError(t, err, "another namespace's landing file was removed")
}

func TestEndingANamespaceThatNeverOpenedHereIsNotAnError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "qntx-operational.db")
	assert.NoError(t, removeLanding(dbPath, "pond"))
}

func TestALandingFileIsNamedByTheSlug(t *testing.T) {
	assert.Equal(t, "/var/lib/qntx/namespaces/clean.db", landingPath("/var/lib/qntx/qntx-operational.db", "Clean"))
}
