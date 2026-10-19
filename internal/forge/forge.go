package forge

import (
	"context"

	errors "github.com/teranos/sacred-error"
)

// Writer is what the core asks of a forge to put a change on it.
type Writer interface {
	// CreateBranch makes branch, at the commit from.
	CreateBranch(ctx context.Context, as Acting, repo Repo, branch, from string) error
	// AddFile puts a file that is not on branch there.
	AddFile(ctx context.Context, as Acting, repo Repo, branch, path string, content []byte, message string) error
	// ReplaceFile puts content over the file at path on branch, which is the
	// blob replaces.
	ReplaceFile(ctx context.Context, as Acting, repo Repo, branch, path string, content []byte, replaces, message string) error
	// DeleteFile takes the file at path, the blob sha, off branch.
	DeleteFile(ctx context.Context, as Acting, repo Repo, branch, path, sha, message string) error
	// NewChange proposes head into base.
	NewChange(ctx context.Context, as Acting, repo Repo, head, base, title, body string) (Change, error)
	// MergeChange merges the change as a merge commit, if it is still at sha,
	// and says the commit it made.
	MergeChange(ctx context.Context, as Acting, repo Repo, number int64, sha string) (string, error)
}

// CheckState is how one check stands.
type CheckState string

const (
	CheckWaiting CheckState = "waiting"
	CheckRunning CheckState = "running"
	CheckDone    CheckState = "done"
	CheckFailed  CheckState = "failed"
)

// CheckScope is which checks are asked about.
type CheckScope int

const (
	// OwnCI is the checks of the forge's own CI: on GitHub, Actions.
	OwnCI CheckScope = iota + 1
	// EveryCheck is every check anything reported, the forge's own CI or not.
	EveryCheck
)

// Checks is what CI says of a commit.
type Checks struct {
	// Ran is whether the forge's own CI made anything for the commit at all,
	// and Settled whether all of it finished. Both are told of OwnCI only.
	Ran     bool
	Settled bool
	// States is each check by name, the newest of a name standing.
	States map[string]CheckState
	// Failed is what did not conclude well, by name or by what it was.
	Failed []string
}

// Checker is what the core asks of a forge to learn how the CI of a commit, or
// of the commit a branch is at, stands.
type Checker interface {
	Checks(ctx context.Context, as Acting, repo Repo, ref string, scope CheckScope) (Checks, error)
}

// Forge is everything the core asks of a forge. What has no peer on another
// forge is not here; it is asked of that forge by name, where it is needed.
type Forge interface {
	Reader
	Writer
	Checker
}

// Forges are the forges the node reaches, by host.
type Forges struct {
	by map[string]Forge
}

// ForgesOf are the forges named, by the host each is on.
func ForgesOf(by map[string]Forge) Forges { return Forges{by: by} }

// For is the forge repo is on, or why there is none.
func (f Forges) For(repo Repo) (Forge, error) {
	forge, ok := f.by[repo.Host]
	if !ok {
		return nil, errors.Newf("the node reaches no repositories on %s, and %s is on it", repo.Host, repo)
	}
	return forge, nil
}
