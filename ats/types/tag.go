package types

import "strings"

// TagNamespace is what a tag predicate begins with.
//
// `[subject] is tag:ci-runner by [actor]` says the subject is tagged ci-runner,
// and who says so. The tag itself is a type: "restaurant is real because
// someone attested it, not because a schema declares it" (ADR-026) is a
// description of tagging, so a tag is attested the way any type is.
//
// Two claims, and they are different ones. `[ci-runner] is type` says the tag
// exists. `[subject] is tag:ci-runner` says this thing has it.
const TagNamespace = "tag:"

// Tagged is the tag a predicate names, and false for a predicate that names no
// tag. `tag:` alone names every tag rather than one, and the write path refuses
// it before this is asked.
func Tagged(predicate string) (string, bool) {
	if !strings.HasPrefix(predicate, TagNamespace) {
		return "", false
	}
	tag := strings.TrimSpace(strings.TrimPrefix(predicate, TagNamespace))
	if tag == "" {
		return "", false
	}
	return tag, true
}

// TagsNamed is every tag these predicates name, in the order they were named
// and without repeats.
func TagsNamed(predicates []string) []string {
	var named []string
	seen := make(map[string]bool, len(predicates))
	for _, predicate := range predicates {
		tag, ok := Tagged(predicate)
		if !ok || seen[tag] {
			continue
		}
		seen[tag] = true
		named = append(named, tag)
	}
	return named
}

// TagDef is what a tag says on the day it is born: its own name, and nothing
// about how it looks.
//
// The colour is attested, so it is somebody's to say — and saying it here would
// be this build having an opinion about what ci-runner looks like, which it has
// no way to hold. EnsureTypesExist leaves a tag alone once it says anything, so
// the first person to attest a colour keeps it.
func TagDef(tag string) TypeDef {
	return TypeDef{Name: tag, Label: tag}
}

// TagDefs is what each of these tags says on the day it is born.
func TagDefs(tags []string) []TypeDef {
	defs := make([]TypeDef, 0, len(tags))
	for _, tag := range tags {
		defs = append(defs, TagDef(tag))
	}
	return defs
}
