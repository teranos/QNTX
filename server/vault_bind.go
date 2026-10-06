package server

// "left click a dir: - Bind to repository md folder - gh user or orgs, click
// one - see repos, click one - see subdirs, click one"

// Binding a folder of the vault to a folder of a repository (ADR-049) asks the
// box for the vault's folders, and GitHub, as the App, for where it is
// installed, what each installation reaches and what a repository holds.

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

// vaultBranch is the branch a vault's folder is bound to.
const vaultBranch = "main"

// githubPage is as many as GitHub gives on one page.
const githubPage = 100

// vaultNamed is the vault the node keeps by that name.
func (s *QNTXServer) vaultNamed(name string) (Vault, *protocol.Refusal) {
	vaults, err := s.nodeRecords().Vaults()
	if err != nil {
		return Vault{}, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	for _, vault := range vaults {
		if vault.Name == name {
			return vault, nil
		}
	}
	return Vault{}, &protocol.Refusal{Why: sigil.NotFound, Param: "name", Says: "the node keeps no vault named " + name}
}

// vaultDirsAt is every folder under root, by its place below it. A folder
// starting with a dot is Obsidian's own (.obsidian, .trash), and so is all in it.
func vaultDirsAt(root string) ([]string, error) {
	dirs := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root || !d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		place, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		dirs = append(dirs, filepath.ToSlash(place))
		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "the vault's copy at %s was not read", root)
	}
	slices.Sort(dirs)
	return dirs, nil
}

// vaultHasDir says whether place is a folder of the vault's copy.
func vaultHasDir(vault Vault, place string) bool {
	info, err := os.Stat(filepath.Join(vault.Path, filepath.FromSlash(place)))
	return err == nil && info.IsDir()
}

func (s *QNTXServer) vaultDirs(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	vault, refused := s.vaultNamed(sent["name"])
	if refused != nil {
		return nil, refused
	}
	dirs, err := vaultDirsAt(vault.Path)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return map[string]any{"dirs": dirs}, nil
}

// vaultOwner is one user or organization the App is installed on.
type vaultOwner struct {
	Login        string `json:"login"`
	Type         string `json:"type"`
	Installation int64  `json:"installation"`
}

func (s *QNTXServer) vaultOwners(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	owners := []vaultOwner{}
	for page := int64(1); ; page++ {
		answered, _ := s.gitHubService().ListInstallationsForTheAuthenticatedApp(ctx,
			&protocol.GitHubListInstallationsForTheAuthenticatedAppRequest{PerPage: githubPage, Page: page})
		if !answered.GetSuccess() {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "where the App is installed was not learned: " + answered.GetError()}
		}
		for _, installation := range answered.GetItems() {
			account := installation.GetAccount().GetFields()
			owners = append(owners, vaultOwner{
				Login:        account["login"].GetStringValue(),
				Type:         account["type"].GetStringValue(),
				Installation: installation.GetId(),
			})
		}
		if len(answered.GetItems()) < githubPage {
			break
		}
	}
	slices.SortFunc(owners, func(a, b vaultOwner) int { return strings.Compare(a.Login, b.Login) })
	return map[string]any{"owners": owners}, nil
}

func (s *QNTXServer) vaultRepos(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	id, err := strconv.ParseInt(sent["installation"], 10, 64)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "installation", Says: sent["installation"] + " is no installation's id"}
	}
	repos := []string{}
	for page := int64(1); ; page++ {
		answered, _ := s.gitHubService().ListRepositoriesAccessibleToTheAppInstallation(services.AsInstallationOf(ctx, id),
			&protocol.GitHubListRepositoriesAccessibleToTheAppInstallationRequest{PerPage: githubPage, Page: page})
		if !answered.GetSuccess() {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "what installation " + sent["installation"] + " reaches was not learned: " + answered.GetError()}
		}
		for _, repo := range answered.GetRepositories() {
			repos = append(repos, repo.GetFields()["full_name"].GetStringValue())
		}
		if len(answered.GetRepositories()) < githubPage {
			break
		}
	}
	slices.Sort(repos)
	return map[string]any{"repos": repos}, nil
}

// repoDirs is the folders directly inside path of owner/repo on branch, or why
// they are not: GitHub's own words, or path being a file.
func (s *QNTXServer) repoDirs(ctx context.Context, owner, repo, branch, path string) ([]string, error) {
	if path == "" {
		// GitHub lists a repository's top at ".".
		path = "."
	}
	answered, _ := s.gitHubService().GetRepositoryContent(services.AsInstallation(ctx),
		&protocol.GitHubGetRepositoryContentRequest{Owner: owner, Repo: repo, Path: path, Ref: branch})
	if !answered.GetSuccess() {
		return nil, errors.New(answered.GetError())
	}
	if answered.GetType() == "file" {
		return nil, errors.Newf("%s is a file of %s/%s on %s, not a folder", path, owner, repo, branch)
	}
	dirs := []string{}
	for _, entry := range answered.GetItems() {
		if entry.GetType() == "dir" {
			dirs = append(dirs, entry.GetPath())
		}
	}
	slices.Sort(dirs)
	return dirs, nil
}

func (s *QNTXServer) vaultSubdirs(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	owner, repo, ok := strings.Cut(sent["repo"], "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "repo", Says: sent["repo"] + " names no repository: owner/repo"}
	}
	dirs, err := s.repoDirs(ctx, owner, repo, vaultBranch, sent["path"])
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return map[string]any{"dirs": dirs}, nil
}

func (s *QNTXServer) vaultBind(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	vault, refused := s.vaultNamed(sent["name"])
	if refused != nil {
		return nil, refused
	}
	place := sent["place"]
	if !vaultHasDir(vault, place) {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "place", Says: place + " is no folder of " + vault.Name + " at " + vault.Path}
	}
	folder := sent["repo"] + "@" + vaultBranch + ":" + sent["path"] + "=" + place
	folders, err := vaultFoldersOf(append(slices.Clone(vault.Folders), folder))
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Says: err.Error()}
	}
	vault.Folders = folders
	if err := s.nodeRecords().SetVault(actorOf(ctx), vault); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return s.vaultList(ctx, sent)
}

// vaultFolderState is what one folder a vault holds is now.
type vaultFolderState struct {
	Folder string `json:"folder"`
	Place  string `json:"place"`
	State  string `json:"state"`
	Why    string `json:"why"`
}

// "what should a valid binding show? that its active, green dot,"
const (
	vaultActive  = "active"
	vaultInvalid = "invalid"
)

func (s *QNTXServer) vaultStates(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	vault, refused := s.vaultNamed(sent["name"])
	if refused != nil {
		return nil, refused
	}
	states := []vaultFolderState{}
	for _, folder := range vault.Folders {
		repo, place, _ := strings.Cut(folder, "=")
		state := vaultFolderState{Folder: folder, Place: place, State: vaultActive}
		source, err := parseBuildSource(repo, true)
		switch {
		case err != nil:
			state.State, state.Why = vaultInvalid, err.Error()
		case !vaultHasDir(vault, place):
			state.State, state.Why = vaultInvalid, place+" is no folder of "+vault.Name+" at "+vault.Path
		default:
			if _, err := s.repoDirs(ctx, source.Owner, source.Repo, source.Branch, source.Path); err != nil {
				state.State, state.Why = vaultInvalid, err.Error()
			}
		}
		states = append(states, state)
	}
	return map[string]any{"folders": states}, nil
}
