package am

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/viper"

	"github.com/teranos/QNTX/errors"
)

var globalConfig *Config
var viperInstance *viper.Viper

// ConfigSources tracks where each setting came from during loading
// Exported for use by introspection
var ConfigSources = make(map[string]SourceInfo)

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

// GetViper returns the Viper instance for advanced configuration access.
// Returns nil if initialization fails - callers should handle nil safely.
func GetViper() *viper.Viper {
	v, _ := initViper()
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
}

// initViper initializes Viper with configuration sources and defaults
func initViper() (*viper.Viper, error) {
	if viperInstance != nil {
		return viperInstance, nil
	}

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
		return nil, err
	}

	viperInstance = v
	return v, nil
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
	ConfigSources[key] = SourceInfo{
		Source: source,
		Path:   path,
	}
}

// TrackNestedSources recursively tracks sources for nested configuration
// Exported for testing
func TrackNestedSources(settings map[string]interface{}, prefix string, source ConfigSource, path string) {
	for key, value := range settings {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		// Track this key's source
		trackSource(fullKey, source, path)

		// If nested map, recurse
		if nestedMap, ok := value.(map[string]interface{}); ok {
			TrackNestedSources(nestedMap, fullKey, source, path)
		}
	}
}

// mergeConfigFiles manually merges configuration files in the correct precedence order
// Precedence (lowest to highest): system < user < project < env vars
func mergeConfigFiles(v *viper.Viper) error {
	homeDir, _ := os.UserHomeDir()

	// Ensure ~/.qntx directory exists
	qntxDir := filepath.Join(homeDir, ".qntx")
	os.MkdirAll(qntxDir, DefaultDirPermissions)

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
		{filepath.Join(qntxDir, "config_from_ui.toml"), SourceUserUI},
		{filepath.Join(qntxDir, "am_from_ui.toml"), SourceUserUI},
	}

	// Add project config if found (highest file precedence, below env vars)
	if projectConfig != "" {
		configFiles = append(configFiles, configFile{projectConfig, SourceProject})
	}

	// Track defaults first
	for key, value := range v.AllSettings() {
		trackSource(key, SourceDefault, "")
		// Track nested defaults
		if nestedMap, ok := value.(map[string]interface{}); ok {
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
	for key := range ConfigSources {
		envKey := "QNTX_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		if envValue := os.Getenv(envKey); envValue != "" {
			// This setting was overridden by environment
			trackSource(key, SourceEnvironment, envKey)
		}
	}

	return nil
}

// LoadPluginConfigs loads plugin-specific configuration from ~/.qntx/plugins/{name}.toml files
// Config values are loaded under the plugin name namespace (e.g., python.python_paths)
// Returns nil if plugins directory doesn't exist (not an error), or actual errors encountered
func LoadPluginConfigs(pluginPaths []string) error {
	v, err := initViper()
	if err != nil {
		return errors.Wrap(err, "failed to initialize viper")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(err, "failed to get home directory")
	}

	pluginsDir := filepath.Join(homeDir, ".qntx", "plugins")

	// Check if plugins directory exists
	if _, err := os.Stat(pluginsDir); os.IsNotExist(err) {
		// Plugins directory doesn't exist yet - this is fine
		return nil
	}

	// Scan for plugin TOML files in the plugins directory
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		err = errors.Wrapf(err, "failed to read plugins directory")
		return errors.WithSafeDetails(err, "path=%s", pluginsDir)
	}

	var loadErrors []error

	for _, entry := range entries {
		// Skip directories and non-TOML files
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}

		pluginConfigPath := filepath.Join(pluginsDir, entry.Name())

		// Load the plugin TOML file
		tempViper := viper.New()
		tempViper.SetConfigFile(pluginConfigPath)
		tempViper.SetConfigType("toml")

		if err := tempViper.ReadInConfig(); err != nil {
			// Collect error but continue - one bad config shouldn't break all plugins
			wrappedErr := errors.Wrapf(err, "failed to read plugin config")
			wrappedErr = errors.WithSafeDetails(wrappedErr, "file=%s", entry.Name())
			wrappedErr = errors.WithHintf(wrappedErr, "check TOML syntax in %s", pluginConfigPath)
			loadErrors = append(loadErrors, wrappedErr)
			continue
		}

		// Extract plugin name from the TOML or filename
		pluginName := tempViper.GetString("name")
		if pluginName == "" {
			// Use filename without .toml extension as plugin name
			pluginName = strings.TrimSuffix(entry.Name(), ".toml")
		}

		// Get the [config] section if it exists
		configSection := tempViper.GetStringMap("config")
		if len(configSection) == 0 {
			// No [config] section - this is fine, plugin uses defaults
			continue
		}

		// Merge config values under plugin namespace (e.g., python.key)
		// Convert all values to strings for consistency with protobuf
		for key, value := range configSection {
			fullKey := pluginName + "." + key

			// Type assertion: ensure value can be converted to string
			var strValue string
			switch v := value.(type) {
			case string:
				strValue = v
			case int, int8, int16, int32, int64:
				strValue = fmt.Sprintf("%d", v)
			case float32, float64:
				strValue = fmt.Sprintf("%f", v)
			case bool:
				strValue = fmt.Sprintf("%t", v)
			default:
				// Complex types (arrays, maps) - serialize as JSON string
				if jsonBytes, err := json.Marshal(v); err == nil {
					strValue = string(jsonBytes)
				} else {
					typeErr := errors.Newf("unsupported config value type")
					typeErr = errors.WithSafeDetails(typeErr, "file=%s key=%s type=%T", entry.Name(), key, value)
					typeErr = errors.WithHint(typeErr, "config values must be strings, numbers, booleans, or JSON-serializable types")
					loadErrors = append(loadErrors, typeErr)
					continue
				}
			}

			v.Set(fullKey, strValue)
		}
	}

	// If there were errors, combine them
	if len(loadErrors) > 0 {
		baseErr := errors.Newf("%d plugin config(s) failed to load", len(loadErrors))
		for i, err := range loadErrors {
			baseErr = errors.Wrapf(err, "%s (error %d/%d)", baseErr.Error(), i+1, len(loadErrors))
		}
		return errors.WithHintf(baseErr, "fix plugin configuration files in %s", pluginsDir)
	}

	return nil
}

// Get returns a configuration value using dot notation
func Get(key string) interface{} {
	v, _ := initViper()
	if v == nil {
		return nil
	}
	return v.Get(key)
}

// GetString returns a configuration value as string using dot notation
func GetString(key string) string {
	v, _ := initViper()
	if v == nil {
		return ""
	}
	return v.GetString(key)
}

// GetBool returns a configuration value as bool using dot notation
func GetBool(key string) bool {
	v, _ := initViper()
	if v == nil {
		return false
	}
	return v.GetBool(key)
}

// GetInt returns a configuration value as int using dot notation
func GetInt(key string) int {
	v, _ := initViper()
	if v == nil {
		return 0
	}
	return v.GetInt(key)
}

// GetFloat64 returns a configuration value as float64 using dot notation
func GetFloat64(key string) float64 {
	v, _ := initViper()
	if v == nil {
		return 0
	}
	return v.GetFloat64(key)
}

// GetStringSlice returns a configuration value as string slice using dot notation
func GetStringSlice(key string) []string {
	v, _ := initViper()
	if v == nil {
		return nil
	}
	return v.GetStringSlice(key)
}

// Set sets a configuration value using dot notation (runtime override)
func Set(key string, value interface{}) {
	v, _ := initViper()
	if v != nil {
		v.Set(key, value)
	}
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
	return config.Database.Path, nil
}

// GetServerConfig returns the server configuration
func GetServerConfig() (*ServerConfig, error) {
	config, err := Load()
	if err != nil {
		return nil, err
	}
	return &config.Server, nil
}

// UpdatePluginConfig updates a plugin's runtime configuration.
// It writes the updated config to ~/.qntx/plugins/{pluginName}.toml and updates viper.
// If the plugin config file doesn't exist, it creates one with sensible defaults.
func UpdatePluginConfig(pluginName string, config map[string]string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.Wrap(err, "failed to get home directory")
	}

	pluginsDir := filepath.Join(homeDir, ".qntx", "plugins")
	configPath := filepath.Join(pluginsDir, pluginName+".toml")

	// Ensure plugins directory exists
	if err := os.MkdirAll(pluginsDir, DefaultDirPermissions); err != nil {
		err = errors.Wrapf(err, "failed to create plugins directory")
		return errors.WithSafeDetails(err, "path=%s", pluginsDir)
	}

	// Load existing config or create new one
	pluginConfig := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		// Parse existing config
		if err := toml.Unmarshal(data, &pluginConfig); err != nil {
			wrappedErr := errors.Wrapf(err, "failed to parse existing plugin config")
			wrappedErr = errors.WithSafeDetails(wrappedErr, "plugin=%s", pluginName)
			return errors.WithHintf(wrappedErr, "fix TOML syntax in %s or delete the file to recreate", configPath)
		}
	} else if !os.IsNotExist(err) {
		// Read error other than "not exist"
		return errors.Wrapf(err, "failed to read plugin config at %s", configPath)
	}

	// Ensure basic fields are set if creating new config
	if pluginConfig["name"] == nil {
		pluginConfig["name"] = pluginName
	}
	if pluginConfig["enabled"] == nil {
		pluginConfig["enabled"] = true
	}
	if pluginConfig["auto_start"] == nil {
		pluginConfig["auto_start"] = true
	}

	// Update [config] section
	pluginConfig["config"] = config

	// Write updated config to disk
	if err := writePluginConfigFile(configPath, pluginConfig); err != nil {
		return errors.Wrap(err, "failed to write plugin config")
	}

	// Update viper with new values
	v, err := initViper()
	if err != nil {
		return errors.Wrap(err, "failed to initialize viper")
	}
	for key, value := range config {
		fullKey := pluginName + "." + key
		v.Set(fullKey, value)
	}

	return nil
}

// WritePluginConfigToTemp writes plugin config to a temporary file for validation.
// Returns the temp file path on success.
func WritePluginConfigToTemp(pluginName string, config map[string]string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to get home directory")
	}

	pluginsDir := filepath.Join(homeDir, ".qntx", "plugins")
	configPath := filepath.Join(pluginsDir, pluginName+".toml")

	// Load existing config or create new one
	pluginConfig := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		if err := toml.Unmarshal(data, &pluginConfig); err != nil {
			wrappedErr := errors.Wrapf(err, "failed to parse existing plugin config")
			wrappedErr = errors.WithSafeDetails(wrappedErr, "plugin=%s", pluginName)
			return "", errors.WithHintf(wrappedErr, "fix TOML syntax in %s", configPath)
		}
	} else if !os.IsNotExist(err) {
		return "", errors.Wrapf(err, "failed to read plugin config at %s", configPath)
	}

	// Set defaults if missing
	if pluginConfig["name"] == nil {
		pluginConfig["name"] = pluginName
	}
	if pluginConfig["enabled"] == nil {
		pluginConfig["enabled"] = true
	}
	if pluginConfig["auto_start"] == nil {
		pluginConfig["auto_start"] = true
	}

	// Update [config] section
	pluginConfig["config"] = config

	// Create temp file
	tempFile, err := os.CreateTemp("", pluginName+"-*.toml")
	if err != nil {
		return "", errors.Wrapf(err, "failed to create temp file for plugin %s", pluginName)
	}
	tempPath := tempFile.Name()
	tempFile.Close()

	// Write to temp file
	if err := writePluginConfigFile(tempPath, pluginConfig); err != nil {
		os.Remove(tempPath)
		return "", err
	}

	return tempPath, nil
}

// writePluginConfigFile writes plugin configuration to a TOML file.
// Internal helper used by both UpdatePluginConfig and WritePluginConfigToTemp.
func writePluginConfigFile(path string, config map[string]interface{}) error {
	// Create a buffer to write TOML
	buf := &strings.Builder{}

	// Encode to TOML
	encoder := toml.NewEncoder(buf)
	if err := encoder.Encode(config); err != nil {
		wrappedErr := errors.Wrapf(err, "failed to encode plugin config as TOML")
		return errors.WithSafeDetails(wrappedErr, "path=%s", path)
	}

	// Write to file with safe permissions
	if err := os.WriteFile(path, []byte(buf.String()), DefaultFilePermissions); err != nil {
		wrappedErr := errors.Wrapf(err, "failed to write plugin config file")
		return errors.WithSafeDetails(wrappedErr, "path=%s", path)
	}

	return nil
}
