// Package slug holds the one reading of a namespace name a door finds it by.
//
// It is a package of its own because the two ends that need it cannot see each
// other: am.toml is read below the attestation store, so the config package
// cannot reach the store's, and the store cannot reach the config's.
package slug

import "strings"

// Of is the slug of a namespace name: the name lowercased, and nothing else.
//
// A namespace is kept under the name it was created with — "Clean" — and a
// door in am.toml is keyed by a TOML key, which arrives lowercase — "clean".
// The slug is where the two meet.
//
// It is not a sanitizer. Nothing but case is touched, so two names that differ
// by more than case have two slugs and no door confuses them.
func Of(name string) string { return strings.ToLower(name) }
