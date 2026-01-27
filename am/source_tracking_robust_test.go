package am

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserObservableBehavior tests only what users can actually see and rely on
func TestUserObservableBehavior(t *testing.T) {
	t.Run("Config from am.toml wins over config.toml", func(t *testing.T) {
		// Reset global state
		Reset()
		defer Reset()

		// Setup: Create both config files with conflicting values
		tempDir := t.TempDir()
		qntxDir := filepath.Join(tempDir, ".qntx")
		require.NoError(t, os.MkdirAll(qntxDir, 0755))

		// config.toml says database path is "old.db"
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "config.toml"),
			[]byte(`[database]
path = "old.db"`),
			0644,
		))

		// am.toml says database path is "new.db"
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "am.toml"),
			[]byte(`[database]
path = "new.db"`),
			0644,
		))

		os.Chdir(tempDir)
		os.Setenv("HOME", tempDir)
		defer os.Unsetenv("HOME")

		// Action: Load configuration
		cfg, err := Load()
		require.NoError(t, err)

		// Observable behavior: The loaded config uses the value from am.toml
		assert.Equal(t, "new.db", cfg.Database.Path)
	})

	t.Run("Introspection shows which file provided each setting", func(t *testing.T) {
		// Reset global state
		Reset()
		defer Reset()

		// Setup: Create files with non-overlapping settings
		tempDir := t.TempDir()
		qntxDir := filepath.Join(tempDir, ".qntx")
		require.NoError(t, os.MkdirAll(qntxDir, 0755))

		// config.toml only has server settings
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "config.toml"),
			[]byte(`[server]
log_level = "debug"`),
			0644,
		))

		// am.toml only has database settings
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "am.toml"),
			[]byte(`[database]
path = "data.db"`),
			0644,
		))

		os.Chdir(tempDir)
		os.Setenv("HOME", tempDir)
		defer os.Unsetenv("HOME")

		// Action: Load and introspect
		_, err := Load()
		require.NoError(t, err)

		intro, err := GetConfigIntrospection()
		require.NoError(t, err)

		// Observable behavior: Introspection tells us which file each setting came from
		var dbPathSource, logLevelSource string
		for _, setting := range intro.Settings {
			if setting.Key == "database.path" {
				dbPathSource = filepath.Base(setting.SourcePath)
			}
			if setting.Key == "server.log_level" {
				logLevelSource = filepath.Base(setting.SourcePath)
			}
		}

		// User can see that database.path came from am.toml
		assert.Equal(t, "am.toml", dbPathSource)
		// User can see that server.log_level came from config.toml
		assert.Equal(t, "config.toml", logLevelSource)
	})

	t.Run("Project config overrides user config", func(t *testing.T) {
		// Reset global state
		Reset()
		defer Reset()

		// Setup: User config in home directory
		homeDir := t.TempDir()
		userQntxDir := filepath.Join(homeDir, ".qntx")
		require.NoError(t, os.MkdirAll(userQntxDir, 0755))
		require.NoError(t, os.WriteFile(
			filepath.Join(userQntxDir, "am.toml"),
			[]byte(`[server]
port = 1111`),
			0644,
		))

		// Project config in working directory
		projectDir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(projectDir, "am.toml"),
			[]byte(`[server]
port = 2222`),
			0644,
		))

		os.Chdir(projectDir)
		os.Setenv("HOME", homeDir)
		defer os.Unsetenv("HOME")

		// Action: Load configuration
		cfg, err := Load()
		require.NoError(t, err)

		// Observable behavior: Project config wins
		require.NotNil(t, cfg.Server.Port)
		assert.Equal(t, 2222, *cfg.Server.Port)
	})

	t.Run("Introspection lists all active settings", func(t *testing.T) {
		// Reset global state
		Reset()
		defer Reset()

		// Setup: Simple config file
		tempDir := t.TempDir()
		qntxDir := filepath.Join(tempDir, ".qntx")
		require.NoError(t, os.MkdirAll(qntxDir, 0755))
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "am.toml"),
			[]byte(`[database]
path = "test.db"

[pulse]
workers = 3`),
			0644,
		))

		os.Chdir(tempDir)
		os.Setenv("HOME", tempDir)
		defer os.Unsetenv("HOME")

		// Action: Load and introspect
		cfg, err := Load()
		require.NoError(t, err)

		intro, err := GetConfigIntrospection()
		require.NoError(t, err)

		// Observable behavior: All settings appear in introspection
		settingsMap := make(map[string]interface{})
		for _, s := range intro.Settings {
			settingsMap[s.Key] = s.Value
		}

		// Settings from our file should be there
		assert.Equal(t, "test.db", settingsMap["database.path"])
		assert.Equal(t, int64(3), settingsMap["pulse.workers"])

		// Default settings should also be there (not just our overrides)
		assert.NotNil(t, settingsMap["pulse.cost_per_score_usd"], "Defaults should appear in introspection")

		// What we loaded should match what introspection reports
		assert.Equal(t, cfg.Database.Path, settingsMap["database.path"])
		assert.Equal(t, int64(cfg.Pulse.Workers), settingsMap["pulse.workers"])
	})
}
