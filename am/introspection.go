package am

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// ConfigSource represents where a configuration value came from
type ConfigSource string

const (
	SourceDefault     ConfigSource = "default"
	SourceSystem      ConfigSource = "system"      // /etc/qntx/am.toml
	SourceUser        ConfigSource = "user"        // ~/.qntx/am.toml
	SourceUserUI      ConfigSource = "user_ui"     // ~/.qntx/am_from_ui.toml
	SourceProject     ConfigSource = "project"     // project am.toml
	SourceEnvironment ConfigSource = "environment" // QNTX_* env vars
)

// SettingInfo contains metadata about a configuration setting
type SettingInfo struct {
	Key        string       `json:"key"`
	Value      interface{}  `json:"value"`
	Source     ConfigSource `json:"source"`
	SourcePath string       `json:"source_path,omitempty"` // File path or env var name
}

// ConfigIntrospection provides metadata about the active configuration
type ConfigIntrospection struct {
	ConfigFile string        `json:"config_file"` // Path to active config file
	Settings   []SettingInfo `json:"settings"`    // All settings with sources
}

// GetConfigIntrospection returns detailed information about active configuration
// with accurate source tracking by reading each config file individually
func GetConfigIntrospection() (*ConfigIntrospection, error) {
	v := GetViper()

	introspection := &ConfigIntrospection{
		ConfigFile: v.ConfigFileUsed(),
		Settings:   make([]SettingInfo, 0),
	}

	// Build map of settings to their sources by reading each config file
	sourceMap := buildSourceMap()

	// Get all effective settings from merged Viper config
	allSettings := v.AllSettings()

	// Flatten nested settings and assign sources
	flattenSettingsWithSources(allSettings, "", introspection, sourceMap)

	return introspection, nil
}

// SourceInfo tracks where a configuration value originated
// Used internally for building configuration introspection data
type SourceInfo struct {
	Source ConfigSource // The type of config source (default, system, user, etc.)
	Path   string       // File path or environment variable name
}

// buildSourceMap reads each config file and builds a map of setting -> source info
// Precedence order (lowest to highest): System < User < User UI < Project < Environment
func buildSourceMap() map[string]SourceInfo {
	sourceMap := make(map[string]SourceInfo)

	// Get home directory for user config paths
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// If we can't get home directory, skip user/UI configs and use system/project only
		homeDir = ""
	}

	// Define config files in precedence order (lowest to highest)
	// Supports both am.toml (new) and config.toml (backward compat)
	configFiles := []struct {
		path   string
		source ConfigSource
	}{
		{"/etc/qntx/am.toml", SourceSystem},
		{"/etc/qntx/config.toml", SourceSystem},
		{filepath.Join(homeDir, ".qntx", "am.toml"), SourceUser},
		{filepath.Join(homeDir, ".qntx", "config.toml"), SourceUser},
		{filepath.Join(homeDir, ".qntx", "am_from_ui.toml"), SourceUserUI},
		{filepath.Join(homeDir, ".qntx", "config_from_ui.toml"), SourceUserUI},
		{findProjectConfig(), SourceProject},
	}

	// Read each config file and mark settings with their source
	for _, cf := range configFiles {
		if cf.path == "" {
			continue // Skip if path not found (e.g., no project config or no home dir)
		}

		if data, err := os.ReadFile(cf.path); err == nil {
			var settings map[string]interface{}
			if err := toml.Unmarshal(data, &settings); err == nil {
				// Flatten and mark all settings from this file
				markSettingsFromSource(settings, "", cf.source, cf.path, sourceMap)
			}
		}
	}

	// Note: Environment variable checks happen in flattenSettingsWithSources()
	// to ensure they override all file-based sources

	return sourceMap
}

// markSettingsFromSource recursively marks all settings from a config file with their source
func markSettingsFromSource(settings map[string]interface{}, prefix string, source ConfigSource, sourcePath string, sourceMap map[string]SourceInfo) {
	for key, value := range settings {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		// If nested map, recurse
		if nestedMap, ok := value.(map[string]interface{}); ok {
			markSettingsFromSource(nestedMap, fullKey, source, sourcePath, sourceMap)
		} else {
			// Mark this setting with its source (later files override earlier)
			sourceMap[fullKey] = SourceInfo{
				Source: source,
				Path:   sourcePath,
			}
		}
	}
}

// flattenSettingsWithSources flattens settings and assigns sources from sourceMap
func flattenSettingsWithSources(settings map[string]interface{}, prefix string, introspection *ConfigIntrospection, sourceMap map[string]SourceInfo) {
	// Sort keys for deterministic iteration
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := settings[key]
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		// Check if value is a nested map
		if nestedMap, ok := value.(map[string]interface{}); ok {
			flattenSettingsWithSources(nestedMap, fullKey, introspection, sourceMap)
			continue
		}

		// Get source from sourceMap, default to SourceDefault if not found
		sourceInfo := SourceInfo{Source: SourceDefault, Path: "built-in default"}
		if si, ok := sourceMap[fullKey]; ok {
			sourceInfo = si
		}

		// Check if environment variable overrides
		envKey := "QNTX_" + strings.ToUpper(strings.ReplaceAll(fullKey, ".", "_"))
		if envValue := os.Getenv(envKey); envValue != "" {
			sourceInfo = SourceInfo{
				Source: SourceEnvironment,
				Path:   envKey,
			}
		}

		introspection.Settings = append(introspection.Settings, SettingInfo{
			Key:        fullKey,
			Value:      value,
			Source:     sourceInfo.Source,
			SourcePath: sourceInfo.Path,
		})
	}
}

// GetConfigSummary returns a human-readable config summary
func GetConfigSummary() map[string]interface{} {
	v := GetViper()

	summary := map[string]interface{}{
		"config_file": v.ConfigFileUsed(),
		"sources": map[string]int{
			"environment": 0,
			"config_file": 0,
			"default":     0,
		},
	}

	// Count settings by source
	introspection, err := GetConfigIntrospection()
	if err != nil {
		return summary
	}

	sources := summary["sources"].(map[string]int)
	for _, setting := range introspection.Settings {
		sourceKey := string(setting.Source)
		sources[sourceKey]++ // Safe: initializes to 0 if not exists
	}

	return summary
}
