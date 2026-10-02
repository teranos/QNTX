package config

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
	if cfg.Storage.Sqlite.Path != "qntx.db" {
		t.Errorf("expected default storage.sqlite.path 'qntx.db', got %q", cfg.Storage.Sqlite.Path)
	}

	if cfg.Server.Port == nil || *cfg.Server.Port != DefaultServerPort {
		t.Errorf("expected default port %d, got %v", DefaultServerPort, cfg.Server.Port)
	}

	if cfg.Pulse.Workers != 1 {
		t.Errorf("expected default workers 1, got %d", cfg.Pulse.Workers)
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
				Pulse:   PulseConfig{Workers: 0},
				Storage: StorageConfig{Backend: "sqlite", Sqlite: SqliteConfig{BoundedStorage: BoundedStorageConfig{ActorContextLimit: 32, ActorContextsLimit: 64, EntityActorsLimit: 64}}},
			},
			wantErr: false,
		},
		{
			name: "negative workers is invalid",
			config: Config{
				Pulse:   PulseConfig{Workers: -1},
				Storage: StorageConfig{Backend: "sqlite", Sqlite: SqliteConfig{BoundedStorage: BoundedStorageConfig{ActorContextLimit: 32, ActorContextsLimit: 64, EntityActorsLimit: 64}}},
			},
			wantErr: true,
		},
		{
			name: "zero ticker interval is valid (no periodic ticking)",
			config: Config{
				Pulse:   PulseConfig{TickerIntervalSeconds: 0},
				Storage: StorageConfig{Backend: "sqlite", Sqlite: SqliteConfig{BoundedStorage: BoundedStorageConfig{ActorContextLimit: 32, ActorContextsLimit: 64, EntityActorsLimit: 64}}},
			},
			wantErr: false,
		},
		{
			name: "negative ticker interval is invalid",
			config: Config{
				Pulse:   PulseConfig{TickerIntervalSeconds: -1},
				Storage: StorageConfig{Backend: "sqlite", Sqlite: SqliteConfig{BoundedStorage: BoundedStorageConfig{ActorContextLimit: 32, ActorContextsLimit: 64, EntityActorsLimit: 64}}},
			},
			wantErr: true,
		},
		{
			name: "empty database path is valid",
			config: Config{
				Storage: StorageConfig{Backend: "sqlite", Sqlite: SqliteConfig{Path: "", BoundedStorage: BoundedStorageConfig{ActorContextLimit: 32, ActorContextsLimit: 64, EntityActorsLimit: 64}}},
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
		expected any
	}{
		{"storage.backend", "sqlite"},
		{"storage.sqlite.path", "qntx.db"},
		{"server.port", DefaultServerPort},
		{"server.log_theme", "everforest"},
		{"pulse.workers", 1},
		{"pulse.ticker_interval_seconds", 1},
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
	if cfg.Server.Port == nil || *cfg.Server.Port != DefaultServerPort {
		t.Errorf("expected default port %d, got %v", DefaultServerPort, cfg.Server.Port)
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

func TestGetServerAllowedOrigins_IncludesWildcardPorts(t *testing.T) {
	v := viper.New()
	SetDefaults(v)

	cfg, err := LoadWithViper(v)
	if err != nil {
		t.Fatalf("LoadWithViper() failed: %v", err)
	}

	origins := cfg.GetServerAllowedOrigins()

	// Wildcard port patterns must be present so that origins like
	// http://localhost:8822 (dev frontend) pass origin checks.
	required := []string{
		"http://localhost:*",
		"http://127.0.0.1:*",
		"https://localhost:*",
		"https://127.0.0.1:*",
	}
	for _, want := range required {
		found := false
		for _, got := range origins {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("GetServerAllowedOrigins() missing %q, got %v", want, origins)
		}
	}
}

// The App is one origin per platform and the node must answer all of them.
// Tauri serves it from tauri://localhost on iOS and desktop, and from
// tauri.localhost on Android and Windows. With the Android pair missing the
// phone reached the node, got a 200, and was refused it by CORS: the App
// showed only that api.q.sbvh.nl did not respond.
func TestGetServerAllowedOrigins_IncludesEveryAppOrigin(t *testing.T) {
	v := viper.New()
	SetDefaults(v)

	cfg, err := LoadWithViper(v)
	if err != nil {
		t.Fatalf("LoadWithViper() failed: %v", err)
	}

	origins := cfg.GetServerAllowedOrigins()

	required := []string{
		"tauri://localhost",
		"http://tauri.localhost",
		"https://tauri.localhost",
	}
	for _, want := range required {
		found := false
		for _, got := range origins {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("GetServerAllowedOrigins() missing %q, got %v", want, origins)
		}
	}
}

// TestValidate_ParquetLocation verifies ADR-024: when backend = "parquet",
// storage.parquet.location must be non-empty and use a supported URL scheme.
func TestValidate_ParquetLocation(t *testing.T) {
	tests := []struct {
		name     string
		location string
		wantErr  bool
	}{
		{"s3 url is valid", "s3://bucket/prefix", false},
		{"file url is valid", "file:///var/lib/qntx/parquet", false},
		{"empty location rejected", "", true},
		{"unknown scheme rejected", "gs://bucket/prefix", true},
		{"bare path rejected", "/var/lib/qntx/parquet", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Storage: StorageConfig{
					Backend: "parquet",
					Parquet: ParquetConfig{Location: tt.location},
					Sqlite: SqliteConfig{
						BoundedStorage: BoundedStorageConfig{ActorContextLimit: 32, ActorContextsLimit: 64, EntityActorsLimit: 64},
					},
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_StorageBackend verifies ADR-023/ADR-024: only "sqlite" and "parquet"
// are accepted backend values; unknown values are rejected at load time.
func TestValidate_StorageBackend(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		wantErr bool
	}{
		{"sqlite is valid", "sqlite", false},
		{"parquet is valid", "parquet", false},
		{"unknown backend rejected", "postgres", true},
		{"typo rejected", "sqlight", true},
		{"empty backend rejected", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Storage: StorageConfig{
					Backend: tt.backend,
					Sqlite: SqliteConfig{
						BoundedStorage: BoundedStorageConfig{
							ActorContextLimit:  32,
							ActorContextsLimit: 64,
							EntityActorsLimit:  64,
						},
					},
					// Provide a valid Parquet location so parquet-backend cases
					// don't fail on the location requirement — this test focuses
					// on backend-value validation only.
					Parquet: ParquetConfig{Location: "s3://bucket/prefix"},
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestLoad_Defaults_StorageBackend verifies ADR-023: backend defaults to
// "sqlite" and SQLite-specific config lives under [storage.sqlite].
func TestLoad_Defaults_StorageBackend(t *testing.T) {
	v := viper.New()
	SetDefaults(v)

	cfg, err := LoadWithViper(v)
	if err != nil {
		t.Fatalf("LoadWithViper() failed: %v", err)
	}

	if cfg.Storage.Backend != "sqlite" {
		t.Errorf("expected default storage backend %q, got %q", "sqlite", cfg.Storage.Backend)
	}
	if cfg.Storage.Sqlite.Path != "qntx.db" {
		t.Errorf("expected default storage.sqlite.path %q, got %q", "qntx.db", cfg.Storage.Sqlite.Path)
	}
}
