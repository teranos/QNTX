package server

// "6. And QNTX ensures changes flow back into the Obsidian Vault as well"

// "main wins"

// Main reaching the vault (ADR-049): each note of a bound folder on its
// repository's branch is written into the vault's copy on the box.

// Obsidian Sync takes it from there to every device. A note that differs is
// main's, and what is only in the vault is left as it is.

import (
	"context"
	"crypto/sha1" //nolint:gosec // git names a blob by its SHA-1, used only to compare with GitHub's
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/errors"
)

// vaultFilling holds one filling of the vault at a time, so two never write
// one note at once.
var vaultFilling sync.Mutex

// goFill is where a filling runs: its own goroutine, which a test runs in its own.
var goFill = sacred.Go

// noteSuffix is what a note of a repository's folder ends with.
const noteSuffix = ".md"

// repoNote is one note of a repository's folder, by its path under the folder.
type repoNote struct {
	under string
	path  string
	sha   string
}

// gitBlobSHA is what git names content by, as GitHub gives a file's sha.
func gitBlobSHA(content []byte) string {
	h := sha1.New() //nolint:gosec // see the import
	h.Write([]byte("blob " + strconv.Itoa(len(content)) + "\x00"))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// repoNotes is every note under folder of owner/repo on branch, its folders'
// own included.
func (s *QNTXServer) repoNotes(ctx context.Context, owner, repo, branch, folder string) ([]repoNote, error) {
	var notes []repoNote
	var walk func(dir string) error
	walk = func(dir string) error {
		answered, err := s.gitHubService().GetRepositoryContent(services.AsInstallation(ctx),
			&protocol.GitHubGetRepositoryContentRequest{Owner: owner, Repo: repo, Path: dir, Ref: branch})
		if err != nil {
			return errors.Wrapf(err, "%s of %s/%s on %s was not asked", dir, owner, repo, branch)
		}
		if !answered.GetSuccess() {
			return errors.New(answered.GetError())
		}
		if answered.GetType() == "file" {
			return errors.Newf("%s is a file of %s/%s on %s, not a folder", dir, owner, repo, branch)
		}
		for _, entry := range answered.GetItems() {
			switch {
			case entry.GetType() == "dir":
				if err := walk(entry.GetPath()); err != nil {
					return err
				}
			case entry.GetType() == "file" && strings.HasSuffix(entry.GetName(), noteSuffix):
				under := strings.TrimPrefix(entry.GetPath(), folder+"/")
				notes = append(notes, repoNote{under: under, path: entry.GetPath(), sha: entry.GetSha()})
			}
		}
		return nil
	}
	if err := walk(folder); err != nil {
		return nil, err
	}
	return notes, nil
}

// repoNoteContent is a note's content on branch, as GitHub gives it.
func (s *QNTXServer) repoNoteContent(ctx context.Context, owner, repo, branch, file string) ([]byte, error) {
	answered, err := s.gitHubService().GetRepositoryContent(services.AsInstallation(ctx),
		&protocol.GitHubGetRepositoryContentRequest{Owner: owner, Repo: repo, Path: file, Ref: branch})
	if err != nil {
		return nil, errors.Wrapf(err, "%s of %s/%s on %s was not asked", file, owner, repo, branch)
	}
	if !answered.GetSuccess() {
		return nil, errors.New(answered.GetError())
	}
	if answered.GetEncoding() != "base64" {
		return nil, errors.Newf("GitHub gave %s of %s/%s on %s encoded as %q, which the node does not read; a file over 1 MB comes with none",
			file, owner, repo, branch, answered.GetEncoding())
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(answered.GetContent(), "\n", ""))
	if err != nil {
		return nil, errors.Wrapf(err, "%s of %s/%s on %s did not decode", file, owner, repo, branch)
	}
	return content, nil
}

// vaultFilled is what filling one folder did.
type vaultFilled struct {
	Folder  string
	Wrote   []string
	Removed []string
	Same    int
	Refused error
}

// fillVaultFolder writes each note of folder's repository folder that the
// vault's copy does not hold as the branch has it.
func (s *QNTXServer) fillVaultFolder(ctx context.Context, vault Vault, folder string) vaultFilled {
	filled := vaultFilled{Folder: folder, Wrote: []string{}, Removed: []string{}}
	repo, place, named := strings.Cut(folder, "=")
	if !named {
		filled.Refused = errors.Newf("%s names no place in the vault", folder)
		return filled
	}
	source, err := parseBuildSource(repo, true)
	if err != nil {
		filled.Refused = err
		return filled
	}
	notes, err := s.repoNotes(ctx, source.Owner, source.Repo, source.Branch, source.Path)
	if err != nil {
		filled.Refused = err
		return filled
	}
	root := filepath.Join(vault.Path, filepath.FromSlash(place))
	for _, note := range notes {
		at := filepath.Join(root, filepath.FromSlash(path.Clean(note.under)))
		if !strings.HasPrefix(at, root+string(filepath.Separator)) {
			filled.Refused = errors.Newf("%s of %s would be written outside %s, at %s", note.path, repo, root, at)
			return filled
		}
		held, err := os.ReadFile(at)
		if err != nil && !os.IsNotExist(err) {
			filled.Refused = errors.Wrapf(err, "%s in the vault did not read", at)
			return filled
		}
		if err == nil && gitBlobSHA(held) == note.sha {
			filled.Same++
			continue
		}
		content, err := s.repoNoteContent(ctx, source.Owner, source.Repo, source.Branch, note.path)
		if err != nil {
			filled.Refused = err
			return filled
		}
		if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
			filled.Refused = errors.Wrapf(err, "could not create %s", filepath.Dir(at))
			return filled
		}
		if err := os.WriteFile(at, content, 0o644); err != nil { //nolint:gosec // a note is read by Obsidian Sync, which runs as another process
			filled.Refused = errors.Wrapf(err, "could not write %s", at)
			return filled
		}
		filled.Wrote = append(filled.Wrote, path.Join(place, note.under))
	}

	// "yes"
	// A note main had when this folder was last filled, and has no more, is
	// gone from the vault too. One main never had is the vault's own.
	seen, err := mainSaw(vault.Name)
	if err != nil {
		filled.Refused = err
		return filled
	}
	now := make([]string, 0, len(notes))
	for _, note := range notes {
		now = append(now, note.under)
	}
	for _, under := range seen[folder] {
		if slices.Contains(now, under) {
			continue
		}
		at := filepath.Join(root, filepath.FromSlash(path.Clean(under)))
		if !strings.HasPrefix(at, root+string(filepath.Separator)) {
			filled.Refused = errors.Newf("%s, which main no longer has, would be removed outside %s, at %s", under, root, at)
			return filled
		}
		if err := os.Remove(at); err != nil && !os.IsNotExist(err) {
			filled.Refused = errors.Wrapf(err, "could not remove %s, which main no longer has", at)
			return filled
		}
		filled.Removed = append(filled.Removed, path.Join(place, under))
	}
	seen[folder] = now
	if err := keepMainSaw(vault.Name, seen); err != nil {
		filled.Refused = err
	}
	return filled
}

// vaultsSeenDir is where the node keeps, per vault, the notes main had when
// each folder was last filled.
var vaultsSeenDir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to resolve the home directory the vaults' notes are kept under")
	}
	return filepath.Join(home, ".qntx", "vaults"), nil
}

// mainSaw is, per folder of the vault, the notes main had when it was last filled.
func mainSaw(name string) (map[string][]string, error) {
	dir, err := vaultsSeenDir()
	if err != nil {
		return nil, err
	}
	at := filepath.Join(dir, name+".json")
	held, err := os.ReadFile(at)
	if os.IsNotExist(err) {
		return map[string][]string{}, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "what main had for %s did not read from %s", name, at)
	}
	seen := map[string][]string{}
	if err := json.Unmarshal(held, &seen); err != nil {
		return nil, errors.Wrapf(err, "what main had for %s, in %s, is not JSON the node wrote", name, at)
	}
	return seen, nil
}

// keepMainSaw writes down what main had, per folder of the vault.
func keepMainSaw(name string, seen map[string][]string) error {
	dir, err := vaultsSeenDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.Wrapf(err, "could not create %s", dir)
	}
	written, err := json.MarshalIndent(seen, "", "  ")
	if err != nil {
		return errors.Wrapf(err, "what main had for %s did not marshal", name)
	}
	at := filepath.Join(dir, name+".json")
	return errors.Wrapf(os.WriteFile(at, written, 0o600), "could not write %s", at)
}

// folderMoved says whether a bound folder is to be filled now.
type folderMoved func(source buildSource, place string) bool

// fillVault fills each enabled folder of the vault named that moved says to.
func (s *QNTXServer) fillVault(ctx context.Context, name string, moved folderMoved) []vaultFilled {
	vaultFilling.Lock()
	defer vaultFilling.Unlock()
	logger := s.logger.Named("vault")
	vault, refused := s.vaultNamed(name)
	if refused != nil {
		logger.Errorw("A vault was to be filled and the node does not hold it", "vault", name, "refused", refused.GetSays())
		return nil
	}
	var done []vaultFilled
	for _, folder := range vault.Folders {
		repo, place, named := strings.Cut(folder, "=")
		if named && slices.Contains(vault.Disabled, place) {
			continue
		}
		if source, err := parseBuildSource(repo, true); named && err == nil && !moved(source, place) {
			continue
		}
		filled := s.fillVaultFolder(ctx, vault, folder)
		if filled.Refused != nil {
			logger.Errorw("A vault's folder was not filled from its repository", "vault", vault.Name, "folder", folder, "wrote", filled.Wrote, "removed", filled.Removed, "error", filled.Refused)
		} else {
			logger.Infow("A vault's folder was filled from its repository", "vault", vault.Name, "folder", folder, "wrote", filled.Wrote, "removed", filled.Removed, "same", filled.Same)
		}
		done = append(done, filled)
	}
	return done
}

// everyFolder fills a folder whatever moved.
func everyFolder(buildSource, string) bool { return true }

// FillVaults fills every enabled folder of every vault: a push while the node
// was down reached nobody.
func (s *QNTXServer) FillVaults() {
	vaults, err := s.nodeRecords().Vaults()
	if err != nil {
		s.logger.Named("vault").Errorw("The vaults were not read, so none is filled", "error", err)
		return
	}
	for _, vault := range vaults {
		goFill("vault.fill."+vault.Name, func() { s.fillVault(s.lifetime(), vault.Name, everyFolder) })
	}
}

// fillPlaceSoon fills the one folder bound at place, once it is bound or enabled.
func (s *QNTXServer) fillPlaceSoon(name, place string) {
	goFill("vault.fill."+name, func() {
		s.fillVault(s.lifetime(), name, func(_ buildSource, at string) bool { return at == place })
	})
}

// movedBy is whether push changed a note under source's folder on its branch.
func movedBy(push gitHubPush) folderMoved {
	return func(source buildSource, _ string) bool {
		return strings.EqualFold(source.Owner+"/"+source.Repo, push.Repository.FullName) &&
			source.Branch == push.branch() && push.touchedUnder(source.Path)
	}
}

// vaultsMovedBy fills each enabled folder a push changed a note of, and names
// them as vault:place.
func (s *QNTXServer) vaultsMovedBy(push gitHubPush) []string {
	vaults, err := s.nodeRecords().Vaults()
	if err != nil {
		s.logger.Named("vault").Errorw("A push arrived and the vaults were not read", "repo", push.Repository.FullName, "error", err)
		return []string{}
	}
	moved := movedBy(push)
	named := []string{}
	for _, vault := range vaults {
		fills := false
		for _, folder := range vault.Folders {
			repo, place, ok := strings.Cut(folder, "=")
			source, err := parseBuildSource(repo, true)
			if ok && err == nil && !slices.Contains(vault.Disabled, place) && moved(source, place) {
				named = append(named, vault.Name+":"+place)
				fills = true
			}
		}
		if fills {
			goFill("vault.fill."+vault.Name, func() { s.fillVault(s.lifetime(), vault.Name, moved) })
		}
	}
	return named
}
