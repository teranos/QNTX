package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/go-getter"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/errors"
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
			failedPlugins = append(failedPlugins, record.Name)
			manager.mu.Lock()
			manager.failedPlugins[record.Name] = err.Error()
			manager.mu.Unlock()
			logger.Errorw("Plugin not loaded: its args do not read", "plugin", record.Name, "error", err)
			continue
		}
		pluginNames = append(pluginNames, record.Name)
		args[record.Name] = launch
	}

	// Sort plugin names for deterministic iteration
	sort.Strings(pluginNames)
	enabled := len(pluginNames) + len(failedPlugins)
	searchPaths := cfg.Plugin.Paths

	var pluginConfigs []PluginConfig
	for _, pluginName := range pluginNames {
		pluginConfig, err := discoverPlugin(pluginName, searchPaths, logger)
		if err != nil {
			failedPlugins = append(failedPlugins, pluginName)
			manager.mu.Lock()
			manager.failedPlugins[pluginName] = err.Error()
			manager.mu.Unlock()
			logger.Warnw("Plugin unavailable", "plugin", pluginName, "error", err,
				"searched", searchPaths, "tried", pluginBinaryNames(pluginName),
				"hints", errors.GetAllHints(err))
			continue
		}
		pluginConfig.Args = args[pluginName]
		logger.Debugf("Will load '%s' plugin from %s", pluginName, pluginConfig.Source)
		pluginConfigs = append(pluginConfigs, pluginConfig)
	}

	if err := manager.LoadPlugins(ctx, pluginConfigs); err != nil {
		return errors.Wrapf(err, "failed to load %d plugins", len(pluginConfigs))
	}

	// Each failure was said as it happened and is held in the manager's failed plugins.
	logger.Infow("Plugin discovery complete",
		"known", len(known),
		"enabled", enabled,
		"launching", len(pluginConfigs),
		"failed", failedPlugins,
	)
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
// as a JSON list. A plugin with no args key has none; an args key that holds
// no JSON list is refused.
func recordArgs(record PluginRecord) ([]string, error) {
	raw, set := record.Config["args"]
	if !set {
		return nil, nil
	}
	var args []string
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, errors.Wrapf(err, "plugin %s: args is %q, and args is a JSON list of strings", record.Name, raw)
	}
	return args, nil
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
		for _, binaryName := range pluginBinaryNames(name) {
			candidate := filepath.Join(searchPath, binaryName)
			fileInfo, err := os.Stat(candidate)
			if err != nil {
				continue
			}

			if fileInfo.IsDir() {
				// A TypeScript plugin directory: package.json marks it, plugin.ts runs it.
				if pluginTs, ok := typeScriptPluginInDir(candidate); ok {
					logger.Debugf("Found '%s' TypeScript plugin: %s", name, pluginTs)
					return PluginConfig{Name: name, Enabled: true, Source: LaunchBinary(pluginTs)}, nil
				}

				// Native plugin shipped as a tree: the binary sits inside
				// the directory with its private libraries beside it, found
				// via an $ORIGIN-relative RPATH. QNTX's own release is
				// packaged this way; plugins may be too.
				if binary, ok := nativePluginInDir(candidate, name); ok {
					logger.Debugf("Found '%s' plugin tree: %s", name, binary)
					return PluginConfig{Name: name, Enabled: true, Source: LaunchBinary(binary)}, nil
				}

				// Not a valid plugin directory, continue searching
				continue
			}

			// A TypeScript plugin file runs under bun, executable or not.
			if strings.HasSuffix(candidate, ".ts") {
				logger.Debugf("Found '%s' TypeScript plugin: %s", name, candidate)
				return PluginConfig{Name: name, Enabled: true, Source: LaunchBinary(candidate)}, nil
			}

			// Issue #137: This doesn't work on Windows where executability is by extension
			binary, err := exec.LookPath(candidate)
			if err != nil {
				logger.Debugw("Found plugin binary but it does not run",
					"plugin", name,
					"path", candidate,
					"error", err,
				)
				continue
			}

			logger.Debugf("Found '%s' plugin binary: %s", name, binary)
			return PluginConfig{Name: name, Enabled: true, Source: LaunchBinary(binary)}, nil
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
		path, err := exec.LookPath(filepath.Join(dir, candidate))
		if err != nil {
			continue
		}
		return path, true
	}

	return "", false
}

// typeScriptPluginInDir is the plugin.ts of a directory whose package.json
// says "qntx-plugin": true.
func typeScriptPluginInDir(dir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return "", false
	}
	var pkg struct {
		QNTXPlugin bool `json:"qntx-plugin"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil || !pkg.QNTXPlugin {
		return "", false
	}
	pluginTs := filepath.Join(dir, "plugin.ts")
	info, err := os.Stat(pluginTs)
	if err != nil || info.IsDir() {
		return "", false
	}
	return pluginTs, true
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
		return "", errors.Wrapf(err, "no working directory to resolve %s against", path)
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

	// go-getter detects a local path, relative or absolute, as a file:// URL.
	if u.Scheme == "file" {
		return u.Path, nil
	}

	err = errors.Newf("unsupported path scheme: %s (expected file:// or local path)", u.Scheme)
	return "", errors.WithHint(err, "use a local filesystem path like ~/.qntx/plugins/ instead of remote URLs")
}
