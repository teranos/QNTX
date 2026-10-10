package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/go-getter"
	"github.com/teranos/QNTX/internal/config"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// LoadPluginsFromRecords loads every enabled plugin the node knows (ADR-043),
// from the build on disk. One whose build has not landed is marked failed with
// why, and the runner's next build of it starts it.
func LoadPluginsFromRecords(ctx context.Context, manager *PluginManager, records PluginRecords, cfg *config.Config, logger *zap.SugaredLogger) error {
	known, err := records.Plugins()
	if err != nil {
		return errors.Wrapf(err, "failed to read the plugin records to load from %v", cfg.Plugin.Paths)
	}
	args := make(map[string][]string)
	var pluginNames []string
	var failedPlugins []string
	for _, record := range known {
		if !record.Enabled {
			continue
		}
		launch, err := recordArgs(record)
		if err != nil {
			logger.Errorw("Plugin not loaded: its args do not read", "plugin", record.Name, "error", err)
			failedPlugins = append(failedPlugins, record.Name)
			manager.mu.Lock()
			manager.failedPlugins[record.Name] = err.Error()
			manager.mu.Unlock()
			continue
		}
		pluginNames = append(pluginNames, record.Name)
		args[record.Name] = launch
	}
	if len(pluginNames) == 0 && len(failedPlugins) == 0 {
		logger.Infow("No plugin is enabled", "known", len(known))
		return nil
	}

	// Sort plugin names for deterministic iteration
	sort.Strings(pluginNames)
	enabled := len(pluginNames) + len(failedPlugins)
	searchPaths := cfg.Plugin.Paths

	var pluginConfigs []PluginConfig
	for _, pluginName := range pluginNames {
		pluginConfig, err := discoverPlugin(pluginName, searchPaths, logger)
		if err != nil {
			logger.Warnf("Plugin '%s' unavailable: %v - searched paths: %v, tried names: [qntx-%s-plugin, qntx-%s, %s]%s",
				pluginName, err, searchPaths, pluginName, pluginName, pluginName,
				formatHints(err))
			failedPlugins = append(failedPlugins, pluginName)
			manager.mu.Lock()
			manager.failedPlugins[pluginName] = err.Error()
			manager.mu.Unlock()
			continue
		}
		if len(args[pluginName]) > 0 {
			pluginConfig.Args = args[pluginName]
		}
		logger.Debugf("Will load '%s' plugin from binary: %s", pluginName, pluginConfig.Binary)
		pluginConfigs = append(pluginConfigs, pluginConfig)
	}

	if len(pluginConfigs) > 0 {
		if err := manager.LoadPlugins(ctx, pluginConfigs); err != nil {
			return errors.Wrapf(err, "failed to load %d plugins", len(pluginConfigs))
		}
	}

	if len(failedPlugins) > 0 {
		logger.Warnw("Some enabled plugins failed to load",
			"enabled", enabled,
			"loaded", len(pluginConfigs),
			"failed", failedPlugins,
		)
	} else if len(pluginConfigs) > 0 {
		logger.Debugw("Plugin discovery complete",
			"enabled", enabled,
			"loaded", len(pluginConfigs),
		)
	}
	return nil
}

// ConfigureWebSocketFromConfig hands every plugin, loaded now or enabled later,
// the node's WebSocket settings: keepalive, and server.allowed_origins.
func ConfigureWebSocketFromConfig(manager *PluginManager, cfg *config.Config) {
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

// recordArgs is the launch args a plugin's config holds under args, written
// as a JSON list. A plugin with no args key has none.
func recordArgs(record PluginRecord) ([]string, error) {
	raw, set := record.Config["args"]
	if !set || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, errors.Wrapf(err, "plugin %s: args is %q, and args is a JSON list of strings", record.Name, raw)
	}
	return args, nil
}

// formatHints renders an error's hints for a log line, or "" when it has none.
// Hints hold the fix; %v alone shows only the failure.
func formatHints(err error) string {
	hints := errors.GetAllHints(err)
	if len(hints) == 0 {
		return ""
	}
	return " - " + strings.Join(hints, "; ")
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
		candidates := make([]string, 0, 3)
		for _, binaryName := range pluginBinaryNames(name) {
			candidates = append(candidates, filepath.Join(searchPath, binaryName))
		}

		for _, candidate := range candidates {
			if fileInfo, err := os.Stat(candidate); err == nil {
				// Special handling for TypeScript plugins
				if fileInfo.IsDir() {
					// Check if this is a TypeScript plugin directory (has package.json with qntx-plugin marker)
					pkgPath := filepath.Join(candidate, "package.json")
					if _, err := os.Stat(pkgPath); err == nil {
						// Has package.json, check if it's a QNTX plugin
						var pkg struct {
							QNTXPlugin bool `json:"qntx-plugin"`
						}
						if data, err := os.ReadFile(pkgPath); err == nil {
							if err := json.Unmarshal(data, &pkg); err == nil && pkg.QNTXPlugin {
								// TypeScript plugin directory - look for plugin.ts
								pluginTsPath := filepath.Join(candidate, "plugin.ts")
								if _, err := os.Stat(pluginTsPath); err == nil {
									logger.Debugf("Found '%s' TypeScript plugin: %s", name, pluginTsPath)
									return PluginConfig{
										Name:      name,
										Enabled:   true,
										Binary:    pluginTsPath,
										AutoStart: true,
									}, nil
								}
							}
						}
					}

					// Native plugin shipped as a tree: the binary sits inside
					// the directory with its private libraries beside it, found
					// via an $ORIGIN-relative RPATH. QNTX's own release is
					// packaged this way; plugins may be too.
					if binary, ok := nativePluginInDir(candidate, name); ok {
						logger.Debugf("Found '%s' plugin tree: %s", name, binary)
						return PluginConfig{
							Name:      name,
							Enabled:   true,
							Binary:    binary,
							AutoStart: true,
						}, nil
					}

					// Not a valid plugin directory, continue searching
					continue
				}

				// Regular file - check if executable
				// Issue #137: This doesn't work on Windows where executability is by extension
				if fileInfo.Mode()&0111 == 0 {
					// Not executable - check if it's a .ts file (TypeScript plugin)
					if strings.HasSuffix(candidate, ".ts") {
						logger.Debugf("Found '%s' TypeScript plugin: %s", name, candidate)
						return PluginConfig{
							Name:      name,
							Enabled:   true,
							Binary:    candidate,
							AutoStart: true,
						}, nil
					}

					logger.Debugw("Found plugin binary but not executable",
						"plugin", name,
						"path", candidate,
					)
					continue
				}

				logger.Debugf("Found '%s' plugin binary: %s", name, candidate)

				return PluginConfig{
					Name:      name,
					Enabled:   true,
					Binary:    candidate,
					AutoStart: true,
				}, nil
			}
		}
	}

	err := errors.Newf("plugin binary not found in search paths: %s", strings.Join(expandedPaths, ", "))
	return PluginConfig{}, errors.WithHintf(err, "the runner installs a build of '%s' when its workflow's package step lands one; or install the binary to one of those paths, or add its path to [plugin] paths", name)
}

// nativePluginInDir looks for an executable plugin binary inside dir, trying
// the same names discovery tries at the top level. Returns the path to it.
//
// A tree is how a native plugin ships anything it cannot statically link: the
// binary plus a lib/ directory, reached by an RPATH relative to the binary. The
// alternative is a single file that must find its libraries on the host, which
// only holds when the host and the build machine agree — the assumption that
// makes a binary built on one distro fail to exec on another.
func nativePluginInDir(dir, name string) (string, bool) {
	for _, candidate := range pluginBinaryNames(name) {
		path := filepath.Join(dir, candidate)

		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Mode()&0111 == 0 {
			continue
		}

		return path, true
	}

	return "", false
}

// pluginBinaryNames lists the file names a plugin binary may have, most
// specific first. Discovery and tree lookup must agree on these, so they read
// them from here rather than each spelling them out.
func pluginBinaryNames(name string) []string {
	return []string{
		PluginBinaryName(name),
		fmt.Sprintf("qntx-%s", name),
		name,
	}
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

	err = errors.Newf("unsupported path scheme: %s (expected file:// or local path)", u.Scheme)
	return "", errors.WithHint(err, "use a local filesystem path like ~/.qntx/plugins/ instead of remote URLs")
}
