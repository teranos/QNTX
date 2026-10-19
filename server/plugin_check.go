package server

// Check, the first stage of adding a plugin, asked of the plugin's forge as the node (ADR-043).
// "the node should be the root identity github for github as a base"

import (
	"context"
	"net/url"
	"strings"

	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
)

// TODO(forge): A plugin's repository URL is read for any forge the node reaches: a forge.Repo, a ref and a path. The /tree/<ref>/<path> form is GitHub's; another forge's tree URL is read by its adapter.
// gitHubSource is a plugin's repository URL, read into the parts GitHub names.
// A tree URL names a Ref, and a Path when the plugin is a directory of it; a
// plugin that is the whole repository names neither.
type gitHubSource struct {
	Owner     string
	Repo      string
	Ref       string
	RefNamed  bool
	Path      string
	PathNamed bool
}

// readGitHubSource reads github.com/owner/repo, or .../tree/<ref>/<path>.
func readGitHubSource(repo string) (gitHubSource, error) {
	u, err := url.Parse(strings.TrimSpace(repo))
	if err != nil {
		return gitHubSource{}, errors.Wrapf(err, "%q is not a repository URL", repo)
	}
	// TODO(forge): Any host the node has a Forge for, not github.com alone.
	if u.Host != "github.com" {
		return gitHubSource{}, errors.Newf("%q is not on github.com, and a plugin's repository is", repo)
	}
	// An owner or repository named empty is one GitHub does not have, and it
	// says so when asked.
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return gitHubSource{}, errors.Newf("%q names no owner/repo", repo)
	}
	source := gitHubSource{Owner: parts[0], Repo: strings.TrimSuffix(parts[1], ".git")}
	if len(parts) == 2 {
		return source, nil
	}
	if parts[2] != "tree" || len(parts) < 4 {
		return gitHubSource{}, errors.Newf("%q continues past owner/repo with %q, and only a tree URL does", repo, parts[2])
	}
	source.Ref, source.RefNamed = parts[3], true
	source.Path, source.PathNamed = strings.Join(parts[4:], "/"), len(parts) > 4
	return source, nil
}

// checkPlugin asks GitHub, as the node, whether repo is there. Nothing is
// written, installed or downloaded.
func (s *QNTXServer) checkPlugin(ctx context.Context, repo string) (*protocol.PluginChecked, error) {
	repo = strings.TrimSpace(repo)
	source, err := readGitHubSource(repo)
	if err != nil {
		return nil, err
	}
	// A repository on GitHub names its owner and itself, so a plugin read from
	// one is named; "." is a directory naming no plugin.
	name := config.PluginNameFromRepo(repo)
	if name == "." {
		return nil, errors.Newf("%q names no plugin", repo)
	}
	// TODO(forge): The repository is the URL's own host and path, once the URL is read for any forge.
	// The node reads as itself.
	where := forge.Repo{Host: forge.GitHub, Path: source.Owner + "/" + source.Repo}
	reader, err := s.forges().For(where)
	if err != nil {
		return nil, err
	}
	var refused forge.Refused
	found, err := reader.Describe(ctx, forge.AsNode, where)
	if errors.As(err, &refused) {
		return nil, errors.Newf("%s: %s", where.Path, refused.Says)
	}
	if err != nil {
		return nil, err
	}
	// A URL naming no ref is the repository as its default branch is.
	ref := found.DefaultBranch
	if source.RefNamed {
		ref = source.Ref
	}
	checked := &protocol.PluginChecked{
		Name: name, Repo: repo, Repository: found.FullName, Private: found.Private,
		Ref: ref, Path: source.Path,
	}

	if source.PathNamed {
		kind, err := reader.KindAt(ctx, forge.AsNode, where, ref, source.Path)
		if errors.As(err, &refused) {
			return nil, errors.Newf("%s is not in %s at %s: %s", source.Path, found.FullName, ref, refused.Says)
		}
		if err != nil {
			return nil, errors.Wrapf(err, "%s in %s at %s", source.Path, found.FullName, ref)
		}
		if kind != forge.Dir {
			return nil, errors.Newf("%s in %s at %s: is not a directory, and a plugin is", source.Path, found.FullName, ref)
		}
	}

	// The README of the plugin's directory, or of the repository when the
	// plugin is the whole of it.
	readmeOf := forge.Root
	if source.PathNamed {
		readmeOf = source.Path
	}
	readme, err := reader.Readme(ctx, forge.AsNode, where, ref, readmeOf)
	if errors.As(err, &refused) {
		checked.ReadmeSaid = refused.Says
		return checked, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "the README of %s in %s at %s does not read", readmeOf, found.FullName, ref)
	}
	checked.Readme, checked.ReadmePath = readme.Text, readme.Path
	return checked, nil
}
