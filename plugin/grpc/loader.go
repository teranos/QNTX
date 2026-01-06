package grpc

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/go-getter"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
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

	// Build map of enabled plugins for deduplication
	enabledPlugins := make(map[string]bool)
	for _, name := range cfg.Plugin.Enabled {
		enabledPlugins[name] = true
	}

	// Sort plugin names for deterministic iteration
	pluginNames := make([]string, 0, len(enabledPlugins))
	for name := range enabledPlugins {
		pluginNames = append(pluginNames, name)
	}
	sort.Strings(pluginNames)

	// Discover plugins from configured paths (deduplicated)
	var pluginConfigs []PluginConfig
	var failedPlugins []string
	for _, pluginName := range pluginNames {
		logger.Infof("Searching for '%s' plugin binary in %d paths", pluginName, len(cfg.Plugin.Paths))

		pluginConfig, err := discoverPlugin(pluginName, cfg.Plugin.Paths, logger)
		if err != nil {
			logger.Warnf("Plugin '%s' not found - searched paths: %v, tried names: [qntx-%s-plugin, qntx-%s, %s]",
				pluginName, cfg.Plugin.Paths, pluginName, pluginName, pluginName)
			failedPlugins = append(failedPlugins, pluginName)
			continue
		}
		logger.Infof("Will load '%s' plugin from binary: %s", pluginName, pluginConfig.Binary)
		pluginConfigs = append(pluginConfigs, pluginConfig)
	}

	// Load discovered plugins
	if len(pluginConfigs) > 0 {
		if err := manager.LoadPlugins(ctx, pluginConfigs); err != nil {
			return nil, errors.Wrap(err, "failed to load plugins")
		}

		// Configure WebSocket settings from am.Config
		keepaliveCfg := NewKeepaliveConfigFromSettings(
			cfg.Plugin.WebSocket.Keepalive.Enabled,
			cfg.Plugin.WebSocket.Keepalive.PingIntervalSecs,
			cfg.Plugin.WebSocket.Keepalive.PongTimeoutSecs,
			cfg.Plugin.WebSocket.Keepalive.ReconnectAttempts,
		)

		// Build WebSocket origin config from server allowed origins
		wsConfig := WebSocketConfig{
			AllowedOrigins:   cfg.GetServerAllowedOrigins(),
			AllowAllOrigins:  false,
			AllowCredentials: false,
		}

		manager.ConfigureWebSocket(keepaliveCfg, wsConfig)
	}

	// Log summary of discovery results
	if len(failedPlugins) > 0 {
		logger.Warnw("Some enabled plugins failed to load",
			"enabled", len(cfg.Plugin.Enabled),
			"loaded", len(pluginConfigs),
			"failed", failedPlugins,
		)
	} else if len(pluginConfigs) > 0 {
		logger.Infow("Plugin discovery complete",
			"enabled", len(cfg.Plugin.Enabled),
			"loaded", len(pluginConfigs),
		)
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
				// Check if executable (Unix-specific: checks permission bits)
				// Issue #137: This doesn't work on Windows where executability is by extension
				if fileInfo.Mode()&0111 == 0 {
					logger.Debugw("Found plugin binary but not executable",
						"plugin", name,
						"path", candidate,
					)
					continue
				}

				logger.Infof("Found '%s' plugin binary: %s", name, candidate)

				return PluginConfig{
					Name:      name,
					Enabled:   true,
					Binary:    candidate,
					AutoStart: true,
				}, nil
			}
		}
	}

	return PluginConfig{}, errors.Newf("plugin binary not found in search paths: %s", strings.Join(expandedPaths, ", "))
}

// expandAndValidatePath safely expands and validates a path using go-getter.
// Handles ~, relative paths, and validates the result is a valid filesystem path.
func expandAndValidatePath(path string) (string, error) {
	// Handle tilde expansion first (go-getter doesn't do this)
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.Wrap(err, "failed to get home directory")
		}
		path = filepath.Join(home, path[2:])
	} else if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.Wrap(err, "failed to get home directory")
		}
		return home, nil
	}

	// Get current working directory for resolving relative paths
	pwd, err := os.Getwd()
	if err != nil {
		pwd = "."
	}

	// Use go-getter's detection to safely handle paths
	detected, err := getter.Detect(path, pwd, getter.Detectors)
	if err != nil {
		return "", errors.Wrap(err, "invalid path")
	}

	// Parse the detected URL/path
	u, err := url.Parse(detected)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse path")
	}

	// For file:// URLs, extract the path
	if u.Scheme == "file" {
		return u.Path, nil
	}

	// For local paths (no scheme or empty scheme), make absolute
	if u.Scheme == "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", errors.Wrap(err, "failed to make absolute path")
		}
		return abs, nil
	}

	return "", errors.Newf("unsupported path scheme: %s (expected file:// or local path)", u.Scheme)
}
