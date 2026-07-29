package config

import (
	"net/url"
	"path"
	"strings"
)

// EnabledPlugin is one parsed entry from [plugin] enabled.
//
// An entry says which plugin the operator wants and, optionally, where it comes
// from. A bare name asserts the binary is already on disk and never reaches the
// network. A repo URL says QNTX may go get it when it is absent.
type EnabledPlugin struct {
	// Name is the plugin identity used everywhere else: registry key, config
	// section, log field, and the name the binary must report over gRPC.
	Name string

	// Repo is the source to fetch from when the binary is absent.
	// Empty for bare-name entries.
	Repo string
}

// ParseEnabledEntry splits a [plugin] enabled entry into name and source.
// An entry is a URL if it parses as one, a name otherwise — so both forms
// can be mixed freely in the same list.
func ParseEnabledEntry(entry string) EnabledPlugin {
	entry = strings.TrimSpace(entry)

	u, err := url.Parse(entry)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return EnabledPlugin{Name: entry}
	}

	return EnabledPlugin{Name: PluginNameFromRepo(entry), Repo: entry}
}

// PluginNameFromRepo derives the plugin name from a repo URL: the last path
// segment, with any .git suffix removed.
//
//	https://github.com/sbvh-nl/duif → duif
func PluginNameFromRepo(repo string) string {
	trimmed := strings.TrimSuffix(strings.TrimRight(repo, "/"), ".git")
	return path.Base(trimmed)
}

// EnabledPlugins returns the parsed [plugin] enabled list.
func (c *PluginConfig) EnabledPlugins() []EnabledPlugin {
	parsed := make([]EnabledPlugin, 0, len(c.Enabled))
	for _, entry := range c.Enabled {
		parsed = append(parsed, ParseEnabledEntry(entry))
	}
	return parsed
}

// EnabledNames returns the plugin names from [plugin] enabled, with repo URLs
// reduced to the name they derive.
func (c *PluginConfig) EnabledNames() []string {
	names := make([]string, 0, len(c.Enabled))
	for _, entry := range c.Enabled {
		names = append(names, ParseEnabledEntry(entry).Name)
	}
	return names
}

// RepoFor returns the source repo declared for a plugin, or "" when the plugin
// was enabled by bare name and must already be on disk.
func (c *PluginConfig) RepoFor(name string) string {
	for _, entry := range c.Enabled {
		if parsed := ParseEnabledEntry(entry); parsed.Name == name {
			return parsed.Repo
		}
	}
	return ""
}

// PluginRepo reads the source repo for a plugin from the loaded configuration.
// Used where only the plugin name is in hand — runtime enable and restart.
func PluginRepo(name string) string {
	for _, entry := range GetStringSlice("plugin.enabled") {
		if parsed := ParseEnabledEntry(entry); parsed.Name == name {
			return parsed.Repo
		}
	}
	return ""
}

// PluginAccessToken reads the secret reference configured for a forge host.
// Tokens are set once per host in [plugin.access_token], not per plugin.
// Returns "" for a host with no entry — public repos need none.
func PluginAccessToken(host string) string {
	return GetStringMapString("plugin.access_token")[strings.ToLower(host)]
}
