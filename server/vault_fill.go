package server

// "6. And QNTX ensures changes flow back into the Obsidian Vault as well"

// "my word should supersede always no exception"

// Main reaching the vault (ADR-049): main's change to a note is written into
// the vault's copy on the box, where the vault did not change it.

// Obsidian Sync takes it from there to every device. What the vault changed
// stays as the vault has it (vault_merge.go).

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // git names a blob by its SHA-1, used only to compare with GitHub's
	"encoding/base64"
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	errors "github.com/teranos/sacred-error"
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

// repoNotes is every note under folder of repo on branch, its folders'
// own included.
func (s *QNTXServer) repoNotes(ctx context.Context, repo gitHubRepo, branch, folder string) ([]repoNote, error) {
	var notes []repoNote
	var walk func(dir string) error
	walk = func(dir string) error {
		answered, err := s.gitHubService().GetRepositoryContent(services.AsInstallation(ctx),
			&protocol.GitHubGetRepositoryContentRequest{Owner: repo.Owner, Repo: repo.Name, Path: dir, Ref: branch})
		if err != nil {
			return errors.Wrapf(err, "%s of %s on %s was not asked", dir, repo, branch)
		}
		if !answered.GetSuccess() {
			return errors.New(answered.GetError())
		}
		if answered.GetType() == "file" {
			return errors.Newf("%s is a file of %s on %s, not a folder", dir, repo, branch)
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
func (s *QNTXServer) repoNoteContent(ctx context.Context, repo gitHubRepo, branch, file string) ([]byte, error) {
	answered, err := s.gitHubService().GetRepositoryContent(services.AsInstallation(ctx),
		&protocol.GitHubGetRepositoryContentRequest{Owner: repo.Owner, Repo: repo.Name, Path: file, Ref: branch})
	if err != nil {
		return nil, errors.Wrapf(err, "%s of %s on %s was not asked", file, repo, branch)
	}
	if !answered.GetSuccess() {
		return nil, errors.New(answered.GetError())
	}
	if answered.GetEncoding() != "base64" {
		return nil, errors.Newf("GitHub gave %s of %s on %s encoded as %q, which the node does not read; a file over 1 MB comes with none",
			file, repo, branch, answered.GetEncoding())
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(answered.GetContent(), "\n", ""))
	if err != nil {
		return nil, errors.Wrapf(err, "%s of %s on %s did not decode", file, repo, branch)
	}
	return content, nil
}

// vaultFilled is what filling one folder did: each note written as main has
// it, merged, kept as the vault has it, or removed, and how many were the same.
type vaultFilled struct {
	Folder  string
	Wrote   []string
	Merged  []string
	Kept    []string
	Removed []string
	Same    int
	Refused error
}

// readIfThere is a file's content, and whether there is one.
func readIfThere(at string) ([]byte, bool, error) {
	content, err := os.ReadFile(at)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errors.Wrapf(err, "%s did not read", at)
	}
	return content, true, nil
}

// writeNote writes a note, and the folders it is in.
func writeNote(at string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return errors.Wrapf(err, "could not create %s", filepath.Dir(at))
	}
	return errors.Wrapf(os.WriteFile(at, content, mode), "could not write %s", at)
}

// fillVaultFolder brings main's changes to the notes of folder into the vault's
// copy, against what main had when they last agreed (the base).
func (s *QNTXServer) fillVaultFolder(ctx context.Context, vault Vault, folder string) vaultFilled {
	filled := vaultFilled{Folder: folder, Wrote: []string{}, Merged: []string{}, Kept: []string{}, Removed: []string{}}
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
	on, err := source.onGitHub()
	if err != nil {
		filled.Refused = err
		return filled
	}
	notes, err := s.repoNotes(ctx, on, source.Branch, source.Path)
	if err != nil {
		filled.Refused = err
		return filled
	}
	root := filepath.Join(vault.Path, filepath.FromSlash(place))
	bases, err := vaultBases(vault.Name, place)
	if err != nil {
		filled.Refused = err
		return filled
	}
	now := map[string]bool{}
	for _, note := range notes {
		now[note.under] = true
		at := filepath.Join(root, filepath.FromSlash(path.Clean(note.under)))
		baseAt := filepath.Join(bases, filepath.FromSlash(path.Clean(note.under)))
		if !strings.HasPrefix(at, root+string(filepath.Separator)) || !strings.HasPrefix(baseAt, bases+string(filepath.Separator)) {
			filled.Refused = errors.Newf("%s of %s would be written outside %s, at %s", note.path, repo, root, at)
			return filled
		}
		placed := path.Join(place, note.under)
		held, inVault, err := readIfThere(at)
		if err != nil {
			filled.Refused = err
			return filled
		}
		base, hasBase, err := readIfThere(baseAt)
		if err != nil {
			filled.Refused = err
			return filled
		}
		if inVault && gitBlobSHA(held) == note.sha {
			filled.Same++
			if !hasBase || !bytes.Equal(base, held) {
				if err := writeNote(baseAt, held, 0o600); err != nil {
					filled.Refused = err
					return filled
				}
			}
			continue
		}
		// Taken out of the vault after they agreed: it stays out.
		if !inVault && hasBase {
			continue
		}
		content, err := s.repoNoteContent(ctx, on, source.Branch, note.path)
		if err != nil {
			filled.Refused = err
			return filled
		}
		write, writes := content, false
		switch {
		case !inVault, bytes.Equal(held, base) && hasBase:
			writes = true
			filled.Wrote = append(filled.Wrote, placed)
		case !hasBase, bytes.Equal(content, base):
			// Nothing says main changed it, or main did not: the vault's word.
			filled.Kept = append(filled.Kept, placed)
		case inVault && hasBase:
			// Both changed it since they agreed.
			write = mergeVaultWins(base, held, content)
			if bytes.Equal(write, held) {
				filled.Kept = append(filled.Kept, placed)
			} else {
				writes = true
				filled.Merged = append(filled.Merged, placed)
			}
		}
		if writes {
			if err := writeNote(at, write, 0o644); err != nil { //nolint:gosec // a note is read by Obsidian Sync, which runs as another process
				filled.Refused = err
				return filled
			}
		}
		if err := writeNote(baseAt, content, 0o600); err != nil {
			filled.Refused = err
			return filled
		}
	}

	// "yes"
	// A note main no longer has is gone from the vault too, unless the vault changed it.
	gone, err := basesNotIn(bases, now)
	if err != nil {
		filled.Refused = err
		return filled
	}
	for _, under := range gone {
		at := filepath.Join(root, filepath.FromSlash(path.Clean(under)))
		baseAt := filepath.Join(bases, filepath.FromSlash(path.Clean(under)))
		if !strings.HasPrefix(at, root+string(filepath.Separator)) {
			filled.Refused = errors.Newf("%s, which main no longer has, would be removed outside %s, at %s", under, root, at)
			return filled
		}
		held, inVault, err := readIfThere(at)
		if err != nil {
			filled.Refused = err
			return filled
		}
		base, hadBase, err := readIfThere(baseAt)
		if err != nil {
			filled.Refused = err
			return filled
		}
		switch {
		case inVault && hadBase && bytes.Equal(held, base):
			if err := os.Remove(at); err != nil && !os.IsNotExist(err) {
				filled.Refused = errors.Wrapf(err, "could not remove %s, which main no longer has", at)
				return filled
			}
			filled.Removed = append(filled.Removed, path.Join(place, under))
		case inVault:
			filled.Kept = append(filled.Kept, path.Join(place, under))
		}
		if err := os.Remove(baseAt); err != nil && !os.IsNotExist(err) {
			filled.Refused = errors.Wrapf(err, "could not remove the base of %s", under)
			return filled
		}
	}
	return filled
}

// vaultsSeenDir is where the node keeps, per vault and folder, each note as
// main had it when the vault and main last agreed on it: the base a merge reads.
var vaultsSeenDir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to resolve the home directory the vaults' notes are kept under")
	}
	return filepath.Join(home, ".qntx", "vaults"), nil
}

// vaultBases is where the bases of one place of a vault are kept.
func vaultBases(name, place string) (string, error) {
	dir, err := vaultsSeenDir()
	if err != nil {
		return "", err
	}
	kept := filepath.Join(dir, name)
	at := filepath.Join(kept, filepath.FromSlash(path.Clean(place)))
	if !strings.HasPrefix(at, kept+string(filepath.Separator)) {
		return "", errors.Newf("the bases of %s would be kept outside %s, at %s", place, kept, at)
	}
	return at, nil
}

// basesNotIn is every note a base is kept for that main does not have now.
func basesNotIn(bases string, now map[string]bool) ([]string, error) {
	var gone []string
	err := filepath.WalkDir(bases, func(at string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return filepath.SkipAll
		}
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), noteSuffix) {
			return nil
		}
		under, err := filepath.Rel(bases, at)
		if err != nil {
			return err
		}
		if under = filepath.ToSlash(under); !now[under] {
			gone = append(gone, under)
		}
		return nil
	})
	return gone, errors.Wrapf(err, "the bases in %s did not read", bases)
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
			logger.Errorw("A vault's folder was not filled from its repository", "vault", vault.Name, "folder", folder, "wrote", filled.Wrote, "merged", filled.Merged, "kept", filled.Kept, "removed", filled.Removed, "error", filled.Refused)
		} else {
			logger.Infow("A vault's folder was filled from its repository", "vault", vault.Name, "folder", folder, "wrote", filled.Wrote, "merged", filled.Merged, "kept", filled.Kept, "removed", filled.Removed, "same", filled.Same)
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
func movedBy(push forge.Push) folderMoved {
	return func(source buildSource, _ string) bool {
		return source.Repo.Is(push.Repo) && source.Branch == push.Branch() && push.TouchedUnder(source.Path)
	}
}

// vaultsMovedBy fills each enabled folder a push changed a note of, and names
// them as vault:place.
func (s *QNTXServer) vaultsMovedBy(push forge.Push) []string {
	vaults, err := s.nodeRecords().Vaults()
	if err != nil {
		s.logger.Named("vault").Errorw("A push arrived and the vaults were not read", "repo", push.Repo.String(), "error", err)
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
