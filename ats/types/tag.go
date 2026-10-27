package types

import (
	"slices"
	"strings"
)

// TagNamespace is what a tag predicate begins with.
//
// "Not just Users, but most things can have freely defined k:v tags"
//
// "v is tag:k of subject":
//
//	MyRootAgent is tag:Name of [agent did] by [user that tagged]
//
// The value is the subject, the key is the predicate, and the thing tagged is
// the context. "Name is the key": a thing's name is the value of its Name tag.
//
// "a tag is a type", and the key is the tag: `[Name] is type`.
const TagNamespace = "tag:"

// TagKey is the key a predicate names, and false for a predicate that names no
// tag. `tag:` alone names every tag rather than one, and the write path refuses
// it before this is asked.
func TagKey(predicate string) (string, bool) {
	if !strings.HasPrefix(predicate, TagNamespace) {
		return "", false
	}
	key := strings.TrimSpace(strings.TrimPrefix(predicate, TagNamespace))
	if key == "" {
		return "", false
	}
	return key, true
}

// TagKeys is every key these predicates name, in the order they were named and
// without repeats.
func TagKeys(predicates []string) []string {
	var keys []string
	seen := make(map[string]bool, len(predicates))
	for _, predicate := range predicates {
		key, ok := TagKey(predicate)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}

// TagDef is what a key says on the day it is born: its own name, and nothing
// about how it looks. The colour is attested, so it is somebody's to say.
func TagDef(key string) TypeDef {
	return TypeDef{Name: key, Label: key}
}

// TagDefs is what each of these keys says on the day it is born.
func TagDefs(keys []string) []TypeDef {
	defs := make([]TypeDef, 0, len(keys))
	for _, key := range keys {
		defs = append(defs, TagDef(key))
	}
	return defs
}

// TagsOf is the tags thing holds among found: each key and its value.
//
// One value per key: a thing tagged twice with the same key holds the newer,
// "Replace". Newer is the attestation's own timestamp — when it was made, not
// when it arrived.
func TagsOf(found []*As, thing string) map[string]string {
	held := map[string]string{}
	when := map[string]*As{}
	for _, as := range found {
		if len(as.Subjects) != 1 || !slices.Contains(as.Contexts, thing) {
			continue
		}
		for _, key := range TagKeys(as.Predicates) {
			if newest, ok := when[key]; ok && !as.Timestamp.After(newest.Timestamp) {
				continue
			}
			when[key] = as
			held[key] = as.Subjects[0]
		}
	}
	return held
}
