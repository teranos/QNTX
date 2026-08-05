package config

import (
	"net/url"
	"path"
	"strings"

	"github.com/teranos/errors"
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
//	https://github.com/sbvh-nl/duif                          → duif
//	https://github.com/teranos/QNTX/tree/main/qntx-plugins/loom → loom
//	https://github.com/teranos/pyre/tree/main/               → pyre
//
// The last segment is only the name once tree and its ref are accounted for.
// Taking it blindly names the third case after the branch.
func PluginNameFromRepo(repo string) string {
	trimmed := strings.TrimSuffix(strings.TrimRight(repo, "/"), ".git")

	if u, err := url.Parse(trimmed); err == nil {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 4 && parts[2] == "tree" {
			if len(parts) > 4 {
				return parts[len(parts)-1]
			}
			return strings.TrimSuffix(parts[1], ".git")
		}
	}

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

// RefForHost returns the secret reference configured for a forge host, or ""
// when the host has no entry — public repos need no credential.
// Credentials are set once per host, not per plugin.
func (c *PluginConfig) RefForHost(host string) string {
	for _, entry := range c.AccessToken {
		if strings.EqualFold(entry.Host, host) {
			return entry.Ref
		}
	}
	return ""
}

// PluginAccessToken reads the secret reference for a forge host from the loaded
// configuration. Used where only the host is in hand — runtime fetch.
//
// Decodes [[plugin.access_token]] into the slice rather than indexing a map by
// host: the host is a value here, so a dot in it stays a dot.
func PluginAccessToken(host string) (string, error) {
	v, err := initViper()
	if err != nil {
		return "", errors.Wrap(err, "failed to load configuration to read plugin.access_token")
	}
	if v == nil {
		return "", nil
	}

	var refs []AccessTokenRef
	if err := v.UnmarshalKey("plugin.access_token", &refs); err != nil {
		return "", errors.Wrap(err, "failed to decode plugin.access_token")
	}

	cfg := PluginConfig{AccessToken: refs}
	return cfg.RefForHost(host), nil
}
