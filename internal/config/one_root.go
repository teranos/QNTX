package config

import (
	"maps"
	"slices"
	"strings"

	"github.com/spf13/viper"
)

// "how many instances of the same thing are there in it that can easily be
// derived and overwritten on divergence ?"

// fromOneRoot derives what follows from auth.rp_id where am.toml is read.
// A key written in am.toml stands, written empty included; an omitted one is derived.
func (c *Config) fromOneRoot(v *viper.Viper) {
	root := c.Auth.RPID
	// "localhost" is the dev node: auth.New's loopback fallback answers for it.
	named := root != "" && !strings.EqualFold(root, "localhost")

	if named && !v.IsSet("auth.rp_origins") {
		c.Auth.RPOrigins = []string{"https://" + root}
	}
	if named && !v.IsSet("mail.from") {
		c.Mail.From = "system@" + root
	}

	// Two origins are a choice of suffix only am.toml can make.
	for namespace, door := range c.Auth.Door {
		if v.IsSet("auth.door."+namespace+".rp_id") || len(door.Origins) != 1 {
			continue
		}
		if host, web := webHost(door.Origins[0]); web {
			door.RPID = host
			c.Auth.Door[namespace] = door
		}
	}

	// An app's scheme is a return address, and is allowed only where written.
	allowed := slices.Clone(c.Server.AllowedOrigins)
	add := func(origins []string) {
		for _, origin := range origins {
			if _, web := webHost(origin); web && !slices.Contains(allowed, origin) {
				allowed = append(allowed, origin)
			}
		}
	}
	add(c.Auth.RPOrigins)
	for _, namespace := range slices.Sorted(maps.Keys(c.Auth.Door)) {
		add(c.Auth.Door[namespace].Origins)
	}
	c.Server.AllowedOrigins = allowed
}

// webHost is the host of an http or https origin: no port, lowercased.
// False for an app's own scheme, which has no host.
func webHost(origin string) (string, bool) {
	rest, found := strings.CutPrefix(origin, "https://")
	if !found {
		rest, found = strings.CutPrefix(origin, "http://")
	}
	if !found {
		return "", false
	}
	if cut := strings.Index(rest, "/"); cut != -1 {
		rest = rest[:cut]
	}
	if cut := strings.LastIndex(rest, ":"); cut != -1 && !strings.Contains(rest[cut:], "]") {
		rest = rest[:cut]
	}
	host := strings.ToLower(strings.TrimSuffix(rest, "."))
	return host, host != ""
}
