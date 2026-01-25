package am

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestLoad_Defaults(t *testing.T) {
	// Create isolated viper instance without loading user/system config
	v := viper.New()
	SetDefaults(v)

	// Load config from isolated viper
	cfg, err := LoadWithViper(v)
	if err != nil {
		t.Fatalf("LoadWithViper() failed: %v", err)
	}

	// Check default values are applied
	if cfg.Database.Path != "qntx.db" {
		t.Errorf("expected default database path 'qntx.db', got %q", cfg.Database.Path)
	}

	if cfg.Server.Port != DefaultServerPort {
		t.Errorf("expected default port %d, got %d", DefaultServerPort, cfg.Server.Port)
	}

	if cfg.Pulse.Workers != 1 {
		t.Errorf("expected default workers 1, got %d", cfg.Pulse.Workers)
	}

	if cfg.LocalInference.BaseURL != "http://localhost:11434" {
		t.Errorf("expected default local inference URL, got %q", cfg.LocalInference.BaseURL)
	}
}

func TestValidate_ZeroValues(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "zero workers is valid (no background workers)",
			config: Config{
				Pulse: PulseConfig{Workers: 0},
			},
			wantErr: false,
		},
		{
			name: "negative workers is invalid",
			config: Config{
				Pulse: PulseConfig{Workers: -1},
			},
			wantErr: true,
		},
		{
			name: "zero ticker interval is valid (no periodic ticking)",
			config: Config{
				Pulse: PulseConfig{TickerIntervalSeconds: 0},
			},
			wantErr: false,
		},
		{
			name: "negative ticker interval is invalid",
			config: Config{
				Pulse: PulseConfig{TickerIntervalSeconds: -1},
			},
			wantErr: true,
		},
		{
			name: "empty database path is valid",
			config: Config{
				Database: DatabaseConfig{Path: ""},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSetDefaults(t *testing.T) {
	v := viper.New()
	SetDefaults(v)

	// Verify critical defaults are set
	tests := []struct {
		key      string
		expected interface{}
	}{
		{"database.path", "qntx.db"},
		{"server.port", DefaultServerPort},
		{"server.log_theme", "everforest"},
		{"pulse.workers", 1},
		{"pulse.ticker_interval_seconds", 1},
		{"local_inference.enabled", true},
		{"local_inference.base_url", "http://localhost:11434"},
		{"code.gopls.enabled", true},
		{"ax.default_actor", "ax@user"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := v.Get(tt.key)
			if got != tt.expected {
				t.Errorf("default %s = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}

func TestFindProjectConfig(t *testing.T) {
	// Create temporary directory structure
	tmpDir := t.TempDir()

	// Test 1: am.toml preferred over config.toml
	t.Run("prefers am.toml", func(t *testing.T) {
		subDir := filepath.Join(tmpDir, "test1", "subdir")
		os.MkdirAll(subDir, DefaultDirPermissions)

		// Create both config files
		os.WriteFile(filepath.Join(tmpDir, "test1", "am.toml"), []byte(""), DefaultFilePermissions)
		os.WriteFile(filepath.Join(tmpDir, "test1", "config.toml"), []byte(""), DefaultFilePermissions)

		// Change to subdirectory
		oldWd, _ := os.Getwd()
		defer os.Chdir(oldWd)
		os.Chdir(subDir)

		result := findProjectConfig()
		if result == "" {
			t.Error("expected to find config file")
		}
		if !filepath.IsAbs(result) {
			t.Error("expected absolute path")
		}
		if filepath.Base(result) != "am.toml" {
			t.Errorf("expected am.toml, got %s", filepath.Base(result))
		}
	})

	// Test 2: Falls back to config.toml if am.toml not present
	t.Run("fallback to config.toml", func(t *testing.T) {
		subDir := filepath.Join(tmpDir, "test2", "subdir")
		os.MkdirAll(subDir, DefaultDirPermissions)

		// Create only config.toml
		os.WriteFile(filepath.Join(tmpDir, "test2", "config.toml"), []byte(""), DefaultFilePermissions)

		oldWd, _ := os.Getwd()
		defer os.Chdir(oldWd)
		os.Chdir(subDir)

		result := findProjectConfig()
		if result == "" {
			t.Error("expected to find config file")
		}
		if filepath.Base(result) != "config.toml" {
			t.Errorf("expected config.toml, got %s", filepath.Base(result))
		}
	})

	// Test 3: Returns empty string when no config found
	t.Run("no config found", func(t *testing.T) {
		subDir := filepath.Join(tmpDir, "test3", "subdir")
		os.MkdirAll(subDir, DefaultDirPermissions)

		oldWd, _ := os.Getwd()
		defer os.Chdir(oldWd)
		os.Chdir(subDir)

		result := findProjectConfig()
		if result != "" {
			t.Errorf("expected empty string, got %s", result)
		}
	})
}

func TestGetServerPort(t *testing.T) {
	// Create isolated viper instance without loading user/system/project config
	v := viper.New()
	SetDefaults(v)

	cfg, err := LoadWithViper(v)
	if err != nil {
		t.Fatalf("LoadWithViper() failed: %v", err)
	}

	// Test that default port is set correctly
	if cfg.Server.Port != DefaultServerPort {
		t.Errorf("expected default port %d, got %d", DefaultServerPort, cfg.Server.Port)
	}
}

func TestGetDatabasePath(t *testing.T) {
	// Create isolated viper instance without loading user/system config
	v := viper.New()
	SetDefaults(v)

	cfg, err := LoadWithViper(v)
	if err != nil {
		t.Fatalf("LoadWithViper() failed: %v", err)
	}

	path := cfg.GetDatabasePath()
	if path != "qntx.db" {
		t.Errorf("expected default path 'qntx.db', got %q", path)
	}
}

func TestLoadPluginConfigs(t *testing.T) {
	// Setup: Create test home directory
	testHome := t.TempDir()
	pluginsDir := filepath.Join(testHome, ".qntx", "plugins")
	os.MkdirAll(pluginsDir, DefaultDirPermissions)

	// Create test plugin config
	testConfig := `name = "python"
enabled = true
binary = "qntx-python-plugin"
auto_start = true
args = ["--port", "9000"]

[config]
python_paths = "/custom/python/path"
default_modules = "numpy,pandas,scipy,matplotlib"
timeout_secs = "60"
max_workers = 4
enable_debug = true
`
	configPath := filepath.Join(pluginsDir, "python.toml")
	if err := os.WriteFile(configPath, []byte(testConfig), DefaultFilePermissions); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Override HOME for this test
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", testHome)
	defer os.Setenv("HOME", originalHome)

	// Reset viper to force reload
	Reset()

	// Test: Load plugin configs
	if err := LoadPluginConfigs(nil); err != nil {
		t.Fatalf("LoadPluginConfigs() failed: %v", err)
	}

	// Verify: Check that values are loaded into viper
	tests := []struct {
		key      string
		expected string
	}{
		{"python.python_paths", "/custom/python/path"},
		{"python.default_modules", "numpy,pandas,scipy,matplotlib"},
		{"python.timeout_secs", "60"},
		{"python.max_workers", "4"},
		{"python.enable_debug", "true"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := GetString(tt.key)
			if got != tt.expected {
				t.Errorf("GetString(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestLoadPluginConfigs_NoPluginsDir(t *testing.T) {
	// Setup: Create test home without plugins directory
	testHome := t.TempDir()

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", testHome)
	defer os.Setenv("HOME", originalHome)

	Reset()

	// Should not error when plugins directory doesn't exist
	if err := LoadPluginConfigs(nil); err != nil {
		t.Errorf("LoadPluginConfigs() should not error when plugins dir doesn't exist, got: %v", err)
	}
}

func TestLoadPluginConfigs_InvalidTOML(t *testing.T) {
	// Setup: Create test home directory
	testHome := t.TempDir()
	pluginsDir := filepath.Join(testHome, ".qntx", "plugins")
	os.MkdirAll(pluginsDir, DefaultDirPermissions)

	// Create invalid TOML
	invalidConfig := `name = "python"
[config]
broken syntax here
`
	configPath := filepath.Join(pluginsDir, "python.toml")
	os.WriteFile(configPath, []byte(invalidConfig), DefaultFilePermissions)

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", testHome)
	defer os.Setenv("HOME", originalHome)

	Reset()

	// Should return error with helpful message
	err := LoadPluginConfigs(nil)
	if err == nil {
		t.Error("LoadPluginConfigs() should error on invalid TOML")
	}
}

func TestUpdatePluginConfig(t *testing.T) {
	// Setup: Create test home directory
	testHome := t.TempDir()
	pluginsDir := filepath.Join(testHome, ".qntx", "plugins")
	os.MkdirAll(pluginsDir, DefaultDirPermissions)

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", testHome)
	defer os.Setenv("HOME", originalHome)

	Reset()

	// Test: Update config for a new plugin
	newConfig := map[string]string{
		"python_paths":     "/new/path",
		"default_modules":  "numpy,pandas",
		"timeout_secs":     "30",
	}

	if err := UpdatePluginConfig("python", newConfig); err != nil {
		t.Fatalf("UpdatePluginConfig() failed: %v", err)
	}

	// Verify: Config file was created
	configPath := filepath.Join(pluginsDir, "python.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Config file was not created")
	}

	// Verify: Values are in viper
	if got := GetString("python.python_paths"); got != "/new/path" {
		t.Errorf("python.python_paths = %q, want %q", got, "/new/path")
	}

	// Verify: File contains correct TOML
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "python_paths = \"/new/path\"") {
		t.Error("Config file doesn't contain expected values")
	}
}

func TestWritePluginConfigToTemp(t *testing.T) {
	// Setup: Create test home directory
	testHome := t.TempDir()
	pluginsDir := filepath.Join(testHome, ".qntx", "plugins")
	os.MkdirAll(pluginsDir, DefaultDirPermissions)

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", testHome)
	defer os.Setenv("HOME", originalHome)

	Reset()

	// Test: Write config to temp file
	testConfig := map[string]string{
		"python_paths": "/test/path",
		"timeout_secs": "45",
	}

	tempPath, err := WritePluginConfigToTemp("python", testConfig)
	if err != nil {
		t.Fatalf("WritePluginConfigToTemp() failed: %v", err)
	}
	defer os.Remove(tempPath)

	// Verify: Temp file exists
	if _, err := os.Stat(tempPath); os.IsNotExist(err) {
		t.Error("Temp file was not created")
	}

	// Verify: Temp file contains valid TOML
	data, err := os.ReadFile(tempPath)
	if err != nil {
		t.Fatalf("Failed to read temp file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "python_paths = \"/test/path\"") {
		t.Error("Temp file doesn't contain expected values")
	}
}
