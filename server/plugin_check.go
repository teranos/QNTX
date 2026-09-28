package server

// Check, the first stage of adding a plugin, asked of GitHubService as the node (ADR-043).
// "the node should be the root identity github for github as a base"

import (
	"context"
	"net/url"
	"strings"

	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
)

// gitHubSource is a plugin's repository URL, read into the parts GitHub names.
// Ref and Path are empty for a plugin that is the whole repository.
type gitHubSource struct {
	Owner string
	Repo  string
	Ref   string
	Path  string
}

// readGitHubSource reads github.com/owner/repo, or .../tree/<ref>/<path>.
func readGitHubSource(repo string) (gitHubSource, error) {
	u, err := url.Parse(strings.TrimSpace(repo))
	if err != nil || u.Host == "" {
		return gitHubSource{}, errors.Newf("%q is not a repository URL", repo)
	}
	if u.Host != "github.com" {
		return gitHubSource{}, errors.Newf("%q is on %s, and a plugin's repository is on github.com", repo, u.Host)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return gitHubSource{}, errors.Newf("%q names no owner/repo", repo)
	}
	source := gitHubSource{Owner: parts[0], Repo: strings.TrimSuffix(parts[1], ".git")}
	if len(parts) == 2 {
		return source, nil
	}
	if parts[2] != "tree" || len(parts) < 4 {
		return gitHubSource{}, errors.Newf("%q continues past owner/repo with %q, and only a tree URL does", repo, parts[2])
	}
	source.Ref = parts[3]
	source.Path = strings.Join(parts[4:], "/")
	return source, nil
}

// checkedPlugin is what Check found.
type checkedPlugin struct {
	Name       string `json:"name"`
	Repo       string `json:"repo"`
	Repository string `json:"repository"`
	Private    bool   `json:"private"`
	Ref        string `json:"ref"`
	Path       string `json:"path,omitempty"`
}

// checkPlugin asks GitHub, as the node, whether repo is there. Nothing is
// written, installed or downloaded.
func (s *QNTXServer) checkPlugin(ctx context.Context, repo string) (checkedPlugin, error) {
	repo = strings.TrimSpace(repo)
	name := config.PluginNameFromRepo(repo)
	if name == "" || name == "." || name == "/" {
		return checkedPlugin{}, errors.Newf("%q names no plugin", repo)
	}
	source, err := readGitHubSource(repo)
	if err != nil {
		return checkedPlugin{}, err
	}
	// The empty namespace is the node's own GitHub.
	found, err := s.gitHubService().GetARepository(ctx, &protocol.GitHubGetARepositoryRequest{Owner: source.Owner, Repo: source.Repo})
	if err != nil {
		return checkedPlugin{}, errors.Wrapf(err, "GitHubService did not answer for %s/%s", source.Owner, source.Repo)
	}
	if !found.Success {
		return checkedPlugin{}, errors.Newf("%s/%s: %s", source.Owner, source.Repo, found.Error)
	}
	ref := source.Ref
	if ref == "" {
		ref = found.DefaultBranch
	}
	return checkedPlugin{
		Name: name, Repo: repo, Repository: found.FullName, Private: found.Private,
		Ref: ref, Path: source.Path,
	}, nil
}
