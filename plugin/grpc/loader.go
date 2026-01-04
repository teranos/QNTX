package grpc

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-getter"
	"github.com/teranos/QNTX/am"
	"go.uber.org/zap"
)

// LoadPluginsFromConfig loads plugins based on am configuration.
// It discovers plugin binaries from configured paths and loads enabled plugins.
func LoadPluginsFromConfig(ctx context.Context, cfg *am.Config, logger *zap.SugaredLogger) (*PluginManager, error) {
	manager := NewPluginManager(logger)

	// If no plugins enabled, return empty manager
	if len(cfg.Plugin.Enabled) == 0 {
		logger.Infow("No plugins enabled in configuration")
		return manager, nil
	}

	// Build map of enabled plugins for quick lookup
	enabledPlugins := make(map[string]bool)
	for _, name := range cfg.Plugin.Enabled {
		enabledPlugins[name] = true
	}

	// Discover plugins from configured paths
	var pluginConfigs []PluginConfig
	for _, pluginName := range cfg.Plugin.Enabled {
		pluginConfig, err := discoverPlugin(pluginName, cfg.Plugin.Paths, logger)
		if err != nil {
			logger.Warnw("Failed to discover plugin",
				"plugin", pluginName,
				"error", err,
				"paths", cfg.Plugin.Paths,
			)
			continue
		}
		pluginConfigs = append(pluginConfigs, pluginConfig)
	}

	// Load discovered plugins
	if len(pluginConfigs) > 0 {
		if err := manager.LoadPlugins(ctx, pluginConfigs); err != nil {
			return nil, fmt.Errorf("failed to load plugins: %w", err)
		}
	}

	return manager, nil
}

// discoverPlugin finds a plugin binary in the configured search paths.
func discoverPlugin(name string, searchPaths []string, logger *zap.SugaredLogger) (PluginConfig, error) {
	// Expand and validate paths using go-getter's detection
	expandedPaths := make([]string, 0, len(searchPaths))
	for _, path := range searchPaths {
		expanded, err := expandAndValidatePath(path)
		if err != nil {
			logger.Warnw("Invalid search path, skipping",
				"path", path,
				"error", err,
			)
			continue
		}
		expandedPaths = append(expandedPaths, expanded)
	}

	// Search for plugin binary
	for _, searchPath := range expandedPaths {
		// Try common plugin binary names
		candidates := []string{
			filepath.Join(searchPath, fmt.Sprintf("qntx-%s-plugin", name)),
			filepath.Join(searchPath, fmt.Sprintf("qntx-%s", name)),
			filepath.Join(searchPath, name),
		}

		for _, candidate := range candidates {
			if fileInfo, err := os.Stat(candidate); err == nil {
				// Check if executable
				if fileInfo.Mode()&0111 == 0 {
					logger.Debugw("Found plugin binary but not executable",
						"plugin", name,
						"path", candidate,
					)
					continue
				}

				logger.Infow("Discovered plugin binary",
					"plugin", name,
					"path", candidate,
				)

				return PluginConfig{
					Name:      name,
					Enabled:   true,
					Binary:    candidate,
					AutoStart: true,
				}, nil
			}
		}
	}

	return PluginConfig{}, fmt.Errorf("plugin binary not found in search paths: %s", strings.Join(expandedPaths, ", "))
}

// expandAndValidatePath safely expands and validates a path using go-getter.
// Handles ~, relative paths, and validates the result is a valid filesystem path.
func expandAndValidatePath(path string) (string, error) {
	// Use go-getter's detection to safely handle paths
	// This handles ~, relative paths, and more
	detected, err := getter.Detect(path, filepath.Dir(path), getter.Detectors)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	// Parse the detected URL/path
	u, err := url.Parse(detected)
	if err != nil {
		return "", fmt.Errorf("failed to parse path: %w", err)
	}

	// For file:// URLs, extract the path
	if u.Scheme == "file" {
		return u.Path, nil
	}

	// For local paths (no scheme or empty scheme), use as-is
	if u.Scheme == "" {
		// If path starts with ~, expand it manually as fallback
		if strings.HasPrefix(path, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("failed to get home directory: %w", err)
			}
			return filepath.Join(home, path[2:]), nil
		}
		if path == "~" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("failed to get home directory: %w", err)
			}
			return home, nil
		}
		// Make absolute
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("failed to make absolute path: %w", err)
		}
		return abs, nil
	}

	return "", fmt.Errorf("unsupported path scheme: %s (expected file:// or local path)", u.Scheme)
}
