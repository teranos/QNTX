// Package forge names a repository on any source code forge, and says what a
// push to one changed. GitHub and GitLab are peers here: a repository is its
// host and its path, and nothing is read from a name that has no host.
package forge

import (
	"strings"

	name "github.com/teranos/QNTX/internal/named"
	errors "github.com/teranos/sacred-error"
)

// GitHub is the host GitHub serves repositories from.
const GitHub = "github.com"

// Repo is a repository: the host that serves it and its path there. A path is
// two segments on GitHub, owner/repo, and more on a forge that nests, as
// group/subgroup/project.
type Repo struct {
	Host string
	Path string
}

// ParseRepo reads host/path, as github.com/owner/repo or
// gitlab.com/group/subgroup/project. A name with no host is refused: owner/repo
// says which forge it is on to nobody.
func ParseRepo(said string) (Repo, error) {
	trimmed := strings.TrimSuffix(strings.Trim(strings.TrimSpace(said), "/"), ".git")
	host, path, found := strings.Cut(trimmed, "/")
	if !found || !strings.Contains(host, ".") {
		return Repo{}, errors.Newf("%q names no host: github.com/owner/repo, or gitlab.com/group/project", said)
	}
	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		return Repo{}, errors.Newf("%q names no owner and repository after %s", said, host)
	}
	named := make([]string, 0, len(segments))
	for _, segment := range segments {
		text, err := name.TextOf("a segment of "+said, segment)
		if err != nil {
			return Repo{}, err
		}
		named = append(named, text.String())
	}
	return Repo{Host: host, Path: strings.Join(named, "/")}, nil
}

func (r Repo) String() string { return r.Host + "/" + r.Path }

// Is whether r and other are the same repository. Case is not told apart, as
// the matching it replaces did not.
func (r Repo) Is(other Repo) bool {
	return strings.EqualFold(r.Host, other.Host) && strings.EqualFold(r.Path, other.Path)
}
