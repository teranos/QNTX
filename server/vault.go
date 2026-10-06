package server

// An Obsidian vault the node keeps a copy of, and the folders of repositories it
// holds (ADR-049), kept where the node keeps what it knows about itself. Each
// change is a new line, and the newest line about a vault holds.

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/errors"
)

// A VAULT line's predicate is the vault's name.
const vaultSubject = "VAULT"

const vaultPath = "/api/vault"

// Vault is one vault as the node keeps it: where its copy is on the box, and
// each folder it holds as owner/repo@branch:path=place, place being in the vault.
type Vault struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Folders []string `json:"folders"`
}

// "So, we could do the Same for Obsidian"

// "name both ends"

// vaultFolders reads folders separated by spaces, each a build's input, = and
// its place in the vault. One that does not read refuses them all.
func vaultFolders(said string) ([]string, error) {
	folders := []string{}
	var places []string
	for _, field := range strings.Fields(said) {
		repo, place, ok := strings.Cut(field, "=")
		if !ok || place == "" {
			return nil, errors.Newf("%q names no place in the vault: owner/repo@branch:path=place", field)
		}
		source, err := parseBuildSource(repo, true)
		if err != nil {
			return nil, err
		}
		if filepath.IsAbs(place) || !filepath.IsLocal(place) {
			return nil, errors.Newf("%q is no place inside the vault", place)
		}
		place = filepath.ToSlash(filepath.Clean(place))
		// A note is one folder's, so no place is another's or inside it.
		for _, other := range places {
			if place == other || strings.HasPrefix(place, other+"/") || strings.HasPrefix(other, place+"/") {
				return nil, errors.Newf("%s and %s are one place in the vault, or one holds the other", other, place)
			}
		}
		places = append(places, place)
		folders = append(folders, source.String()+"="+place)
	}
	return folders, nil
}

// Vaults is every vault the node keeps, by name.
func (r NodeRecords) Vaults() ([]Vault, error) {
	newest, err := newestLines(r.s, vaultSubject, "vaults")
	if err != nil {
		return nil, err
	}
	vaults := make([]Vault, 0, len(newest))
	for name, as := range newest {
		vault := Vault{Name: name, Folders: []string{}}
		if path, ok := as.Attributes["path"].(string); ok {
			vault.Path = path
		}
		if held, ok := as.Attributes["folders"].([]any); ok {
			for _, folder := range held {
				text, ok := folder.(string)
				if !ok {
					return nil, errors.Newf("%s line %s about %s: a folder is %v, not text", vaultSubject, as.ID, name, folder)
				}
				vault.Folders = append(vault.Folders, text)
			}
		}
		vaults = append(vaults, vault)
	}
	slices.SortFunc(vaults, func(a, b Vault) int { return strings.Compare(a.Name, b.Name) })
	return vaults, nil
}

// SetVault writes a vault whole.
func (r NodeRecords) SetVault(actor string, vault Vault) error {
	folders := make([]any, len(vault.Folders))
	for i, folder := range vault.Folders {
		folders[i] = folder
	}
	return r.nodeRecord(actor, vaultSubject, vault.Name, "_", map[string]any{
		"path":    vault.Path,
		"folders": folders,
	})
}

func (s *QNTXServer) vaultSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "vault",
			Sigils: []*protocol.Sigil{
				{
					Name:  "list",
					Does:  "Every Obsidian vault the node keeps a copy of: where the copy is on the box, and the folders of repositories it holds.",
					Gives: []*protocol.Field{{Name: "vaults", Says: "One per vault: its name, its path on the box, and its folders as owner/repo@branch:path=place."}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: vaultPath},
				},
				{
					Name: "set",
					Does: "Say a vault whole: where its copy is on the box, and the folders of repositories it holds, each with where in the vault it is. Refused when a folder names no repository, branch, path or place, when a place leaves the vault, and when two places are one or one holds the other.",
					Takes: []*protocol.Param{
						{Name: "name", Required: true, Says: "The vault's name, as Obsidian Sync names it."},
						{Name: "path", Required: true, Says: "Where the vault's copy is on the box: an absolute path."},
						{Name: "folders", Says: "Each folder the vault holds as owner/repo@branch:path=place, the place being where in the vault it is, separated by spaces. None when not sent."},
					},
					Gives: []*protocol.Field{{Name: "vaults", Says: "Every vault the node keeps now."}},
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: vaultPath},
				},
			},
		},
		Answers: map[string]sigil.Answer{"list": s.vaultList, "set": s.vaultSet},
	}
}

func (s *QNTXServer) vaultList(context.Context, sigil.Sent) (any, *protocol.Refusal) {
	vaults, err := s.nodeRecords().Vaults()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return map[string]any{"vaults": vaults}, nil
}

func (s *QNTXServer) vaultSet(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	name := strings.TrimSpace(sent["name"])
	path := strings.TrimSpace(sent["path"])
	if !filepath.IsAbs(path) {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "path", Says: "a vault's path is absolute, and this said " + path}
	}
	folders, err := vaultFolders(sent["folders"])
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "folders", Says: err.Error()}
	}
	if err := s.nodeRecords().SetVault(actorOf(ctx), Vault{Name: name, Path: filepath.Clean(path), Folders: folders}); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return s.vaultList(ctx, sent)
}
