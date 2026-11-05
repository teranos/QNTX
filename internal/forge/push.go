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
	// DefaultBranch is the repository's default branch, as the forge said it with the push.
	DefaultBranch string
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

// TouchedIn is whether the push changed a file directly in folder: one in a
// folder inside it is not in it.
func (p Push) TouchedIn(folder string) bool {
	for _, changed := range p.Changed {
		if name, in := strings.CutPrefix(changed, folder+"/"); in && !strings.Contains(name, "/") {
			return true
		}
	}
	return false
}
