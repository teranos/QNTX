package config

import (
	"fmt"
	"os"
	"reflect"
	"sort"

	"github.com/spf13/viper"

	errors "github.com/teranos/sacred-error"
)

// bindEveryKey binds each key the Config reads to its environment variable,
// QNTX_ and the key with dots as underscores. Viper hands Unmarshal only the
// keys it knows, so a key nothing else names is named here, or a variable set
// for it never reaches the Config.
func bindEveryKey(v *viper.Viper) error {
	for _, key := range keysOf(reflect.TypeOf(Config{}), "") {
		if err := v.BindEnv(key); err != nil {
			return errors.Wrapf(err, "the environment variable for %s could not be bound", key)
		}
	}
	return nil
}

// BindSensitiveEnvVars explicitly binds sensitive configuration to environment variables.
// BindEnv fails only when called with no keys; a binding that somehow failed
// would leave its env var silently ignored, so it reports like a broken init.
func BindSensitiveEnvVars(v *viper.Viper) {
	bind := func(key, envVar string) {
		if err := v.BindEnv(key, envVar); err != nil {
			reportViperInitErr(key, err)
		}
	}

	// Code command configuration
	bind("code.github.token", "QNTX_CODE_GITHUB_TOKEN")

	// Storage SQLite path
	bind("storage.sqlite.path", "QNTX_STORAGE_SQLITE_PATH")

	// Server bind address (e.g., "0.0.0.0" for all interfaces — requires auth.enabled)
	bind("server.bind_address", "QNTX_BIND_ADDRESS")
}

// IsLoopbackAddress reports whether addr is a loopback address: 127.0.0.1,
// ::1 or localhost. An empty address is no address, and not loopback.
func IsLoopbackAddress(addr string) bool {
	return addr == "127.0.0.1" || addr == "::1" || addr == "localhost"
}

// IsDevMode returns true when QNTX is running in development mode.
// Set by the --dev flag on the server command.
func IsDevMode() bool {
	return os.Getenv("DEV") == "true"
}

// SetDevMode sets the DEV environment variable. Called early in server startup.
// A set that failed means IsDevMode answers false for the rest of the run.
func SetDevMode() {
	if err := os.Setenv("DEV", "true"); err != nil {
		reportViperInitErr("DEV", err)
	}
}

// Port is the port the node listens on, and whether am.toml said one.
func (c *Config) Port() (int, bool) {
	if c.Server.Port == nil {
		return 0, false
	}
	return *c.Server.Port, true
}

// GetServerAllowedOrigins returns the allowed CORS origins
// Merges configured origins with secure defaults, ensuring critical origins
// (localhost, 127.0.0.1, tauri) are always included even if not in config
func (c *Config) GetServerAllowedOrigins() []string {
	// Define secure default origins that should always be allowed
	defaults := []string{
		"http://localhost",
		"http://localhost:*",
		"https://localhost",
		"https://localhost:*",
		"http://127.0.0.1",
		"http://127.0.0.1:*",
		"https://127.0.0.1",
		"https://127.0.0.1:*",
		"tauri://localhost",
		"http://tauri.localhost",
		"https://tauri.localhost",
	}

	// Merge: Start with defaults, add custom origins (deduplicated via map)
	originSet := make(map[string]bool)
	for _, origin := range defaults {
		originSet[origin] = true
	}
	for _, origin := range c.Server.AllowedOrigins {
		originSet[origin] = true
	}

	// Convert map to slice and sort for deterministic output
	merged := make([]string, 0, len(originSet))
	for origin := range originSet {
		merged = append(merged, origin)
	}
	sort.Strings(merged)

	return merged
}

// GetLogPath returns the file log path. If not configured, defaults to tmp/qntx-{port}.log.
func (c *Config) GetLogPath(port int) string {
	if c.Server.LogPath != "" {
		return c.Server.LogPath
	}
	return fmt.Sprintf("tmp/qntx-%d.log", port)
}

// String returns a string representation of the config
func (c *Config) String() string {
	return fmt.Sprintf("Config{Storage: {Backend: %s, Sqlite.Path: %s}, Server: {LogTheme: %s}, Pulse: {Workers: %d}}",
		c.Storage.Backend, c.Storage.Sqlite.Path, c.Server.LogTheme, c.Pulse.Workers)
}
