package forge

import (
	"slices"
	"strings"
)

// Push is what a forge said of a push, in the part any forge says: where it
// went, to which ref, what it left the ref at, and which files it changed.
type Push struct {
	Repo    Repo
	Ref     string
	After   string
	Deleted bool
	// Changed is every file the push added, removed or modified.
	Changed []string
}

// Branch is the branch pushed to, or empty for a tag.
func (p Push) Branch() string {
	branch, ok := strings.CutPrefix(p.Ref, "refs/heads/")
	if !ok {
		return ""
	}
	return branch
}

// Tag is the tag pushed, or empty for a branch.
func (p Push) Tag() string {
	tag, ok := strings.CutPrefix(p.Ref, "refs/tags/")
	if !ok {
		return ""
	}
	return tag
}

// Touched is whether the push changed path.
func (p Push) Touched(path string) bool { return slices.Contains(p.Changed, path) }

// TouchedUnder is whether the push changed anything inside folder.
func (p Push) TouchedUnder(folder string) bool {
	for _, changed := range p.Changed {
		if strings.HasPrefix(changed, folder+"/") {
			return true
		}
	}
	return false
}
