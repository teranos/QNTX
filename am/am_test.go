package am

import (
	"os"
	"path/filepath"
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

	if cfg.Server.Port != DefaultGraphPort {
		t.Errorf("expected default port %d, got %d", DefaultGraphPort, cfg.Server.Port)
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
			name: "zero workers is valid (disabled)",
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
			name: "zero ticker interval is valid (disabled)",
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
			name: "zero rate limit is valid (unlimited)",
			config: Config{
				Pulse: PulseConfig{HTTPMaxRequestsPerMinute: 0},
			},
			wantErr: false,
		},
		{
			name: "negative rate limit is invalid",
			config: Config{
				Pulse: PulseConfig{HTTPMaxRequestsPerMinute: -1},
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
		{"server.port", DefaultGraphPort},
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

func TestGetGraphPort(t *testing.T) {
	// Reset global state
	Reset()

	// Test default behavior
	port := GetGraphPort()
	if port != DefaultGraphPort {
		t.Errorf("expected default port %d, got %d", DefaultGraphPort, port)
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

func TestGetREPLConfig_Defaults(t *testing.T) {
	// Create isolated viper instance without loading user/system config
	v := viper.New()
	SetDefaults(v)

	cfg, err := LoadWithViper(v)
	if err != nil {
		t.Fatalf("LoadWithViper() failed: %v", err)
	}

	repl := cfg.GetREPLConfig()

	// Verify all defaults are applied
	if repl.Search.DebounceMs != 50 {
		t.Errorf("expected debounce 50, got %d", repl.Search.DebounceMs)
	}
	if repl.Search.ResultLimit != 10 {
		t.Errorf("expected result limit 10, got %d", repl.Search.ResultLimit)
	}
	if repl.Display.MaxLines != 10 {
		t.Errorf("expected max lines 10, got %d", repl.Display.MaxLines)
	}
	if repl.Timeouts.CommandSeconds != 30 {
		t.Errorf("expected command timeout 30, got %d", repl.Timeouts.CommandSeconds)
	}
}
