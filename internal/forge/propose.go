package forge

import (
	"context"
	"strings"

	errors "github.com/teranos/sacred-error"
)

// "I guess the point that I am making is that it shouldn't matter what forge we use"

// Proposal is a change proposed to a repository: the branch that holds it, and
// the branch it is proposed into. It ends as a Change, a pull request on one
// forge and a merge request on another, and what is asked of the forge on the
// way there is said here, once, for whoever proposes.
type Proposal struct {
	Repo Repo
	// Branch holds the change. It is made from Into when the forge has none.
	Branch string
	// Into is the branch the change is proposed into.
	Into string
}

// editKind is what an edit does to a file. It starts above zero, so an edit
// that was never said to do anything does nothing.
type editKind int

const (
	added editKind = iota + 1
	replaced
	removed
)

// Edit is one file a proposal changes on its branch: Added, Replaced or
// Removed says which.
type Edit struct {
	Path    string
	kind    editKind
	content []byte
	blob    string
	message string
}

// Added is a file the branch has none of, put there holding content.
func Added(path string, content []byte, message string) Edit {
	return Edit{Path: path, kind: added, content: content, message: message}
}

// Replaced is content put over the file at path, which is the blob there now.
func Replaced(path string, content []byte, blob, message string) Edit {
	return Edit{Path: path, kind: replaced, content: content, blob: blob, message: message}
}

// Removed is the file at path, the blob there now, taken off the branch.
func Removed(path, blob, message string) Edit {
	return Edit{Path: path, kind: removed, blob: blob, message: message}
}

// Committed is what reached a proposal's branch, by path.
type Committed struct {
	// Put is each file added or replaced.
	Put []string
	// Removed is each file taken off.
	Removed []string
}

// HoldBranch makes sure the proposal's branch is on the forge, made from the
// branch it is proposed into when it is not there yet.
func (p Proposal) HoldBranch(ctx context.Context, on Forge, as Acting) error {
	_, err := on.Branch(ctx, as, p.Repo, p.Branch)
	var absent Refused
	if !errors.As(err, &absent) {
		return err
	}
	from, err := on.Branch(ctx, as, p.Repo, p.Into)
	var fromAbsent Refused
	if errors.As(err, &fromAbsent) {
		return errors.Newf("%s is not on %s (%s), and %s, which it would be made from, is not either: %s",
			p.Branch, p.Repo, absent.Says, p.Into, fromAbsent.Says)
	}
	if err != nil {
		return err
	}
	err = on.CreateBranch(ctx, as, p.Repo, p.Branch, from)
	var refused Refused
	if errors.As(err, &refused) {
		return errors.Newf("%s is not on %s (%s), and was not made from %s: %s", p.Branch, p.Repo, absent.Says, p.Into, refused.Says)
	}
	return err
}

// Holds is the blob the proposal's branch holds at each of paths, for the
// ones it holds a file at: what an edit of that file replaces or removes. A
// directory the forge says it has none of holds nothing.
func (p Proposal) Holds(ctx context.Context, on Forge, as Acting, paths []string) (map[string]string, error) {
	asked := map[string]bool{}
	for _, file := range paths {
		asked[file] = true
	}
	held := map[string]string{}
	listed := map[string]bool{}
	for _, file := range paths {
		dir := Root
		if cut := strings.LastIndex(file, "/"); cut >= 0 {
			dir = file[:cut]
		}
		if listed[dir] {
			continue
		}
		listed[dir] = true
		entries, err := on.Entries(ctx, as, p.Repo, p.Branch, dir)
		var absent Refused
		if errors.As(err, &absent) {
			continue
		}
		var notADirectory NotADirectory
		if errors.As(err, &notADirectory) {
			return nil, errors.Newf("%s is a %s of %s on %s, not a folder, so no file is held in it", dir, notADirectory.Kind, p.Repo, p.Branch)
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !asked[entry.Path] {
				continue
			}
			// What is there and no file is neither added over nor replaced:
			// said now, before any edit reaches the branch.
			switch entry.Kind {
			case File:
				held[entry.Path] = entry.SHA
			case Dir:
				return nil, errors.Newf("%s is a folder of %s on %s, not a file", entry.Path, p.Repo, p.Branch)
			default:
				return nil, errors.Newf("%s is held by %s on %s as no file and no folder", entry.Path, p.Repo, p.Branch)
			}
		}
	}
	return held, nil
}

// Commit puts each edit on the proposal's branch, in the order given, a commit
// an edit. It stops at the first one the forge refuses, and what reached the
// branch before it is said with the refusal.
func (p Proposal) Commit(ctx context.Context, on Forge, as Acting, edits []Edit) (Committed, error) {
	done := Committed{Put: []string{}, Removed: []string{}}
	for _, edit := range edits {
		var err error
		switch edit.kind {
		case added:
			err = on.AddFile(ctx, as, p.Repo, p.Branch, edit.Path, edit.content, edit.message)
		case replaced:
			err = on.ReplaceFile(ctx, as, p.Repo, p.Branch, edit.Path, edit.content, edit.blob, edit.message)
		case removed:
			err = on.DeleteFile(ctx, as, p.Repo, p.Branch, edit.Path, edit.blob, edit.message)
		default:
			return done, errors.Newf("%s of %s is edited without saying whether it is added, replaced or removed", edit.Path, p.Repo)
		}
		var refused Refused
		if errors.As(err, &refused) {
			if edit.kind == removed {
				return done, errors.Newf("%s was not removed from %s of %s: %s", edit.Path, p.Branch, p.Repo, refused.Says)
			}
			return done, errors.Newf("%s was not committed to %s of %s: %s", edit.Path, p.Branch, p.Repo, refused.Says)
		}
		if err != nil {
			return done, err
		}
		if edit.kind == removed {
			done.Removed = append(done.Removed, edit.Path)
		} else {
			done.Put = append(done.Put, edit.Path)
		}
	}
	return done, nil
}

// OpenChange is the open change proposing the branch into the branch it is
// proposed into, and whether there is one.
func (p Proposal) OpenChange(ctx context.Context, on Forge, as Acting) (Change, bool, error) {
	open, found, err := on.ChangeFrom(ctx, as, p.Repo, p.Branch, p.Into)
	var refused Refused
	if errors.As(err, &refused) {
		return Change{}, false, errors.Newf("the open changes from %s of %s were not listed: %s", p.Branch, p.Repo, refused.Says)
	}
	if err != nil {
		return Change{}, false, err
	}
	return open, found, nil
}

// Propose is the change that proposes the branch: the one that is open, or one
// opened now under title and body when none is.
func (p Proposal) Propose(ctx context.Context, on Forge, as Acting, title, body string) (Change, error) {
	open, found, err := p.OpenChange(ctx, on, as)
	if err != nil || found {
		return open, err
	}
	made, err := on.NewChange(ctx, as, p.Repo, p.Branch, p.Into, title, body)
	var refused Refused
	if errors.As(err, &refused) {
		return Change{}, errors.Newf("no change from %s into %s of %s was opened: %s", p.Branch, p.Into, p.Repo, refused.Says)
	}
	if err != nil {
		return Change{}, err
	}
	return made, nil
}
