package am

import (
	"os"
	"sort"
	"strings"

	"github.com/teranos/QNTX/errors"
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
// using the sources tracked during actual configuration loading
func GetConfigIntrospection() (*ConfigIntrospection, error) {
	v := GetViper()

	// If sources haven't been tracked yet, force a load
	if len(ConfigSources) == 0 {
		_, err := Load()
		if err != nil {
			return nil, errors.Wrap(err, "failed to load config for introspection")
		}
	}

	introspection := &ConfigIntrospection{
		ConfigFile: v.ConfigFileUsed(),
		Settings:   make([]SettingInfo, 0),
	}

	// Get all effective settings from merged Viper config
	allSettings := v.AllSettings()

	// Use the sources we tracked during loading (single source of truth!)
	// This ensures introspection matches exactly what was loaded
	flattenSettingsWithSources(allSettings, "", introspection, ConfigSources)

	return introspection, nil
}

// SourceInfo tracks where a configuration value originated
// Used internally for building configuration introspection data
type SourceInfo struct {
	Source ConfigSource // The type of config source (default, system, user, etc.)
	Path   string       // File path or environment variable name
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
