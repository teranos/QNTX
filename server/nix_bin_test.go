package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// k is NixOS: no default profile, its nix where the system keeps it. On
// 2026-10-19 its agent's ground was refused at the profile that is not there.
func TestANodesNixIsWhereItWasPut(t *testing.T) {
	profile, system := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(system, "nix"), []byte{}, 0o755))
	assert.Equal(t, system, nixHeldAt([]string{profile, system}))

	require.NoError(t, os.WriteFile(filepath.Join(profile, "nix"), []byte{}, 0o755))
	assert.Equal(t, profile, nixHeldAt([]string{profile, system}), "the installer's profile is asked first, where q and a Mac keep theirs")
}
