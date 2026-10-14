package config

import (
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/viper"

	errors "github.com/teranos/sacred-error"
)

// "absence is absence": a key am.toml left out is not a value. What was said
// is recorded when the config is read, so a key left out is told apart from
// one said as zero, false or empty.

// keysOf is every key the Config reads, by its mapstructure tags. A map or a
// list of tables is one key: what is inside it is the entry's, not the node's.
func keysOf(t reflect.Type, prefix string) []string {
	var keys []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag, tagged := field.Tag.Lookup("mapstructure")
		if !field.IsExported() || !tagged {
			continue
		}
		name, option, optioned := strings.Cut(tag, ",")
		if optioned && option == "squash" {
			keys = append(keys, keysOf(field.Type, prefix)...)
			continue
		}
		key := prefix + name
		inner := field.Type
		if inner.Kind() == reflect.Pointer {
			inner = inner.Elem()
		}
		if inner.Kind() == reflect.Struct {
			keys = append(keys, keysOf(inner, key+".")...)
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

// record notes which of the Config's keys v holds: a file, an environment
// variable or a flag said it.
func (c *Config) record(v *viper.Viper) {
	c.said = map[string]bool{}
	for _, key := range keysOf(reflect.TypeOf(*c), "") {
		if v.IsSet(key) {
			c.said[key] = true
		}
	}
}

// Said reports whether am.toml, the environment or a flag said key.
func (c *Config) Said(key string) bool {
	return c.said[key]
}

// RefuseUnsaid is a refusal naming every one of keys nothing said, and why
// it stops what was asked; nil when each was said.
func (c *Config) RefuseUnsaid(why string, keys ...string) error {
	if !slices.ContainsFunc(keys, func(key string) bool { return !c.said[key] }) {
		return nil
	}
	return errors.Newf("am.toml does not say %s: %s", strings.Join(c.Unsaid(keys...), ", "), why)
}

// Unsaid is the keys of these that nothing said, in order.
func (c *Config) Unsaid(keys ...string) []string {
	var unsaid []string
	for _, key := range keys {
		if !c.said[key] {
			unsaid = append(unsaid, key)
		}
	}
	sort.Strings(unsaid)
	return unsaid
}
