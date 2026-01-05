package grpc

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteConfigWithViper(t *testing.T) {
	t.Run("GetString", func(t *testing.T) {
		config := map[string]string{
			"name":        "test-plugin",
			"description": "A test plugin",
			"version":     "1.0.0",
		}

		rc := newRemoteConfig("test", config)

		assert.Equal(t, "test-plugin", rc.GetString("name"))
		assert.Equal(t, "A test plugin", rc.GetString("description"))
		assert.Equal(t, "1.0.0", rc.GetString("version"))
		assert.Equal(t, "", rc.GetString("nonexistent"))
	})

	t.Run("GetInt", func(t *testing.T) {
		config := map[string]string{
			"port":      "8080",
			"timeout":   "30",
			"float_val": "42.5",
			"invalid":   "not-a-number",
			"negative":  "-100",
		}

		rc := newRemoteConfig("test", config)

		assert.Equal(t, 8080, rc.GetInt("port"))
		assert.Equal(t, 30, rc.GetInt("timeout"))
		assert.Equal(t, 42, rc.GetInt("float_val")) // Truncates float
		assert.Equal(t, 0, rc.GetInt("invalid"))
		assert.Equal(t, -100, rc.GetInt("negative"))
		assert.Equal(t, 0, rc.GetInt("nonexistent"))
	})

	t.Run("GetBool permissive parsing", func(t *testing.T) {
		testCases := []struct {
			value    string
			expected bool
			desc     string
		}{
			// True values
			{"true", true, "lowercase true"},
			{"True", true, "mixed case True"},
			{"TRUE", true, "uppercase TRUE"},
			{"1", true, "numeric 1"},
			{"t", true, "single t"},
			{"T", true, "single T"},
			{"yes", true, "yes"},
			{"Yes", true, "Yes"},
			{"YES", true, "YES"},
			{"y", true, "single y"},
			{"Y", true, "single Y"},
			{"on", true, "on"},
			{"On", true, "On"},
			{"ON", true, "ON"},

			// False values
			{"false", false, "lowercase false"},
			{"False", false, "mixed case False"},
			{"FALSE", false, "uppercase FALSE"},
			{"0", false, "numeric 0"},
			{"f", false, "single f"},
			{"F", false, "single F"},
			{"no", false, "no"},
			{"No", false, "No"},
			{"NO", false, "NO"},
			{"n", false, "single n"},
			{"N", false, "single N"},
			{"off", false, "off"},
			{"Off", false, "Off"},
			{"OFF", false, "OFF"},

			// Other values default to false
			{"", false, "empty string"},
			{"maybe", false, "invalid value"},
			{"2", false, "numeric 2"},
		}

		for _, tc := range testCases {
			t.Run(tc.desc, func(t *testing.T) {
				config := map[string]string{"test": tc.value}
				rc := newRemoteConfig("test", config)
				assert.Equal(t, tc.expected, rc.GetBool("test"), "Value: %q", tc.value)
			})
		}
	})

	t.Run("GetStringSlice", func(t *testing.T) {
		config := map[string]string{
			"csv":        "one,two,three",
			"csv_spaces": "one, two, three",
			"json_array": `["alpha", "beta", "gamma"]`,
			"single":     "single-value",
			"empty":      "",
		}

		rc := newRemoteConfig("test", config)

		// CSV parsing
		assert.Equal(t, []string{"one", "two", "three"}, rc.GetStringSlice("csv"))
		assert.Equal(t, []string{"one", "two", "three"}, rc.GetStringSlice("csv_spaces"))

		// JSON array parsing
		assert.Equal(t, []string{"alpha", "beta", "gamma"}, rc.GetStringSlice("json_array"))

		// Single value becomes array
		assert.Equal(t, []string{"single-value"}, rc.GetStringSlice("single"))

		// Empty and nonexistent
		assert.Nil(t, rc.GetStringSlice("nonexistent"))
		assert.Nil(t, rc.GetStringSlice("empty")) // Empty string returns nil
	})

	t.Run("Get and Set", func(t *testing.T) {
		config := map[string]string{
			"initial": "value",
		}

		rc := newRemoteConfig("test", config)

		// Test Get
		assert.Equal(t, "value", rc.Get("initial"))
		assert.Nil(t, rc.Get("nonexistent"))

		// Test Set with string
		rc.Set("new", "new-value")
		assert.Equal(t, "new-value", rc.GetString("new"))

		// Test Set with int
		rc.Set("number", 42)
		assert.Equal(t, 42, rc.GetInt("number"))

		// Test Set with bool
		rc.Set("flag", true)
		assert.True(t, rc.GetBool("flag"))

		// Test Set with slice
		rc.Set("list", []string{"a", "b", "c"})
		assert.Equal(t, []string{"a", "b", "c"}, rc.GetStringSlice("list"))

		// Test overwriting existing value
		rc.Set("initial", "modified")
		assert.Equal(t, "modified", rc.GetString("initial"))
	})

	t.Run("GetKeys", func(t *testing.T) {
		config := map[string]string{
			"zebra":  "last",
			"alpha":  "first",
			"middle": "center",
			"beta":   "second",
		}

		rc := newRemoteConfig("test", config)

		keys := rc.GetKeys()

		// Should return all keys
		assert.Len(t, keys, 4)

		// Should be sorted alphabetically
		expected := []string{"alpha", "beta", "middle", "zebra"}
		assert.Equal(t, expected, keys)

		// Verify sorting
		assert.True(t, sort.StringsAreSorted(keys))

		// Add a new key and verify it appears
		rc.Set("new-key", "value")
		keys = rc.GetKeys()
		assert.Contains(t, keys, "new-key")
		assert.Len(t, keys, 5)
	})

	t.Run("empty config", func(t *testing.T) {
		rc := newRemoteConfig("test", nil)

		assert.Equal(t, "", rc.GetString("any"))
		assert.Equal(t, 0, rc.GetInt("any"))
		assert.False(t, rc.GetBool("any"))
		assert.Nil(t, rc.GetStringSlice("any"))
		assert.Nil(t, rc.Get("any"))
		assert.Empty(t, rc.GetKeys())

		// Should be able to set values on empty config
		rc.Set("test", "value")
		assert.Equal(t, "value", rc.GetString("test"))
	})

	t.Run("complex viper features", func(t *testing.T) {
		config := map[string]string{
			"database.host":    "localhost",
			"database.port":    "5432",
			"database.name":    "testdb",
			"features.enabled": "true",
			"features.list":    "auth,api,web",
		}

		rc := newRemoteConfig("test", config)

		// Nested keys work with viper
		assert.Equal(t, "localhost", rc.GetString("database.host"))
		assert.Equal(t, 5432, rc.GetInt("database.port"))
		assert.Equal(t, "testdb", rc.GetString("database.name"))
		assert.True(t, rc.GetBool("features.enabled"))

		// CSV in nested key
		expected := []string{"auth", "api", "web"}
		assert.Equal(t, expected, rc.GetStringSlice("features.list"))

		// All keys includes nested
		keys := rc.GetKeys()
		assert.Contains(t, keys, "database.host")
		assert.Contains(t, keys, "database.port")
		assert.Contains(t, keys, "features.enabled")
	})
}

func TestPluginConfigToml(t *testing.T) {
	t.Skip("TOML loading is tested in discovery_test.go")
}

func TestRemoteServiceRegistryConfig(t *testing.T) {
	t.Run("Config returns proper interface", func(t *testing.T) {
		configMap := map[string]string{
			"test.key": "value",
			"port":     "3000",
		}

		registry := NewRemoteServiceRegistry(
			"localhost:50051",
			"localhost:50052",
			"test-token",
			configMap,
			nil, // logger
		)

		config := registry.Config("test-domain")
		require.NotNil(t, config)

		// Verify it implements the interface
		assert.Equal(t, "value", config.GetString("test.key"))
		assert.Equal(t, 3000, config.GetInt("port"))

		// Test GetKeys is available
		keys := config.GetKeys()
		assert.Contains(t, keys, "test.key")
		assert.Contains(t, keys, "port")
	})
}
