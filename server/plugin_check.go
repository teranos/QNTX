package server

// Check, the first stage of adding a plugin, asked of GitHubService as the node (ADR-043).
// "the node should be the root identity github for github as a base"

import (
	"context"
	"encoding/base64"
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
	// "I expected to also see the plugin README if there is one."
	Readme     string `json:"readme,omitempty"`
	ReadmePath string `json:"readme_path,omitempty"`
	// ReadmeSaid is what GitHub answered when no README came back.
	ReadmeSaid string `json:"readme_said,omitempty"`
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
	checked := checkedPlugin{
		Name: name, Repo: repo, Repository: found.FullName, Private: found.Private,
		Ref: ref, Path: source.Path,
	}

	if source.Path != "" {
		held, err := s.gitHubService().GetRepositoryContent(ctx, &protocol.GitHubGetRepositoryContentRequest{
			Owner: source.Owner, Repo: source.Repo, Path: source.Path, Ref: ref})
		if err != nil {
			return checkedPlugin{}, errors.Wrapf(err, "GitHubService did not answer for %s in %s at %s", source.Path, found.FullName, ref)
		}
		if !held.Success {
			return checkedPlugin{}, errors.Newf("%s is not in %s at %s: %s", source.Path, found.FullName, ref, held.Error)
		}
		// A directory answers with its entries; a file answers with itself.
		if held.Type != "" {
			return checkedPlugin{}, errors.Newf("%s in %s at %s is a %s, and a plugin is a directory", source.Path, found.FullName, ref, held.Type)
		}
	}

	readme, err := s.pluginReadme(ctx, source, ref)
	if err != nil {
		return checkedPlugin{}, err
	}
	if readme.Success {
		text, err := readmeText(readme)
		if err != nil {
			return checkedPlugin{}, errors.Wrapf(err, "the README at %s in %s at %s does not read", readme.Path, found.FullName, ref)
		}
		checked.Readme, checked.ReadmePath = text, readme.Path
	} else {
		checked.ReadmeSaid = readme.Error
	}
	return checked, nil
}

// pluginReadme is the README of the plugin's directory, or of the repository
// when the plugin is the whole of it.
func (s *QNTXServer) pluginReadme(ctx context.Context, source gitHubSource, ref string) (*protocol.GitHubGetARepositoryREADMEResponse, error) {
	if source.Path == "" {
		readme, err := s.gitHubService().GetARepositoryREADME(ctx, &protocol.GitHubGetARepositoryREADMERequest{
			Owner: source.Owner, Repo: source.Repo, Ref: ref})
		return readme, errors.Wrapf(err, "GitHubService did not answer for the README of %s/%s at %s", source.Owner, source.Repo, ref)
	}
	readme, err := s.gitHubService().GetARepositoryREADMEForADirectory(ctx, &protocol.GitHubGetARepositoryREADMEForADirectoryRequest{
		Owner: source.Owner, Repo: source.Repo, Dir: source.Path, Ref: ref})
	return readme, errors.Wrapf(err, "GitHubService did not answer for the README of %s in %s/%s at %s", source.Path, source.Owner, source.Repo, ref)
}

// readmeText is a README's content as text. GitHub sends it base64, wrapped in lines.
func readmeText(readme *protocol.GitHubGetARepositoryREADMEResponse) (string, error) {
	if readme.Encoding != "base64" {
		return "", errors.Newf("it is encoded as %q, and base64 is what is read", readme.Encoding)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(readme.Content, "\n", ""))
	if err != nil {
		return "", errors.Wrap(err, "its base64 does not decode")
	}
	return string(raw), nil
}
