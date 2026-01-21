package am

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSourceTrackingIntegration tests that configuration loading correctly tracks
// where each setting came from through the entire load -> introspection flow
func TestSourceTrackingIntegration(t *testing.T) {
	t.Run("Precedence: am.toml wins over config.toml", func(t *testing.T) {
		// Reset global state
		Reset()
		defer Reset()

		// Create temp directory structure
		tempDir := t.TempDir()
		qntxDir := filepath.Join(tempDir, ".qntx")
		require.NoError(t, os.MkdirAll(qntxDir, 0755))

		// Create config.toml with some settings
		configToml := `
[database]
path = "config.db"
max_connections = 10

[server]
port = 8080
log_level = "info"
`
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "config.toml"),
			[]byte(configToml),
			0644,
		))

		// Create am.toml with overlapping settings (should win)
		amToml := `
[database]
path = "am.db"

[plugin]
enabled = ["python", "code"]
`
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "am.toml"),
			[]byte(amToml),
			0644,
		))

		// Set environment to use our test directory
		originalWd, _ := os.Getwd()
		os.Chdir(tempDir)
		defer os.Chdir(originalWd)

		os.Setenv("HOME", tempDir)
		defer os.Unsetenv("HOME")

		// Load configuration through the real path
		cfg, err := Load()
		require.NoError(t, err)

		// Verify am.toml won for overlapping settings
		assert.Equal(t, "am.db", cfg.Database.Path, "am.toml should win over config.toml")

		// Get introspection to verify sources are tracked correctly
		intro, err := GetConfigIntrospection()
		require.NoError(t, err)

		// Find specific settings and verify their sources
		var dbPath, dbMaxConn, serverPort, pluginEnabled *SettingInfo
		for i := range intro.Settings {
			setting := &intro.Settings[i]
			t.Logf("Found setting: %s = %v (from %s)", setting.Key, setting.Value, setting.SourcePath)
			switch setting.Key {
			case "database.path":
				dbPath = setting
			case "database.max_connections":
				dbMaxConn = setting
			case "server.port":
				serverPort = setting
			case "plugin.enabled":
				pluginEnabled = setting
			}
		}

		// Verify database.path came from am.toml (it was in both files)
		require.NotNil(t, dbPath, "database.path should be in introspection")
		assert.Contains(t, dbPath.SourcePath, "am.toml", "database.path should come from am.toml")
		assert.Equal(t, "am.db", dbPath.Value)

		// Verify database.max_connections came from config.toml (only there)
		require.NotNil(t, dbMaxConn, "database.max_connections should be in introspection")
		assert.Contains(t, dbMaxConn.SourcePath, "config.toml", "database.max_connections should come from config.toml")
		assert.Equal(t, float64(10), dbMaxConn.Value) // Viper unmarshals numbers as float64

		// Verify server.port came from config.toml (only there)
		require.NotNil(t, serverPort, "server.port should be in introspection")
		assert.Contains(t, serverPort.SourcePath, "config.toml", "server.port should come from config.toml")

		// Verify plugin.enabled came from am.toml (only there)
		require.NotNil(t, pluginEnabled, "plugin.enabled should be in introspection")
		assert.Contains(t, pluginEnabled.SourcePath, "am.toml", "plugin.enabled should come from am.toml")
	})

	t.Run("Environment variables override files", func(t *testing.T) {
		// Reset global state
		Reset()
		defer Reset()

		// Create temp directory
		tempDir := t.TempDir()
		qntxDir := filepath.Join(tempDir, ".qntx")
		require.NoError(t, os.MkdirAll(qntxDir, 0755))

		// Create am.toml with database config
		amToml := `
[database]
path = "file.db"

[server]
port = 8080
`
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "am.toml"),
			[]byte(amToml),
			0644,
		))

		// Set environment variable to override database.path
		os.Setenv("QNTX_DATABASE_PATH", "env.db")
		defer os.Unsetenv("QNTX_DATABASE_PATH")

		// Set environment
		originalWd, _ := os.Getwd()
		os.Chdir(tempDir)
		defer os.Chdir(originalWd)

		os.Setenv("HOME", tempDir)
		defer os.Unsetenv("HOME")

		// Load configuration
		cfg, err := Load()
		require.NoError(t, err)

		// Verify environment variable won
		assert.Equal(t, "env.db", cfg.Database.Path, "Environment variable should override file")

		// Get introspection
		intro, err := GetConfigIntrospection()
		require.NoError(t, err)

		// Find database.path setting
		var dbPath *SettingInfo
		for i := range intro.Settings {
			if intro.Settings[i].Key == "database.path" {
				dbPath = &intro.Settings[i]
				break
			}
		}

		// Verify it shows as coming from environment
		require.NotNil(t, dbPath)
		assert.Equal(t, SourceEnvironment, dbPath.Source)
		assert.Equal(t, "QNTX_DATABASE_PATH", dbPath.SourcePath)
		assert.Equal(t, "env.db", dbPath.Value)
	})

	t.Run("Project config overrides user config", func(t *testing.T) {
		// Reset global state
		Reset()
		defer Reset()

		// Create temp home directory with user config
		homeDir := t.TempDir()
		userQntxDir := filepath.Join(homeDir, ".qntx")
		require.NoError(t, os.MkdirAll(userQntxDir, 0755))

		userConfig := `
[server]
port = 8080
log_level = "info"
`
		require.NoError(t, os.WriteFile(
			filepath.Join(userQntxDir, "am.toml"),
			[]byte(userConfig),
			0644,
		))

		// Create project directory with project config
		projectDir := t.TempDir()
		projectConfig := `
[server]
port = 9090
`
		require.NoError(t, os.WriteFile(
			filepath.Join(projectDir, "am.toml"),
			[]byte(projectConfig),
			0644,
		))

		// Set environment
		os.Chdir(projectDir)
		os.Setenv("HOME", homeDir)
		defer os.Unsetenv("HOME")

		// Load configuration
		cfg, err := Load()
		require.NoError(t, err)

		// Verify project config won for port
		assert.Equal(t, 9090, cfg.Server.Port, "Project config should override user config")

		// Get introspection
		intro, err := GetConfigIntrospection()
		require.NoError(t, err)

		// Find settings
		var serverPort, serverLogLevel *SettingInfo
		for i := range intro.Settings {
			setting := &intro.Settings[i]
			switch setting.Key {
			case "server.port":
				serverPort = setting
			case "server.log_level":
				serverLogLevel = setting
			}
		}

		// Verify port came from project
		require.NotNil(t, serverPort)
		assert.Equal(t, SourceProject, serverPort.Source)
		assert.Contains(t, serverPort.SourcePath, "am.toml")
		assert.Equal(t, float64(9090), serverPort.Value)

		// Verify log_level came from user (not in project)
		require.NotNil(t, serverLogLevel)
		assert.Equal(t, SourceUser, serverLogLevel.Source)
		assert.Equal(t, "info", serverLogLevel.Value)
	})

	t.Run("UI config files load with correct precedence", func(t *testing.T) {
		// Reset global state
		Reset()
		defer Reset()

		// Create temp directory
		tempDir := t.TempDir()
		qntxDir := filepath.Join(tempDir, ".qntx")
		require.NoError(t, os.MkdirAll(qntxDir, 0755))

		// Create user am.toml
		userConfig := `
[pulse]
workers = 2
daily_budget_usd = 5.0
`
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "am.toml"),
			[]byte(userConfig),
			0644,
		))

		// Create UI config that overrides some settings
		uiConfig := `
[pulse]
daily_budget_usd = 10.0
monthly_budget_usd = 300.0
`
		require.NoError(t, os.WriteFile(
			filepath.Join(qntxDir, "am_from_ui.toml"),
			[]byte(uiConfig),
			0644,
		))

		// Set environment
		originalWd, _ := os.Getwd()
		os.Chdir(tempDir)
		defer os.Chdir(originalWd)

		os.Setenv("HOME", tempDir)
		defer os.Unsetenv("HOME")

		// Load configuration
		_, err := Load()
		require.NoError(t, err)

		// Get introspection
		intro, err := GetConfigIntrospection()
		require.NoError(t, err)

		// Find settings
		settings := make(map[string]*SettingInfo)
		for i := range intro.Settings {
			setting := &intro.Settings[i]
			settings[setting.Key] = setting
		}

		// Verify workers came from user config (not in UI config)
		workers := settings["pulse.workers"]
		require.NotNil(t, workers)
		assert.Equal(t, SourceUser, workers.Source)
		assert.Contains(t, workers.SourcePath, "am.toml")
		assert.Equal(t, float64(2), workers.Value)

		// Verify daily_budget_usd came from UI config (overrode user)
		dailyBudget := settings["pulse.daily_budget_usd"]
		require.NotNil(t, dailyBudget)
		assert.Equal(t, SourceUserUI, dailyBudget.Source)
		assert.Contains(t, dailyBudget.SourcePath, "am_from_ui.toml")
		assert.Equal(t, float64(10), dailyBudget.Value)

		// Verify monthly_budget_usd came from UI config (only there)
		monthlyBudget := settings["pulse.monthly_budget_usd"]
		require.NotNil(t, monthlyBudget)
		assert.Equal(t, SourceUserUI, monthlyBudget.Source)
		assert.Contains(t, monthlyBudget.SourcePath, "am_from_ui.toml")
		assert.Equal(t, float64(300), monthlyBudget.Value)
	})

	t.Run("System config loads when present", func(t *testing.T) {
		// This test would require root access to write to /etc/qntx
		// We can test the logic by temporarily changing what counts as "system" config
		// But for now, skip if not root
		if os.Getuid() != 0 {
			t.Skip("Skipping system config test (requires root)")
		}
		// Would test /etc/qntx/am.toml and /etc/qntx/config.toml loading
	})
}

// TestSourceTrackingDefaults verifies that default values are properly tracked
func TestSourceTrackingDefaults(t *testing.T) {
	// Reset global state
	Reset()
	defer Reset()

	// Create empty temp directory (no config files)
	tempDir := t.TempDir()
	os.Chdir(tempDir)
	os.Setenv("HOME", tempDir)
	defer os.Unsetenv("HOME")

	// Load configuration (should use all defaults)
	_, err := Load()
	require.NoError(t, err)

	// Get introspection
	intro, err := GetConfigIntrospection()
	require.NoError(t, err)

	// Find a known default setting
	var pulseCost *SettingInfo
	for i := range intro.Settings {
		if intro.Settings[i].Key == "pulse.cost_per_score_usd" {
			pulseCost = &intro.Settings[i]
			break
		}
	}

	// Verify it's marked as default with no path
	require.NotNil(t, pulseCost, "Default pulse.cost_per_score_usd should be present")
	assert.Equal(t, SourceDefault, pulseCost.Source)
	assert.Equal(t, "", pulseCost.SourcePath, "Default values should have empty source path")
	assert.Equal(t, 0.002, pulseCost.Value, "Should have the default value")
}
