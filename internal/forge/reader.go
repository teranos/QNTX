package forge

import (
	"context"
	"io"

	errors "github.com/teranos/sacred-error"
)

// Acting is who the node acts as on a forge. A forge has both, and says which
// is which in its own words.
type Acting int

const (
	// AsNode is the node's own identity on the forge: what builds and checks
	// read with. On GitHub, the system namespace's token.
	AsNode Acting = iota + 1
	// AsApp is the application the node is installed as, where the repository
	// is. On GitHub, the App's installation.
	AsApp
)

func (a Acting) String() string {
	switch a {
	case AsNode:
		return "as the node"
	case AsApp:
		return "as the app"
	}
	return "as nobody"
}

// Root is the top of a repository, as a directory path.
const Root = "."

// Repository is what a forge says of a repository.
type Repository struct {
	FullName      string
	Private       bool
	DefaultBranch string
}

// EntryKind is what an entry of a directory is.
type EntryKind int

const (
	// File is a file.
	File EntryKind = iota + 1
	// Dir is a directory.
	Dir
	// Other is anything else a directory holds: a symlink, a submodule.
	Other
)

// Entry is one thing a directory holds.
type Entry struct {
	Name string
	Path string
	SHA  string
	Kind EntryKind
}

// Readme is a repository's or a directory's README, as text.
type Readme struct {
	Path string
	Text string
}

// Refused is the forge answering no: it was asked, and said why not. A forge
// that was not reached is an error of another kind.
type Refused struct{ Says string }

func (r Refused) Error() string { return r.Says }

// NotADirectory is what a directory was asked of, and it is something else.
type NotADirectory struct{ Kind string }

func (n NotADirectory) Error() string { return "it is a " + n.Kind + ", not a directory" }

// Reader is what the core asks of a forge to read a repository. Each adapter
// answers in these terms; what has no peer on another forge is not here, and
// stays in the adapter that has it.
type Reader interface {
	// Describe is the repository as the forge says it is.
	Describe(ctx context.Context, as Acting, repo Repo) (Repository, error)
	// Head is the commit ref points at, or the last commit on it that touched
	// path when one is named.
	Head(ctx context.Context, as Acting, repo Repo, ref, path string) (string, error)
	// Archive is the repository at rev, as a tarball.
	Archive(ctx context.Context, as Acting, repo Repo, rev string) (io.ReadCloser, error)
	// File is the content of the file at path on ref.
	File(ctx context.Context, as Acting, repo Repo, ref, path string) ([]byte, error)
	// KindAt is what is at path on ref.
	KindAt(ctx context.Context, as Acting, repo Repo, ref, path string) (EntryKind, error)
	// Entries is what the directory dir holds on ref. Root is the top.
	Entries(ctx context.Context, as Acting, repo Repo, ref, dir string) ([]Entry, error)
	// Readme is the README of dir on ref; Root is the repository's own.
	Readme(ctx context.Context, as Acting, repo Repo, ref, dir string) (Readme, error)
}

// Readers are the forges the node can read, by host.
type Readers struct {
	by map[string]Reader
}

// ReadersOf are the readers named, by the host each reads.
func ReadersOf(by map[string]Reader) Readers { return Readers{by: by} }

// For is the reader of repo's forge, or why there is none.
func (r Readers) For(repo Repo) (Reader, error) {
	reader, ok := r.by[repo.Host]
	if !ok {
		return nil, errors.Newf("the node reads no repositories on %s, and %s is on it", repo.Host, repo)
	}
	return reader, nil
}
