// Package element is what an element is, as attestations record it.
//
// An element exists because somebody published its module, and what they published
// is an attestation. The shape of that attestation is a fact about the data —
// the route that serves it and the watcher that notices it are both readers of
// the same shape, and neither owns it.
package element

// SubjectPrefix is what an element module attestation is about, joined to the
// element's name. The shape /api/element-config already uses for element config.
const SubjectPrefix = "element-"

// ModulePredicate is the claim: this attestation carries the module.
//
// It is namespaced rather than the bare word `module` because a watcher matches
// on a predicate and nothing else — AxFilter names subjects exactly, with no
// prefix — so a bare word would wake every page on anything that ever used it.
const ModulePredicate = "element:module"

// SourceAttribute is the attribute the module's text is under.
const SourceAttribute = "source"

// Named is the element a subject is about, and whether it is about one at all.
func Named(subject string) (string, bool) {
	if len(subject) <= len(SubjectPrefix) || subject[:len(SubjectPrefix)] != SubjectPrefix {
		return "", false
	}
	return subject[len(SubjectPrefix):], true
}
