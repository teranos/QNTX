// Package glyph is what a glyph is, as attestations record it.
//
// A glyph exists because somebody published its module, and what they published
// is an attestation. The shape of that attestation is a fact about the data —
// the route that serves it and the watcher that notices it are both readers of
// the same shape, and neither owns it.
package glyph

// SubjectPrefix is what a glyph module attestation is about, joined to the
// glyph's name. The shape /api/glyph-config already uses for glyph config.
const SubjectPrefix = "glyph-"

// ModulePredicate is the claim: this attestation carries the module.
//
// It is namespaced rather than the bare word `module` because a watcher matches
// on a predicate and nothing else — AxFilter names subjects exactly, with no
// prefix — so a bare word would wake every page on anything that ever used it.
const ModulePredicate = "glyph:module"

// SourceAttribute is the attribute the module's text is under.
const SourceAttribute = "source"

// Named is the glyph a subject is about, and whether it is about one at all.
func Named(subject string) (string, bool) {
	if len(subject) <= len(SubjectPrefix) || subject[:len(SubjectPrefix)] != SubjectPrefix {
		return "", false
	}
	return subject[len(SubjectPrefix):], true
}
