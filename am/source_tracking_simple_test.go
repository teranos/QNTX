package am

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBasicSourceTracking tests that basic source tracking works for defined config fields
func TestBasicSourceTracking(t *testing.T) {
	t.Run("am.toml vs config.toml precedence", func(t *testing.T) {
		// Reset global state
		Reset()
		defer Reset()

		// Create temp directory
		tempDir := t.TempDir()
		qntxDir := filepath.Join(tempDir, ".qntx")
		require.NoError(t, os.MkdirAll(qntxDir, 0755))

		// Create config.toml
		configToml := `
[database]
path = "config.db"

[server]
port = 8080
`
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "config.toml"),
			[]byte(configToml),
			0644,
		))

		// Create am.toml that overrides database.path
		amToml := `
[database]
path = "am.db"
`
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "am.toml"),
			[]byte(amToml),
			0644,
		))

		// Set environment
		os.Chdir(tempDir)
		os.Setenv("HOME", tempDir)
		defer os.Unsetenv("HOME")

		// Load configuration
		cfg, err := Load()
		require.NoError(t, err)

		// Verify am.toml won
		assert.Equal(t, "am.db", cfg.Database.Path, "am.toml should win over config.toml")

		// Verify source tracking
		assert.Equal(t, SourceUser, ConfigSources["database.path"].Source)
		assert.Contains(t, ConfigSources["database.path"].Path, "am.toml")

		// Verify server.port from config.toml is tracked
		assert.Equal(t, 8080, cfg.Server.Port)
		assert.Equal(t, SourceUser, ConfigSources["server.port"].Source)
		assert.Contains(t, ConfigSources["server.port"].Path, "config.toml")
	})

	t.Run("Default values are tracked", func(t *testing.T) {
		// Reset global state
		Reset()
		defer Reset()

		// Create empty temp directory (no configs)
		tempDir := t.TempDir()
		os.Chdir(tempDir)
		os.Setenv("HOME", tempDir)
		defer os.Unsetenv("HOME")

		// Load configuration (all defaults)
		cfg, err := Load()
		require.NoError(t, err)

		// Check a known default
		assert.Equal(t, 0.002, cfg.Pulse.CostPerScoreUSD)

		// Verify it's tracked as default
		source, exists := ConfigSources["pulse.cost_per_score_usd"]
		assert.True(t, exists, "Default should be tracked")
		assert.Equal(t, SourceDefault, source.Source)
		assert.Equal(t, "", source.Path, "Defaults have no path")
	})

	t.Run("Multiple files at same level", func(t *testing.T) {
		// Reset global state
		Reset()
		defer Reset()

		// Create temp directory
		tempDir := t.TempDir()
		qntxDir := filepath.Join(tempDir, ".qntx")
		require.NoError(t, os.MkdirAll(qntxDir, 0755))

		// Create config.toml with server settings
		configToml := `
[server]
log_level = "info"
`
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "config.toml"),
			[]byte(configToml),
			0644,
		))

		// Create am.toml with different settings
		amToml := `
[database]
path = "test.db"
`
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "am.toml"),
			[]byte(amToml),
			0644,
		))

		// Set environment
		os.Chdir(tempDir)
		os.Setenv("HOME", tempDir)
		defer os.Unsetenv("HOME")

		// Load configuration
		_, err := Load()
		require.NoError(t, err)

		// Verify each setting tracks to correct file
		dbSource := ConfigSources["database.path"]
		assert.Equal(t, SourceUser, dbSource.Source)
		assert.Contains(t, dbSource.Path, "am.toml")

		logSource := ConfigSources["server.log_level"]
		assert.Equal(t, SourceUser, logSource.Source)
		assert.Contains(t, logSource.Path, "config.toml")
	})
}

// TestIntrospectionConsistency verifies introspection matches loaded config
func TestIntrospectionConsistency(t *testing.T) {
	// Reset global state
	Reset()
	defer Reset()

	// Create temp directory with config
	tempDir := t.TempDir()
	qntxDir := filepath.Join(tempDir, ".qntx")
	require.NoError(t, os.MkdirAll(qntxDir, 0755))

	amToml := `
[database]
path = "introspect.db"

[pulse]
workers = 2
`
	require.NoError(t, os.WriteFile(
		filepath.Join(qntxDir, "am.toml"),
		[]byte(amToml),
		0644,
	))

	// Set environment
	os.Chdir(tempDir)
	os.Setenv("HOME", tempDir)
	defer os.Unsetenv("HOME")

	// Load configuration
	cfg, err := Load()
	require.NoError(t, err)

	// Get introspection
	intro, err := GetConfigIntrospection()
	require.NoError(t, err)

	// Build a map for easier lookup
	settings := make(map[string]*SettingInfo)
	for i := range intro.Settings {
		settings[intro.Settings[i].Key] = &intro.Settings[i]
	}

	// Verify database.path
	dbSetting := settings["database.path"]
	require.NotNil(t, dbSetting)
	assert.Equal(t, cfg.Database.Path, dbSetting.Value)
	assert.Equal(t, SourceUser, dbSetting.Source)
	assert.Contains(t, dbSetting.SourcePath, "am.toml")

	// Verify pulse.workers
	workerSetting := settings["pulse.workers"]
	require.NotNil(t, workerSetting)
	assert.Equal(t, cfg.Pulse.Workers, workerSetting.Value)
	assert.Equal(t, SourceUser, workerSetting.Source)
	assert.Contains(t, workerSetting.SourcePath, "am.toml")
}
