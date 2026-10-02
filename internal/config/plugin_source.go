package config

import (
	"net/url"
	"path"
	"strings"

	"github.com/teranos/errors"
)

// PluginNameFromRepo derives the plugin name from a repo URL: the last path
// segment, with any .git suffix removed.
//
//	https://github.com/sbvh-nl/duif                          → duif
//	https://github.com/teranos/QNTX/tree/main/qntx-plugins/kern → kern
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
// configuration. Used where only the host is in hand — the CI watch.
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
