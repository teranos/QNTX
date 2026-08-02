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
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// LoadPluginsFromConfig loads plugins into an existing PluginManager based on am configuration.
// It discovers plugin binaries from configured paths and loads enabled plugins.
func LoadPluginsFromConfig(ctx context.Context, manager *PluginManager, cfg *config.Config, logger *zap.SugaredLogger) error {
	// If no plugins enabled, nothing to do
	if len(cfg.Plugin.Enabled) == 0 {
		logger.Infow("No plugins enabled in configuration")
		return nil
	}

	// Build map of enabled plugins for deduplication.
	// Entries may be bare names or repo URLs — both reduce to a plugin name.
	enabledPlugins := make(map[string]bool)
	for _, name := range cfg.Plugin.EnabledNames() {
		enabledPlugins[name] = true
	}

	// Sort plugin names for deterministic iteration
	pluginNames := make([]string, 0, len(enabledPlugins))
	for name := range enabledPlugins {
		pluginNames = append(pluginNames, name)
	}
	sort.Strings(pluginNames)

	// Discover plugins from configured paths (deduplicated), fetching any that
	// declared a repo and are not on disk
	var pluginConfigs []PluginConfig
	var failedPlugins []string
	for _, pluginName := range pluginNames {
		logger.Debugf("Searching for '%s' plugin binary in %d paths", pluginName, len(cfg.Plugin.Paths))

		pluginConfig, err := resolvePlugin(ctx, pluginName, cfg.Plugin.Paths, logger)
		if err != nil {
			// Hints carry the actionable half of these errors ("set access_token",
			// "install the binary") — without this they never reach the operator.
			logger.Warnf("Plugin '%s' unavailable: %v - searched paths: %v, tried names: [qntx-%s-plugin, qntx-%s, %s]%s",
				pluginName, err, cfg.Plugin.Paths, pluginName, pluginName, pluginName,
				formatHints(err))
			failedPlugins = append(failedPlugins, pluginName)
			manager.mu.Lock()
			manager.failedPlugins[pluginName] = err.Error()
			manager.mu.Unlock()
			continue
		}
		// Read per-plugin args from am.toml (e.g. [myplugin] args = ["--name", "myplugin"])
		if args := config.GetStringSlice(pluginName + ".args"); len(args) > 0 {
			pluginConfig.Args = args
		}
		logger.Debugf("Will load '%s' plugin from binary: %s", pluginName, pluginConfig.Binary)
		pluginConfigs = append(pluginConfigs, pluginConfig)
	}

	// Load discovered plugins
	if len(pluginConfigs) > 0 {
		if err := manager.LoadPlugins(ctx, pluginConfigs); err != nil {
			return errors.Wrap(err, "failed to load plugins")
		}

		// Configure WebSocket settings from config.Config
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
		logger.Debugw("Plugin discovery complete",
			"enabled", len(cfg.Plugin.Enabled),
			"loaded", len(pluginConfigs),
		)
	}

	return nil
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

// resolvePlugin finds a plugin binary on disk, fetching it from the plugin's
// declared repo when it is absent or no longer matches what that repo
// publishes.
//
// A plugin enabled by bare name never reaches the network: no repo, no fetch.
// A binary QNTX did not install is used as-is, whatever it is — hand-placing
// one stays a way to run a build of your own choosing.
//
// A plugin QNTX installed is reconciled against the release on every start.
// Without that, the first build to land is the last one that ever runs: a
// broken binary retries forever and a new release never arrives, both of them
// fixable only by deleting the file by hand on every host.
func resolvePlugin(ctx context.Context, name string, searchPaths []string, logger *zap.SugaredLogger) (PluginConfig, error) {
	pluginCfg, discoverErr := discoverPlugin(name, searchPaths, logger)

	repo := config.PluginRepo(name)
	if repo == "" {
		if discoverErr != nil {
			return PluginConfig{}, discoverErr
		}
		return pluginCfg, nil
	}

	if discoverErr == nil && !managedPluginIsStale(ctx, name, repo, pluginCfg.Binary, logger) {
		return pluginCfg, nil
	}

	if discoverErr != nil {
		logger.Infow("Plugin binary absent, fetching from its repo",
			"plugin", name, "repo", repo, "searched", searchPaths)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, PluginFetchTimeout)
	defer cancel()

	binary, err := fetchPlugin(fetchCtx, name, repo, logger)
	if err != nil {
		// Replacing an installed plugin is an improvement, not a requirement.
		// Losing a working plugin because the forge was unreachable would make
		// every start depend on the network.
		if discoverErr == nil {
			logger.Warnw("Could not fetch the newer plugin; keeping the installed one",
				"plugin", name, "repo", repo, "binary", pluginCfg.Binary, "error", err)
			return pluginCfg, nil
		}
		return PluginConfig{}, errors.Wrapf(err, "failed to fetch plugin '%s' from %s", name, repo)
	}

	return PluginConfig{
		Name:      name,
		Enabled:   true,
		Binary:    binary,
		AutoStart: true,
	}, nil
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
	return PluginConfig{}, errors.WithHintf(err, "install the binary to one of those paths, add its path to [plugin] paths, or enable '%s' by repo URL so QNTX fetches it", name)
}

// managedPluginIsStale reports whether an installed plugin no longer matches
// what its repo publishes, and so should be fetched again.
//
// Only a plugin under QNTX's own install directory is considered. A binary
// found anywhere else was put there deliberately and is never replaced.
//
// Every uncertain answer is false. A release that cannot be reached, a digest
// that cannot be read, a plugin installed before digests were recorded — none
// of those are grounds to discard a plugin that is on disk and may well work. A
// box with no network keeps running what it has.
func managedPluginIsStale(ctx context.Context, name, repo, binary string, logger *zap.SugaredLogger) bool {
	// An install from before plugins were unpacked as trees. QNTX put it there
	// and it can never carry a digest, so there is nothing to compare — but it
	// also shadows the tree that would replace it, so it is always superseded.
	if legacy, err := LegacyPluginInstallPath(name); err == nil && binary == legacy {
		logger.Infow("Plugin was installed before plugins were unpacked as trees, fetching it again",
			"plugin", name, "repo", repo, "binary", binary)
		return true
	}

	dir, err := PluginInstallPath(name)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(dir, binary)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}

	installed, ok := installedDigest(dir)
	if !ok {
		return false
	}

	checkCtx, cancel := context.WithTimeout(ctx, PluginDigestTimeout)
	defer cancel()

	published, err := publishedDigest(checkCtx, name, repo)
	if err != nil {
		logger.Warnw("Could not check the plugin against its latest release; keeping what is installed",
			"plugin", name, "repo", repo, "error", err)
		return false
	}

	if published == installed {
		return false
	}

	logger.Infow("Installed plugin differs from the latest release, fetching it again",
		"plugin", name, "repo", repo, "installed", installed, "published", published)

	return true
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
