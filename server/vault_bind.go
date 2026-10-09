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
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

// "concept of default branch, not main or master"

// repoDefaultBranch is the branch owner/repo names its default, as GitHub says it now.
func (s *QNTXServer) repoDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	answered, err := s.gitHubService().GetARepository(services.AsInstallation(ctx),
		&protocol.GitHubGetARepositoryRequest{Owner: owner, Repo: repo})
	if err != nil {
		return "", errors.Wrapf(err, "the default branch of %s/%s was not asked", owner, repo)
	}
	if !answered.GetSuccess() {
		return "", errors.New(answered.GetError())
	}
	if answered.GetDefaultBranch() == "" {
		return "", errors.Newf("GitHub named no default branch for %s/%s", owner, repo)
	}
	return answered.GetDefaultBranch(), nil
}

// ownerRepo reads owner/repo.
func ownerRepo(said string) (string, string, *protocol.Refusal) {
	owner, repo, named := repoNamed(said)
	if !named {
		return "", "", &protocol.Refusal{Why: sigil.Invalid, Param: "repo", Says: said + " names no repository: owner/repo"}
	}
	return owner, repo, nil
}

// repoNamed is owner and repo of owner/repo, and whether said is one.
func repoNamed(said string) (string, string, bool) {
	owner, repo, ok := strings.Cut(said, "/")
	return owner, repo, ok && owner != "" && repo != "" && !strings.Contains(repo, "/")
}

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
	err := filepath.WalkDir(root, func(at string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if at == root || !d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		place, err := filepath.Rel(root, at)
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

// vaultHasDir says whether place is a folder of the vault's copy. Only its not
// being there is no; a copy that could not be read is the error, as it was.
func vaultHasDir(vault Vault, place string) (bool, error) {
	at := filepath.Join(vault.Path, filepath.FromSlash(place))
	info, err := os.Stat(at)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrapf(err, "whether %s is a folder of %s was not read", at, vault.Name)
	}
	return info.IsDir(), nil
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
	return &protocol.VaultDirs{Dirs: dirs}, nil
}

func (s *QNTXServer) vaultOwners(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	owners := []*protocol.VaultOwner{}
	for page := int64(1); ; page++ {
		answered, err := s.gitHubService().ListInstallationsForTheAuthenticatedApp(ctx,
			&protocol.GitHubListInstallationsForTheAuthenticatedAppRequest{PerPage: githubPage, Page: page})
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "where the App is installed was not asked: " + err.Error()}
		}
		if !answered.GetSuccess() {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "where the App is installed was not learned: " + answered.GetError()}
		}
		for _, installation := range answered.GetItems() {
			account := installation.GetAccount().GetFields()
			owners = append(owners, &protocol.VaultOwner{
				Login:        account["login"].GetStringValue(),
				Type:         account["type"].GetStringValue(),
				Installation: installation.GetId(),
			})
		}
		if len(answered.GetItems()) < githubPage {
			break
		}
	}
	slices.SortFunc(owners, func(a, b *protocol.VaultOwner) int { return strings.Compare(a.GetLogin(), b.GetLogin()) })
	return &protocol.VaultOwners{Owners: owners}, nil
}

func (s *QNTXServer) vaultRepos(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	id, err := strconv.ParseInt(sent["installation"], 10, 64)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "installation", Says: sent["installation"] + " is no installation's id"}
	}
	repos := []string{}
	for page := int64(1); ; page++ {
		answered, err := s.gitHubService().ListRepositoriesAccessibleToTheAppInstallation(services.AsInstallationOf(ctx, id),
			&protocol.GitHubListRepositoriesAccessibleToTheAppInstallationRequest{PerPage: githubPage, Page: page})
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "what installation " + sent["installation"] + " reaches was not asked: " + err.Error()}
		}
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
	return &protocol.VaultRepos{Repos: repos}, nil
}

// repoDirs is the folders directly inside dir of owner/repo on branch, or why
// they are not: GitHub's own words, or dir being a file.
func (s *QNTXServer) repoDirs(ctx context.Context, owner, repo, branch, dir string) ([]string, error) {
	if dir == "" {
		// GitHub lists a repository's top at ".".
		dir = "."
	}
	answered, err := s.gitHubService().GetRepositoryContent(services.AsInstallation(ctx),
		&protocol.GitHubGetRepositoryContentRequest{Owner: owner, Repo: repo, Path: dir, Ref: branch})
	if err != nil {
		return nil, errors.Wrapf(err, "%s of %s/%s on %s was not asked", dir, owner, repo, branch)
	}
	if !answered.GetSuccess() {
		return nil, errors.New(answered.GetError())
	}
	if answered.GetType() == "file" {
		return nil, errors.Newf("%s is a file of %s/%s on %s, not a folder", dir, owner, repo, branch)
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
	owner, repo, refused := ownerRepo(sent["repo"])
	if refused != nil {
		return nil, refused
	}
	branch, err := s.repoDefaultBranch(ctx, owner, repo)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	dirs, err := s.repoDirs(ctx, owner, repo, branch, sent["path"])
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return &protocol.VaultSubdirs{Dirs: dirs, Branch: branch}, nil
}

func (s *QNTXServer) vaultBind(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	vault, refused := s.vaultNamed(sent["name"])
	if refused != nil {
		return nil, refused
	}
	place := sent["place"]
	there, err := vaultHasDir(vault, place)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if !there {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "place", Says: place + " is no folder of " + vault.Name + " at " + vault.Path}
	}
	owner, repo, refused := ownerRepo(sent["repo"])
	if refused != nil {
		return nil, refused
	}
	// "oh, require it to be a dir in the repo , not in its root"
	if dir := path.Clean(sent["path"]); dir == "." || dir == "/" || strings.HasPrefix(dir, "../") || strings.HasPrefix(dir, "/") {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "path", Says: sent["path"] + " is not a folder inside " + sent["repo"] + "; a vault's folder is bound to one, never to the repository's top"}
	}
	branch, err := s.repoDefaultBranch(ctx, owner, repo)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	folder := sent["repo"] + "@" + branch + ":" + sent["path"] + "=" + place
	folders, err := vaultFoldersOf(append(slices.Clone(vault.Folders), folder))
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Says: err.Error()}
	}
	vault.Folders = folders
	if err := s.nodeRecords().SetVault(actorOf(ctx), vault); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	s.fillPlaceSoon(vault.Name, filepath.ToSlash(filepath.Clean(place)))
	return s.vaultList(ctx, sent)
}

// "and if expanded, there should be a two stage button to allow me to unbind as well."
func (s *QNTXServer) vaultUnbind(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	vault, refused := s.vaultNamed(sent["name"])
	if refused != nil {
		return nil, refused
	}
	place := sent["place"]
	kept := []string{}
	for _, folder := range vault.Folders {
		if _, at, _ := strings.Cut(folder, "="); at != place {
			kept = append(kept, folder)
		}
	}
	if len(kept) == len(vault.Folders) {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "place", Says: place + " of " + vault.Name + " is bound to no folder of a repository"}
	}
	vault.Folders = kept
	vault.Disabled = slices.DeleteFunc(vault.Disabled, func(p string) bool { return p == place })
	delete(vault.Sends, place)
	if err := s.nodeRecords().SetVault(actorOf(ctx), vault); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return s.vaultList(ctx, sent)
}

// "and another button to simply disable it, but the bind is still there, it just doesnt do anything"
func (s *QNTXServer) vaultDisable(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	return s.vaultSwitch(ctx, sent, true)
}

func (s *QNTXServer) vaultEnable(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	return s.vaultSwitch(ctx, sent, false)
}

// vaultSwitch disables or enables the folder bound at a place, which stays bound either way.
func (s *QNTXServer) vaultSwitch(ctx context.Context, sent sigil.Sent, disable bool) (any, *protocol.Refusal) {
	vault, refused := s.vaultNamed(sent["name"])
	if refused != nil {
		return nil, refused
	}
	place := sent["place"]
	if !slices.ContainsFunc(vault.Folders, func(f string) bool { _, at, _ := strings.Cut(f, "="); return at == place }) {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "place", Says: place + " of " + vault.Name + " is bound to no folder of a repository"}
	}
	vault.Disabled = slices.DeleteFunc(vault.Disabled, func(p string) bool { return p == place })
	if disable {
		vault.Disabled = append(vault.Disabled, place)
	}
	if err := s.nodeRecords().SetVault(actorOf(ctx), vault); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if !disable {
		s.fillPlaceSoon(vault.Name, place)
	}
	return s.vaultList(ctx, sent)
}

// "what should a valid binding show? that its active, green dot,"
const (
	vaultActive   = "active"
	vaultDisabled = "disabled"
	vaultInvalid  = "invalid"
	// A folder that sends: its branch holds what the default branch does not, or nothing more.
	vaultChanges   = "changes"
	vaultUnchanged = "unchanged"
)

// "it should show red, and have you redo the binding"

// foldersState is what an enabled folder whose place is on the box is now.
func (s *QNTXServer) foldersState(ctx context.Context, vault Vault, source buildSource, place string, state *protocol.VaultFolderState) {
	defaultBranch, err := s.repoDefaultBranch(ctx, source.Owner, source.Repo)
	if err != nil {
		state.State, state.Why = vaultInvalid, err.Error()
		return
	}
	if defaultBranch != source.Branch {
		state.State, state.Why = vaultInvalid, "the default branch of "+source.Owner+"/"+source.Repo+" is now "+defaultBranch+", not "+source.Branch+": bind "+place+" again"
		return
	}
	if _, err := s.repoDirs(ctx, source.Owner, source.Repo, source.Branch, source.Path); err != nil {
		state.State, state.Why = vaultInvalid, err.Error()
		return
	}
	branch, sends := vault.Sends[place]
	if !sends {
		return
	}
	state.Branch = branch
	// A send that failed is what the folder is, until one does not.
	if refused := lastRefused(vault.Name, state.Folder, branch); refused != "" {
		state.State, state.Why = vaultInvalid, refused
		return
	}
	sendsNow, pull, err := s.sendState(ctx, source, branch)
	if err != nil {
		state.State, state.Why = vaultInvalid, err.Error()
		return
	}
	state.State, state.Pull = sendsNow, pull
}

func (s *QNTXServer) vaultStates(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	vault, refused := s.vaultNamed(sent["name"])
	if refused != nil {
		return nil, refused
	}
	states := []*protocol.VaultFolderState{}
	for _, folder := range vault.Folders {
		repo, place, named := strings.Cut(folder, "=")
		state := &protocol.VaultFolderState{Folder: folder, Place: place, State: vaultActive}
		if !named {
			state.State, state.Why = vaultInvalid, folder+" names no place in the vault: owner/repo@branch:path=place"
			states = append(states, state)
			continue
		}
		source, err := parseBuildSource(repo, true)
		if err != nil {
			state.State, state.Why = vaultInvalid, err.Error()
			states = append(states, state)
			continue
		}
		there, err := vaultHasDir(vault, place)
		switch {
		case err != nil:
			state.State, state.Why = vaultInvalid, err.Error()
		case !there:
			state.State, state.Why = vaultInvalid, place+" is no folder of "+vault.Name+" at "+vault.Path
		case slices.Contains(vault.Disabled, place):
			// Disabled does nothing, so nothing is asked of GitHub for it.
			state.State = vaultDisabled
		default:
			s.foldersState(ctx, vault, source, place, state)
		}
		states = append(states, state)
	}
	return &protocol.VaultStates{Folders: states}, nil
}
