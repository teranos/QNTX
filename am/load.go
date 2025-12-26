package am

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

var globalConfig *Config
var viperInstance *viper.Viper

// Load reads the QNTX core configuration using Viper
func Load() (*Config, error) {
	if globalConfig != nil {
		return globalConfig, nil
	}

	v := initViper()

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	globalConfig = &config
	return globalConfig, nil
}

// GetViper returns the Viper instance for advanced configuration access
func GetViper() *viper.Viper {
	return initViper()
}

// LoadWithViper loads configuration using a provided Viper instance
func LoadWithViper(v *viper.Viper) (*Config, error) {
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
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
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config from %s: %w", configPath, err)
	}

	return &config, nil
}

// Reset clears the cached configuration (useful for testing)
func Reset() {
	globalConfig = nil
	viperInstance = nil
}

// initViper initializes Viper with configuration sources and defaults
func initViper() *viper.Viper {
	if viperInstance != nil {
		return viperInstance
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
	mergeConfigFiles(v)

	viperInstance = v
	return v
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

// mergeConfigFiles manually merges configuration files in the correct precedence order
// Precedence (lowest to highest): system < user < project < env vars
func mergeConfigFiles(v *viper.Viper) {
	homeDir, _ := os.UserHomeDir()

	// Ensure ~/.qntx directory exists
	qntxDir := filepath.Join(homeDir, ".qntx")
	os.MkdirAll(qntxDir, DefaultDirPermissions)

	// Build config paths, with project config found via upward search
	projectConfig := findProjectConfig()
	configPaths := []string{
		"/etc/qntx/config.toml",              // System config (lowest precedence)
		filepath.Join(qntxDir, "am.toml"),    // User am config (new format)
		filepath.Join(qntxDir, "config.toml"), // User config (backward compat)
	}

	// Add project config if found (highest file precedence, below env vars)
	if projectConfig != "" {
		configPaths = append(configPaths, projectConfig)
	}

	for _, configPath := range configPaths {
		if _, err := os.Stat(configPath); err == nil {
			// Config file exists, merge it
			tempViper := viper.New()
			tempViper.SetConfigFile(configPath)
			tempViper.SetConfigType("toml")

			if err := tempViper.ReadInConfig(); err == nil {
				// Merge this config into the main viper instance
				for key, value := range tempViper.AllSettings() {
					v.Set(key, value)
				}
			}
		}
	}
}

// Get returns a configuration value using dot notation
func Get(key string) interface{} {
	v := initViper()
	return v.Get(key)
}

// GetString returns a configuration value as string using dot notation
func GetString(key string) string {
	v := initViper()
	return v.GetString(key)
}

// GetBool returns a configuration value as bool using dot notation
func GetBool(key string) bool {
	v := initViper()
	return v.GetBool(key)
}

// GetInt returns a configuration value as int using dot notation
func GetInt(key string) int {
	v := initViper()
	return v.GetInt(key)
}

// GetFloat64 returns a configuration value as float64 using dot notation
func GetFloat64(key string) float64 {
	v := initViper()
	return v.GetFloat64(key)
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
