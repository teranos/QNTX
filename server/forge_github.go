package server

// GitHub as a forge.Forge: the one place the core's reading and writing of a
// repository, and its asking after checks, is put in GitHub's words. What
// GitHub has and another forge may not (the App's installations and the
// repositories they reach, workflow-run artifacts) is not here, and is asked of
// GitHubService where it is needed, by name.

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

// forges are the forges the node reaches.
func (s *QNTXServer) forges() forge.Forges {
	return forge.ForgesOf(map[string]forge.Forge{forge.GitHub: gitHubForge{service: s.gitHubService}})
}

// gitHubForge is github.com, through GitHubService.
type gitHubForge struct {
	service func() *services.GitHubServer
}

var _ forge.Forge = gitHubForge{}

// who is the context and the namespace GitHubService is asked with, as the
// node is, or as the App's installation where the repository is.
func (r gitHubForge) who(ctx context.Context, as forge.Acting) (context.Context, string, error) {
	switch as {
	case forge.AsNode:
		return ctx, auth.NamespaceSystem, nil
	case forge.AsApp:
		return services.AsInstallation(ctx), "", nil
	}
	return ctx, "", errors.Newf("GitHub is not read %s", as)
}

func (r gitHubForge) Describe(ctx context.Context, as forge.Acting, repo forge.Repo) (forge.Repository, error) {
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

func (r gitHubForge) Head(ctx context.Context, as forge.Acting, repo forge.Repo, ref, path string) (string, error) {
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

func (r gitHubForge) Archive(ctx context.Context, as forge.Acting, repo forge.Repo, rev string) (io.ReadCloser, error) {
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

func (r gitHubForge) File(ctx context.Context, as forge.Acting, repo forge.Repo, ref, path string) ([]byte, error) {
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

func (r gitHubForge) KindAt(ctx context.Context, as forge.Acting, repo forge.Repo, ref, path string) (forge.EntryKind, error) {
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

func (r gitHubForge) Entries(ctx context.Context, as forge.Acting, repo forge.Repo, ref, dir string) ([]forge.Entry, error) {
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

func (r gitHubForge) Readme(ctx context.Context, as forge.Acting, repo forge.Repo, ref, dir string) (forge.Readme, error) {
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

func (r gitHubForge) Branch(ctx context.Context, as forge.Acting, repo forge.Repo, branch string) (string, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return "", err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return "", err
	}
	got, err := r.service().GetABranch(ctx, &protocol.GitHubGetABranchRequest{Namespace: namespace, Owner: on.Owner, Repo: on.Name, Branch: branch})
	if err != nil {
		return "", errors.Wrapf(err, "%s of %s was not asked", branch, on)
	}
	if !got.GetSuccess() {
		return "", forge.Refused{Says: got.GetError()}
	}
	return got.GetCommit().GetSha(), nil
}

func (r gitHubForge) Compare(ctx context.Context, as forge.Acting, repo forge.Repo, base, head string) (forge.Comparison, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return forge.Comparison{}, err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return forge.Comparison{}, err
	}
	compared, err := r.service().CompareTwoCommits(ctx, &protocol.GitHubCompareTwoCommitsRequest{
		Namespace: namespace, Owner: on.Owner, Repo: on.Name, Basehead: base + "..." + head})
	if err != nil {
		return forge.Comparison{}, errors.Wrapf(err, "%s and %s of %s were not asked to be compared", base, head, on)
	}
	if !compared.GetSuccess() {
		return forge.Comparison{}, forge.Refused{Says: compared.GetError()}
	}
	return forge.Comparison{Files: len(compared.GetFiles())}, nil
}

func (r gitHubForge) ChangeFrom(ctx context.Context, as forge.Acting, repo forge.Repo, head, base string) (forge.Change, bool, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return forge.Change{}, false, err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return forge.Change{}, false, err
	}
	open, err := r.service().ListPullRequests(ctx, &protocol.GitHubListPullRequestsRequest{
		Namespace: namespace, Owner: on.Owner, Repo: on.Name, State: "open", Head: on.Owner + ":" + head, Base: base})
	if err != nil {
		return forge.Change{}, false, errors.Wrapf(err, "the open pull requests from %s of %s were not asked", head, on)
	}
	if !open.GetSuccess() {
		return forge.Change{}, false, forge.Refused{Says: open.GetError()}
	}
	for _, pull := range open.GetItems() {
		return forge.Change{Number: pull.GetNumber(), URL: pull.GetHtmlUrl()}, true, nil
	}
	return forge.Change{}, false, nil
}

func (r gitHubForge) CreateBranch(ctx context.Context, as forge.Acting, repo forge.Repo, branch, from string) error {
	on, err := onGitHub(repo)
	if err != nil {
		return err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return err
	}
	made, err := r.service().CreateAReference(ctx, &protocol.GitHubCreateAReferenceRequest{
		Namespace: namespace, Owner: on.Owner, Repo: on.Name, Ref: "refs/heads/" + branch, Sha: from})
	if err != nil {
		return errors.Wrapf(err, "%s was not asked to be made on %s", branch, on)
	}
	if !made.GetSuccess() {
		return forge.Refused{Says: made.GetError()}
	}
	return nil
}

// putFile commits content to path on branch, over the blob replaces when one is named.
func (r gitHubForge) putFile(ctx context.Context, as forge.Acting, repo forge.Repo, branch, path string, content []byte, put func(*protocol.GitHubCreateOrUpdateFileContentsRequest), message string) error {
	on, err := onGitHub(repo)
	if err != nil {
		return err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return err
	}
	request := &protocol.GitHubCreateOrUpdateFileContentsRequest{
		Namespace: namespace, Owner: on.Owner, Repo: on.Name, Path: path, Branch: branch,
		Message: message, Content: base64.StdEncoding.EncodeToString(content),
	}
	put(request)
	answered, err := r.service().CreateOrUpdateFileContents(ctx, request)
	if err != nil {
		return errors.Wrapf(err, "%s was not asked to be committed to %s of %s", path, branch, on)
	}
	if !answered.GetSuccess() {
		return forge.Refused{Says: answered.GetError()}
	}
	return nil
}

func (r gitHubForge) AddFile(ctx context.Context, as forge.Acting, repo forge.Repo, branch, path string, content []byte, message string) error {
	return r.putFile(ctx, as, repo, branch, path, content, func(*protocol.GitHubCreateOrUpdateFileContentsRequest) {}, message)
}

func (r gitHubForge) ReplaceFile(ctx context.Context, as forge.Acting, repo forge.Repo, branch, path string, content []byte, replaces, message string) error {
	return r.putFile(ctx, as, repo, branch, path, content, func(request *protocol.GitHubCreateOrUpdateFileContentsRequest) { request.Sha = replaces }, message)
}

func (r gitHubForge) DeleteFile(ctx context.Context, as forge.Acting, repo forge.Repo, branch, path, sha, message string) error {
	on, err := onGitHub(repo)
	if err != nil {
		return err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return err
	}
	answered, err := r.service().DeleteAFile(ctx, &protocol.GitHubDeleteAFileRequest{
		Namespace: namespace, Owner: on.Owner, Repo: on.Name, Path: path, Branch: branch, Sha: sha, Message: message,
	})
	if err != nil {
		return errors.Wrapf(err, "%s was not asked to be removed from %s of %s", path, branch, on)
	}
	if !answered.GetSuccess() {
		return forge.Refused{Says: answered.GetError()}
	}
	return nil
}

func (r gitHubForge) NewChange(ctx context.Context, as forge.Acting, repo forge.Repo, head, base, title, body string) (forge.Change, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return forge.Change{}, err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return forge.Change{}, err
	}
	made, err := r.service().CreateAPullRequest(ctx, &protocol.GitHubCreateAPullRequestRequest{
		Namespace: namespace, Owner: on.Owner, Repo: on.Name, Head: head, Base: base, Title: title, Body: body,
	})
	if err != nil {
		return forge.Change{}, errors.Wrapf(err, "a pull request from %s of %s was not asked for", head, on)
	}
	if !made.GetSuccess() {
		return forge.Change{}, forge.Refused{Says: made.GetError()}
	}
	return forge.Change{Number: made.GetNumber(), URL: made.GetHtmlUrl()}, nil
}

func (r gitHubForge) MergeChange(ctx context.Context, as forge.Acting, repo forge.Repo, number int64, sha string) (string, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return "", err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return "", err
	}
	said, err := r.service().MergeAPullRequest(ctx, &protocol.GitHubMergeAPullRequestRequest{
		Namespace: namespace, Owner: on.Owner, Repo: on.Name, PullNumber: number, Sha: sha, MergeMethod: "merge",
	})
	if err != nil {
		return "", errors.Wrapf(err, "GitHub was not asked to merge %s#%d", on, number)
	}
	if !said.GetSuccess() {
		return "", forge.Refused{Says: said.GetError()}
	}
	return said.GetSha(), nil
}

// Checks is what GitHub says of a commit's checks. OwnCI asks of Actions, by
// its App, its suites and its runs; EveryCheck asks of every run.
func (r gitHubForge) Checks(ctx context.Context, as forge.Acting, repo forge.Repo, ref string, scope forge.CheckScope) (forge.Checks, error) {
	on, err := onGitHub(repo)
	if err != nil {
		return forge.Checks{}, err
	}
	ctx, namespace, err := r.who(ctx, as)
	if err != nil {
		return forge.Checks{}, err
	}
	var actions int64
	switch scope {
	case forge.OwnCI:
		actions = gitHubActionsApp
	case forge.EveryCheck:
	default:
		return forge.Checks{}, errors.Newf("the checks of %s@%s are asked of scope %d, which GitHub does not know", on, ref, scope)
	}

	said := forge.Checks{States: map[string]forge.CheckState{}, Settled: true}
	if scope == forge.OwnCI {
		suites, err := r.service().ListCheckSuitesForAGitReference(ctx, &protocol.GitHubListCheckSuitesForAGitReferenceRequest{
			Namespace: namespace, Owner: on.Owner, Repo: on.Name, Ref: ref, AppId: actions, PerPage: 100,
		})
		if err != nil {
			return forge.Checks{}, errors.Wrapf(err, "GitHub was not asked about the check suites on %s@%s", repo, ref)
		}
		if !suites.GetSuccess() {
			return forge.Checks{}, errors.Newf("the check suites on %s@%s were not listed: %s", repo, ref, suites.GetError())
		}
		for _, suite := range suites.GetCheckSuites() {
			said.Ran = true
			if suite.GetStatus() != "completed" {
				said.Settled = false
				continue
			}
			if !concludedWell(suite.GetConclusion()) {
				said.Failed = append(said.Failed, "a suite concluded "+suite.GetConclusion())
			}
		}
	}
	runs, err := r.service().ListCheckRunsForAGitReference(ctx, &protocol.GitHubListCheckRunsForAGitReferenceRequest{
		Namespace: namespace, Owner: on.Owner, Repo: on.Name, Ref: ref, AppId: actions, PerPage: 100,
	})
	if err != nil {
		return forge.Checks{}, errors.Wrapf(err, "GitHub was not asked about the check runs on %s@%s", repo, ref)
	}
	if !runs.GetSuccess() {
		return forge.Checks{}, errors.Newf("the check runs on %s@%s were not listed: %s", repo, ref, runs.GetError())
	}
	// A check run again is a newer run under the same name; the newest stands.
	newest := map[string]int64{}
	for _, run := range runs.GetCheckRuns() {
		if seen, held := newest[run.GetName()]; held && seen > run.GetId() {
			continue
		}
		newest[run.GetName()] = run.GetId()
		said.States[run.GetName()] = forge.CheckState(checkState(run.GetStatus(), run.GetConclusion()))
	}
	for name, state := range said.States {
		if state == forge.CheckFailed {
			said.Failed = append(said.Failed, name)
		}
	}
	return said, nil
}
