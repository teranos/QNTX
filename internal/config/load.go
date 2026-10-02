package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/viper"

	"github.com/teranos/errors"
)

var globalConfig *Config
var viperInstance *viper.Viper
var viperOnce sync.Once
var viperInitErr error
var projectConfigPath string // Path to the project-level config file (am.toml)

// ConfigSources tracks where each setting came from during loading
// Exported for use by introspection
var ConfigSources = make(map[string]SourceInfo)
var configSourcesMu sync.RWMutex

// Load reads the QNTX core configuration using Viper
func Load() (*Config, error) {
	if globalConfig != nil {
		return globalConfig, nil
	}

	v, err := initViper()
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize viper")
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal config")
	}

	globalConfig = &config
	return globalConfig, nil
}

// ProjectConfigPath returns the path to the project-level config file (am.toml).
// Empty if no project config was found.
func ProjectConfigPath() string {
	return projectConfigPath
}

// GetViper returns the Viper instance for advanced configuration access.
// Returns nil if initialization fails - callers should handle nil safely.
func GetViper() *viper.Viper {
	v, err := initViper()
	if v == nil {
		reportViperInitErr("(GetViper)", err)
	}
	return v
}

// LoadWithViper loads configuration using a provided Viper instance
func LoadWithViper(v *viper.Viper) (*Config, error) {
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal config")
	}
	return &config, nil
}

// LoadFromFile loads configuration from a specific file path
func LoadFromFile(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("toml")

	// Set defaults but don't bind environment variables for this specific load
	SetDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, errors.Wrapf(err, "failed to read config file %s", configPath)
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal config from %s", configPath)
	}

	return &config, nil
}

// Reset clears the cached configuration (useful for testing)
func Reset() {
	globalConfig = nil
	viperInstance = nil
	viperOnce = sync.Once{}
	viperInitErr = nil
	projectConfigPath = ""
	configSourcesMu.Lock()
	ConfigSources = make(map[string]SourceInfo)
	configSourcesMu.Unlock()
}

// initViper initializes Viper with configuration sources and defaults
func initViper() (*viper.Viper, error) {
	viperOnce.Do(func() {
		v := viper.New()

		// Set up environment variable binding
		v.SetEnvPrefix("QNTX")
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		v.AutomaticEnv()

		// Bind specific sensitive configuration values to environment variables
		BindSensitiveEnvVars(v)

		// Set defaults first
		SetDefaults(v)

		// Manually merge configs in precedence order: system -> user -> project -> env vars
		if err := mergeConfigFiles(v); err != nil {
			viperInitErr = err
			return
		}

		viperInstance = v
	})
	return viperInstance, viperInitErr
}

// findProjectConfig searches for config.toml or am.toml by walking up the directory tree
// Returns the path to the first config file found, or empty string if none found
// Preference order: am.toml > config.toml (for backward compatibility)
func findProjectConfig() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Walk up the directory tree looking for config files
	for {
		// Check for am.toml first (new format)
		amPath := filepath.Join(dir, "am.toml")
		if _, err := os.Stat(amPath); err == nil {
			return amPath
		}

		// Fall back to config.toml (backward compatibility)
		configPath := filepath.Join(dir, "config.toml")
		if _, err := os.Stat(configPath); err == nil {
			return configPath
		}

		// Move to parent directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root, stop searching
			break
		}
		dir = parent
	}

	return ""
}

// trackSource records where a configuration key came from
func trackSource(key string, source ConfigSource, path string) {
	configSourcesMu.Lock()
	ConfigSources[key] = SourceInfo{
		Source: source,
		Path:   path,
	}
	configSourcesMu.Unlock()
}

// TrackNestedSources recursively tracks sources for nested configuration
// Exported for testing
func TrackNestedSources(settings map[string]any, prefix string, source ConfigSource, path string) {
	for key, value := range settings {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		// Track this key's source
		trackSource(fullKey, source, path)

		// If nested map, recurse
		if nestedMap, ok := value.(map[string]any); ok {
			TrackNestedSources(nestedMap, fullKey, source, path)
		}
	}
}

// mergeConfigFiles manually merges configuration files in the correct precedence order
// Precedence (lowest to highest): system < user < project < env vars
func mergeConfigFiles(v *viper.Viper) error {
	// A home that cannot be named would make qntxDir "/.qntx" — a path at
	// the filesystem root that was never this user's config directory.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(err, "cannot locate the user config directory: the home directory is unknown")
	}

	// Ensure ~/.qntx directory exists
	qntxDir := filepath.Join(homeDir, ".qntx")
	if err := os.MkdirAll(qntxDir, DefaultDirPermissions); err != nil {
		return errors.Wrapf(err, "failed to create config directory %s", qntxDir)
	}

	// Build config paths, with project config found via upward search
	projectConfig := findProjectConfig()

	// Define config files with their sources
	type configFile struct {
		path   string
		source ConfigSource
	}

	configFiles := []configFile{
		{"/etc/qntx/config.toml", SourceSystem},
		{"/etc/qntx/am.toml", SourceSystem},
		{filepath.Join(qntxDir, "config.toml"), SourceUser},
		{filepath.Join(qntxDir, "am.toml"), SourceUser},
	}

	// Add project config if found (highest file precedence, below env vars)
	if projectConfig != "" {
		configFiles = append(configFiles, configFile{projectConfig, SourceProject})
		projectConfigPath = projectConfig
	}

	// Track defaults first
	for key, value := range v.AllSettings() {
		trackSource(key, SourceDefault, "")
		// Track nested defaults
		if nestedMap, ok := value.(map[string]any); ok {
			TrackNestedSources(nestedMap, key, SourceDefault, "")
		}
	}

	// Process each config file and track sources
	for _, cf := range configFiles {
		if _, err := os.Stat(cf.path); err == nil {
			// Config file exists, merge it
			tempViper := viper.New()
			tempViper.SetConfigFile(cf.path)
			tempViper.SetConfigType("toml")

			if err := tempViper.ReadInConfig(); err == nil {
				// Track sources for all settings in this file
				allSettings := tempViper.AllSettings()
				TrackNestedSources(allSettings, "", cf.source, cf.path)

				// Merge this config into the main viper instance
				// Using MergeConfigMap preserves Viper's natural precedence order,
				// allowing environment variables to override config files properly
				if err := v.MergeConfigMap(allSettings); err != nil {
					return errors.Wrapf(err, "failed to merge config from %s", cf.path)
				}
			}
		}
	}

	// Track environment variable overrides
	// Check each setting to see if it was overridden by an env var
	configSourcesMu.RLock()
	keys := make([]string, 0, len(ConfigSources))
	for key := range ConfigSources {
		keys = append(keys, key)
	}
	configSourcesMu.RUnlock()
	for _, key := range keys {
		envKey := "QNTX_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		if envValue := os.Getenv(envKey); envValue != "" {
			// This setting was overridden by environment
			trackSource(key, SourceEnvironment, envKey)
		}
	}

	return nil
}

// The Get* accessors cannot return errors, and this package sits below the
// logger — a failed initialization reports to stderr, once.
var reportViperInitErrOnce sync.Once

func reportViperInitErr(key string, err error) {
	reportViperInitErrOnce.Do(func() {
		fmt.Fprintf(os.Stderr,
			"config: initialization failed; every lookup (first asked: %q) returns its zero value: %v\n",
			key, err)
	})
}

// Get returns a configuration value using dot notation
func Get(key string) any {
	v, err := initViper()
	if v == nil {
		reportViperInitErr(key, err)
		return nil
	}
	return v.Get(key)
}

// GetString returns a configuration value as string using dot notation
func GetString(key string) string {
	v, err := initViper()
	if v == nil {
		reportViperInitErr(key, err)
		return ""
	}
	return v.GetString(key)
}

// GetBool returns a configuration value as bool using dot notation
func GetBool(key string) bool {
	v, err := initViper()
	if v == nil {
		reportViperInitErr(key, err)
		return false
	}
	return v.GetBool(key)
}

// GetInt returns a configuration value as int using dot notation
func GetInt(key string) int {
	v, err := initViper()
	if v == nil {
		reportViperInitErr(key, err)
		return 0
	}
	return v.GetInt(key)
}

// GetFloat64 returns a configuration value as float64 using dot notation
func GetFloat64(key string) float64 {
	v, err := initViper()
	if v == nil {
		reportViperInitErr(key, err)
		return 0
	}
	return v.GetFloat64(key)
}

// GetStringSlice returns a configuration value as string slice using dot notation
func GetStringSlice(key string) []string {
	v, err := initViper()
	if v == nil {
		reportViperInitErr(key, err)
		return nil
	}
	return v.GetStringSlice(key)
}

// GetStringMapString returns a configuration value as a string map using dot notation
func GetStringMapString(key string) map[string]string {
	v, err := initViper()
	if v == nil {
		reportViperInitErr(key, err)
		return nil
	}
	return v.GetStringMapString(key)
}

// Set sets a configuration value using dot notation (runtime override)
func Set(key string, value any) {
	v, err := initViper()
	if v == nil {
		reportViperInitErr(key, err)
		return
	}
	v.Set(key, value)
}

// GetDatabasePath returns the configured database path
func GetDatabasePath() (string, error) {
	// Check for DB_PATH environment variable first (for dev mode override)
	if dbPath := os.Getenv("DB_PATH"); dbPath != "" {
		return dbPath, nil
	}

	config, err := Load()
	if err != nil {
		return "", err
	}
	return config.Storage.Sqlite.Path, nil
}

// GetServerConfig returns the server configuration
func GetServerConfig() (*ServerConfig, error) {
	config, err := Load()
	if err != nil {
		return nil, err
	}
	return &config.Server, nil
}
