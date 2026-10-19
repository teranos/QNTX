package server

// GitHub as a forge.Reader: the one place the core's reading of a repository
// is put in GitHub's words. What GitHub has and another forge may not (the
// App's installations, workflow-run artifacts, check suites) is not here, and
// is asked of GitHubService where it is needed, by name.

import (
	"context"
	"encoding/base64"
	"io"
	"strings"

	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
)

// readers are the forges the node reads.
func (s *QNTXServer) readers() forge.Readers {
	return forge.ReadersOf(map[string]forge.Reader{forge.GitHub: gitHubReader{service: s.gitHubService}})
}

// gitHubReader reads repositories on github.com through GitHubService.
type gitHubReader struct {
	service func() *services.GitHubServer
}

var _ forge.Reader = gitHubReader{}

// who is the context and the namespace GitHubService is asked with, as the
// node is, or as the App's installation where the repository is.
func (r gitHubReader) who(ctx context.Context, as forge.Acting) (context.Context, string, error) {
	switch as {
	case forge.AsNode:
		return ctx, auth.NamespaceSystem, nil
	case forge.AsApp:
		return services.AsInstallation(ctx), "", nil
	}
	return ctx, "", errors.Newf("GitHub is not read %s", as)
}

func (r gitHubReader) Describe(ctx context.Context, as forge.Acting, repo forge.Repo) (forge.Repository, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return forge.Repository{}, err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return forge.Repository{}, err
	}
	found, err := r.service().GetARepository(ctx, &protocol.GitHubGetARepositoryRequest{Namespace: namespace, Owner: on.Owner, Repo: on.Name})
	if err != nil {
		return forge.Repository{}, errors.Wrapf(err, "GitHubService did not answer for %s", on)
	}
	if !found.GetSuccess() {
		return forge.Repository{}, forge.Refused{Says: found.GetError()}
	}
	return forge.Repository{FullName: found.GetFullName(), Private: found.GetPrivate(), DefaultBranch: found.GetDefaultBranch()}, nil
}

func (r gitHubReader) Head(ctx context.Context, as forge.Acting, repo forge.Repo, ref, path string) (string, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return "", err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return "", err
	}
	said, err := r.service().ListCommits(ctx, &protocol.GitHubListCommitsRequest{
		Namespace: namespace, Owner: on.Owner, Repo: on.Name, Sha: ref, Path: path, PerPage: 1,
	})
	if err != nil {
		return "", err
	}
	if !said.GetSuccess() {
		return "", forge.Refused{Says: said.GetError()}
	}
	// PerPage is 1: the one commit GitHub hands back is where ref is now.
	for _, commit := range said.GetItems() {
		return commit.GetSha(), nil
	}
	return "", errors.Newf("GitHub has no commit for %s@%s", on, ref)
}

func (r gitHubReader) Archive(ctx context.Context, as forge.Acting, repo forge.Repo, rev string) (io.ReadCloser, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return nil, err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return nil, err
	}
	return r.service().Tarball(ctx, namespace, on.Owner, on.Name, rev)
}

func (r gitHubReader) File(ctx context.Context, as forge.Acting, repo forge.Repo, ref, path string) ([]byte, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return nil, err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return nil, err
	}
	answered, err := r.service().GetRepositoryContent(ctx, &protocol.GitHubGetRepositoryContentRequest{
		Namespace: namespace, Owner: on.Owner, Repo: on.Name, Path: path, Ref: ref,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "%s of %s on %s was not asked", path, on, ref)
	}
	if !answered.GetSuccess() {
		return nil, forge.Refused{Says: answered.GetError()}
	}
	if answered.GetEncoding() != "base64" {
		return nil, errors.Newf("GitHub gave %s of %s on %s encoded as %q, which the node does not read; a file over 1 MB comes with none",
			path, on, ref, answered.GetEncoding())
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(answered.GetContent(), "\n", ""))
	if err != nil {
		return nil, errors.Wrapf(err, "%s of %s on %s did not decode", path, on, ref)
	}
	return content, nil
}

func (r gitHubReader) KindAt(ctx context.Context, as forge.Acting, repo forge.Repo, ref, path string) (forge.EntryKind, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return 0, err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return 0, err
	}
	answered, err := r.service().GetRepositoryContent(ctx, &protocol.GitHubGetRepositoryContentRequest{
		Namespace: namespace, Owner: on.Owner, Repo: on.Name, Path: path, Ref: ref,
	})
	if err != nil {
		return 0, errors.Wrapf(err, "%s of %s on %s was not asked", path, on, ref)
	}
	if !answered.GetSuccess() {
		return 0, forge.Refused{Says: answered.GetError()}
	}
	switch answered.GetAnswered() {
	case protocol.GitHubReposContentsAnswer_GITHUB_REPOS_CONTENTS_ANSWER_DIRECTORY:
		return forge.Dir, nil
	case protocol.GitHubReposContentsAnswer_GITHUB_REPOS_CONTENTS_ANSWER_ONE:
		if answered.GetType() == "file" {
			return forge.File, nil
		}
		return forge.Other, nil
	case protocol.GitHubReposContentsAnswer_GITHUB_REPOS_CONTENTS_ANSWER_UNNAMED:
		return 0, errors.Newf("%s of %s on %s is answered by GitHubService without saying what GitHub answered with", path, on, ref)
	}
	return 0, errors.Newf("%s of %s on %s is answered by GitHubService as %d, which this node does not know", path, on, ref, answered.GetAnswered())
}

func (r gitHubReader) Entries(ctx context.Context, as forge.Acting, repo forge.Repo, ref, dir string) ([]forge.Entry, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return nil, err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return nil, err
	}
	answered, err := r.service().GetRepositoryContent(ctx, &protocol.GitHubGetRepositoryContentRequest{
		Namespace: namespace, Owner: on.Owner, Repo: on.Name, Path: dir, Ref: ref,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "%s of %s on %s was not asked", dir, on, ref)
	}
	if !answered.GetSuccess() {
		return nil, forge.Refused{Says: answered.GetError()}
	}
	switch answered.GetAnswered() {
	case protocol.GitHubReposContentsAnswer_GITHUB_REPOS_CONTENTS_ANSWER_DIRECTORY:
		entries := make([]forge.Entry, 0, len(answered.GetItems()))
		for _, item := range answered.GetItems() {
			entries = append(entries, forge.Entry{Name: item.GetName(), Path: item.GetPath(), SHA: item.GetSha(), Kind: entryKind(item.GetType())})
		}
		return entries, nil
	case protocol.GitHubReposContentsAnswer_GITHUB_REPOS_CONTENTS_ANSWER_ONE:
		return nil, forge.NotADirectory{Kind: answered.GetType()}
	case protocol.GitHubReposContentsAnswer_GITHUB_REPOS_CONTENTS_ANSWER_UNNAMED:
		return nil, errors.Newf("%s of %s on %s is answered by GitHubService without saying what GitHub answered with", dir, on, ref)
	}
	return nil, errors.Newf("%s of %s on %s is answered by GitHubService as %d, which this node does not know", dir, on, ref, answered.GetAnswered())
}

// entryKind is what GitHub calls a thing in a directory, as an EntryKind.
func entryKind(github string) forge.EntryKind {
	if github == "file" {
		return forge.File
	}
	if github == "dir" {
		return forge.Dir
	}
	return forge.Other
}

func (r gitHubReader) Readme(ctx context.Context, as forge.Acting, repo forge.Repo, ref, dir string) (forge.Readme, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return forge.Readme{}, err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return forge.Readme{}, err
	}
	var readme *protocol.GitHubGetARepositoryREADMEResponse
	if dir == forge.Root {
		readme, err = r.service().GetARepositoryREADME(ctx, &protocol.GitHubGetARepositoryREADMERequest{
			Namespace: namespace, Owner: on.Owner, Repo: on.Name, Ref: ref})
		if err != nil {
			return forge.Readme{}, errors.Wrapf(err, "GitHubService did not answer for the README of %s at %s", on, ref)
		}
	} else {
		readme, err = r.service().GetARepositoryREADMEForADirectory(ctx, &protocol.GitHubGetARepositoryREADMEForADirectoryRequest{
			Namespace: namespace, Owner: on.Owner, Repo: on.Name, Dir: dir, Ref: ref})
		if err != nil {
			return forge.Readme{}, errors.Wrapf(err, "GitHubService did not answer for the README of %s in %s at %s", dir, on, ref)
		}
	}
	if !readme.GetSuccess() {
		return forge.Readme{}, forge.Refused{Says: readme.GetError()}
	}
	if readme.GetEncoding() != "base64" {
		return forge.Readme{}, errors.Newf("the README at %s is encoded as %q, and base64 is what is read", readme.GetPath(), readme.GetEncoding())
	}
	text, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(readme.GetContent(), "\n", ""))
	if err != nil {
		return forge.Readme{}, errors.Wrapf(err, "the README at %s does not decode", readme.GetPath())
	}
	return forge.Readme{Path: readme.GetPath(), Text: string(text)}, nil
}
