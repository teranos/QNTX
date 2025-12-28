package am

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkSettingsFromSource(t *testing.T) {
	t.Run("Flat settings", func(t *testing.T) {
		settings := map[string]interface{}{
			"workers":               1,
			"daily_budget_usd":      3.0,
			"ticker_interval_seconds": 1,
		}

		sourceMap := make(map[string]SourceInfo)
		markSettingsFromSource(settings, "", SourceUser, "/home/user/.qntx/am.toml", sourceMap)

		assert.Len(t, sourceMap, 3)
		assert.Equal(t, SourceUser, sourceMap["workers"].Source)
		assert.Equal(t, "/home/user/.qntx/am.toml", sourceMap["workers"].Path)
	})

	t.Run("Nested settings", func(t *testing.T) {
		settings := map[string]interface{}{
			"pulse": map[string]interface{}{
				"workers":          1,
				"daily_budget_usd": 3.0,
			},
			"database": map[string]interface{}{
				"path": "qntx.db",
			},
		}

		sourceMap := make(map[string]SourceInfo)
		markSettingsFromSource(settings, "", SourceUser, "/test/am.toml", sourceMap)

		// Verify dotted keys are created correctly
		assert.Equal(t, SourceUser, sourceMap["pulse.workers"].Source)
		assert.Equal(t, SourceUser, sourceMap["pulse.daily_budget_usd"].Source)
		assert.Equal(t, SourceUser, sourceMap["database.path"].Source)

		// Verify all have correct source path
		assert.Equal(t, "/test/am.toml", sourceMap["pulse.workers"].Path)
	})

	t.Run("Deeply nested settings", func(t *testing.T) {
		settings := map[string]interface{}{
			"database": map[string]interface{}{
				"bounded_storage": map[string]interface{}{
					"actor_context_limit": 16,
				},
			},
		}

		sourceMap := make(map[string]SourceInfo)
		markSettingsFromSource(settings, "", SourceProject, "/project/am.toml", sourceMap)

		// Verify deep nesting creates correct dotted key
		info, exists := sourceMap["database.bounded_storage.actor_context_limit"]
		assert.True(t, exists)
		assert.Equal(t, SourceProject, info.Source)
		assert.Equal(t, "/project/am.toml", info.Path)
	})
}

func TestFlattenSettingsWithSources(t *testing.T) {
	t.Run("Basic flattening with source assignment", func(t *testing.T) {
		settings := map[string]interface{}{
			"pulse": map[string]interface{}{
				"workers":          1,
				"daily_budget_usd": 3.0,
			},
		}

		sourceMap := map[string]SourceInfo{
			"pulse.workers": {
				Source: SourceUser,
				Path:   "/home/user/.qntx/am.toml",
			},
			"pulse.daily_budget_usd": {
				Source: SourceUserUI,
				Path:   "/home/user/.qntx/am_from_ui.toml",
			},
		}

		introspection := &ConfigIntrospection{Settings: make([]SettingInfo, 0)}
		flattenSettingsWithSources(settings, "", introspection, sourceMap)

		assert.Len(t, introspection.Settings, 2)

		// Find specific settings
		var workersSetting, budgetSetting *SettingInfo
		for i := range introspection.Settings {
			if introspection.Settings[i].Key == "pulse.workers" {
				workersSetting = &introspection.Settings[i]
			}
			if introspection.Settings[i].Key == "pulse.daily_budget_usd" {
				budgetSetting = &introspection.Settings[i]
			}
		}

		require.NotNil(t, workersSetting)
		require.NotNil(t, budgetSetting)

		assert.Equal(t, SourceUser, workersSetting.Source)
		assert.Equal(t, 1, workersSetting.Value)

		assert.Equal(t, SourceUserUI, budgetSetting.Source)
		assert.Equal(t, 3.0, budgetSetting.Value)
	})

	t.Run("Environment variable override", func(t *testing.T) {
		// Set environment variable
		oldEnv := os.Getenv("QNTX_PULSE_WORKERS")
		defer os.Setenv("QNTX_PULSE_WORKERS", oldEnv)
		os.Setenv("QNTX_PULSE_WORKERS", "5")

		settings := map[string]interface{}{
			"pulse": map[string]interface{}{
				"workers": 1, // Config file value
			},
		}

		sourceMap := map[string]SourceInfo{
			"pulse.workers": {
				Source: SourceUser,
				Path:   "/home/user/.qntx/am.toml",
			},
		}

		introspection := &ConfigIntrospection{Settings: make([]SettingInfo, 0)}
		flattenSettingsWithSources(settings, "", introspection, sourceMap)

		require.Len(t, introspection.Settings, 1)
		setting := introspection.Settings[0]

		// Environment variable should override
		assert.Equal(t, SourceEnvironment, setting.Source)
		assert.Equal(t, "QNTX_PULSE_WORKERS", setting.SourcePath)
	})

	t.Run("Default source for unmapped settings", func(t *testing.T) {
		settings := map[string]interface{}{
			"pulse": map[string]interface{}{
				"workers": 1,
			},
		}

		// Empty source map - setting should get SourceDefault
		sourceMap := make(map[string]SourceInfo)

		introspection := &ConfigIntrospection{Settings: make([]SettingInfo, 0)}
		flattenSettingsWithSources(settings, "", introspection, sourceMap)

		require.Len(t, introspection.Settings, 1)
		setting := introspection.Settings[0]

		assert.Equal(t, SourceDefault, setting.Source)
		assert.Equal(t, "built-in default", setting.SourcePath)
	})
}

func TestBuildSourceMap(t *testing.T) {
	t.Run("Environment variable precedence", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "am.toml")

		// Create config file
		configContent := `
[pulse]
daily_budget_usd = 3.0
workers = 1
`
		err := os.WriteFile(configPath, []byte(configContent), 0644)
		require.NoError(t, err)

		// Set environment variable
		oldEnv := os.Getenv("QNTX_PULSE_DAILY_BUDGET_USD")
		defer os.Setenv("QNTX_PULSE_DAILY_BUDGET_USD", oldEnv)
		os.Setenv("QNTX_PULSE_DAILY_BUDGET_USD", "7.0")

		// Simulate what buildSourceMap does
		sourceMap := make(map[string]SourceInfo)

		settings := map[string]interface{}{
			"pulse": map[string]interface{}{
				"daily_budget_usd": 3.0,
				"workers":          1,
			},
		}

		markSettingsFromSource(settings, "", SourceUser, configPath, sourceMap)

		// Check for environment variable override
		for key := range sourceMap {
			envKey := "QNTX_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
			if os.Getenv(envKey) != "" {
				sourceMap[key] = SourceInfo{
					Source: SourceEnvironment,
					Path:   envKey,
				}
			}
		}

		// Verify environment variable overrode file
		assert.Equal(t, SourceEnvironment, sourceMap["pulse.daily_budget_usd"].Source)
		assert.Equal(t, "QNTX_PULSE_DAILY_BUDGET_USD", sourceMap["pulse.daily_budget_usd"].Path)

		// Verify non-env setting still has file source
		assert.Equal(t, SourceUser, sourceMap["pulse.workers"].Source)
		assert.Equal(t, configPath, sourceMap["pulse.workers"].Path)
	})
}

func TestConfigSourceConstants(t *testing.T) {
	// Verify source constants are correctly defined
	assert.Equal(t, ConfigSource("default"), SourceDefault)
	assert.Equal(t, ConfigSource("system"), SourceSystem)
	assert.Equal(t, ConfigSource("user"), SourceUser)
	assert.Equal(t, ConfigSource("user_ui"), SourceUserUI)
	assert.Equal(t, ConfigSource("project"), SourceProject)
	assert.Equal(t, ConfigSource("environment"), SourceEnvironment)
}

func TestGetConfigIntrospection(t *testing.T) {
	t.Run("Integration test with env var override", func(t *testing.T) {
		// Set environment variable to override a setting
		oldEnv := os.Getenv("QNTX_PULSE_TICKER_INTERVAL_SECONDS")
		defer os.Setenv("QNTX_PULSE_TICKER_INTERVAL_SECONDS", oldEnv)
		os.Setenv("QNTX_PULSE_TICKER_INTERVAL_SECONDS", "99")

		// Get introspection
		introspection, err := GetConfigIntrospection()
		require.NoError(t, err)
		require.NotNil(t, introspection)

		// Build map of settings for easier verification
		settingsByKey := make(map[string]SettingInfo)
		for _, setting := range introspection.Settings {
			settingsByKey[setting.Key] = setting
		}

		// Verify environment variable override is detected
		tickerSetting, ok := settingsByKey["pulse.ticker_interval_seconds"]
		require.True(t, ok, "pulse.ticker_interval_seconds should be in introspection")
		assert.Equal(t, SourceEnvironment, tickerSetting.Source)
		assert.Equal(t, "QNTX_PULSE_TICKER_INTERVAL_SECONDS", tickerSetting.SourcePath)

		// Verify introspection contains expected fields
		// Config file may be empty in test environment (that's okay)
		assert.NotNil(t, introspection)
		assert.NotEmpty(t, introspection.Settings, "Settings should not be empty")

		// Verify settings are in deterministic order (sorted keys)
		lastKey := ""
		for _, setting := range introspection.Settings {
			if lastKey != "" {
				assert.True(t, setting.Key >= lastKey,
					"Settings should be in sorted order: %s should be >= %s", setting.Key, lastKey)
			}
			lastKey = setting.Key
		}

		// Verify all sources are recognized ConfigSource values
		validSources := map[ConfigSource]bool{
			SourceDefault:     true,
			SourceSystem:      true,
			SourceUser:        true,
			SourceUserUI:      true,
			SourceProject:     true,
			SourceEnvironment: true,
		}
		for _, setting := range introspection.Settings {
			assert.True(t, validSources[setting.Source],
				"Setting %s has invalid source: %s", setting.Key, setting.Source)
		}
	})
}
