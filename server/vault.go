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

// 1.0.0 BLOCKER (#1091): a vault is set up on the box by hand, outside the node.

// The node has to sign in to Sync, list the vaults and keep one syncing itself.

// IMPLEMENTME: Phase 7 (ADR-049), notes known to QNTX as attestations. A note is
// a file in the vault's copy and nothing more; the node attests none of it.

// A VAULT line's predicate is the vault's name.
const vaultSubject = "VAULT"

const vaultPath = "/api/vault"

// Vault is one vault as the node keeps it: where its copy is on the box, and
// each folder it holds as owner/repo@branch:path=place, place being in the vault.
type Vault struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Folders []string `json:"folders"`
	// Disabled is each place whose folder is still bound and does nothing.
	Disabled []string `json:"disabled"`
	// Sends is, by place, the branch the vault's changes there are committed
	// to. A place not in it sends nothing: it is opted into, never assumed.
	Sends map[string]string `json:"sends"`
}

// texts is a line's attribute that is a list of text.
func texts(as map[string]any, attribute, subject, id, name string) ([]string, error) {
	out := []string{}
	held, ok := as[attribute].([]any)
	if !ok {
		return out, nil
	}
	for _, item := range held {
		text, ok := item.(string)
		if !ok {
			return nil, errors.Newf("%s line %s about %s: one of %s is %v, not text", subject, id, name, attribute, item)
		}
		out = append(out, text)
	}
	return out, nil
}

func anyOf(items []string) []any {
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = item
	}
	return out
}

// "So, we could do the Same for Obsidian"

// "name both ends"

// vaultFolders reads folders separated by spaces, each a build's input, = and
// its place in the vault. One that does not read refuses them all.
func vaultFolders(said string) ([]string, error) {
	return vaultFoldersOf(strings.Fields(said))
}

// vaultFoldersOf reads folders one by one, so a place may hold a space, as a
// vault's folders do.
func vaultFoldersOf(fields []string) ([]string, error) {
	folders := []string{}
	var places []string
	for _, field := range fields {
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
		vault := Vault{Name: name}
		if path, ok := as.Attributes["path"].(string); ok {
			vault.Path = path
		}
		if vault.Folders, err = texts(as.Attributes, "folders", vaultSubject, as.ID, name); err != nil {
			return nil, err
		}
		if vault.Disabled, err = texts(as.Attributes, "disabled", vaultSubject, as.ID, name); err != nil {
			return nil, err
		}
		vault.Sends = map[string]string{}
		if sends, ok := as.Attributes["sends"].(map[string]any); ok {
			for place, branch := range sends {
				text, ok := branch.(string)
				if !ok {
					return nil, errors.Newf("%s line %s about %s: the branch %s sends to is %v, not text", vaultSubject, as.ID, name, place, branch)
				}
				vault.Sends[place] = text
			}
		}
		vaults = append(vaults, vault)
	}
	slices.SortFunc(vaults, func(a, b Vault) int { return strings.Compare(a.Name, b.Name) })
	return vaults, nil
}

// SetVault writes a vault whole.
func (r NodeRecords) SetVault(actor string, vault Vault) error {
	return r.nodeRecord(actor, vaultSubject, vault.Name, "_", map[string]any{
		"path":     vault.Path,
		"folders":  anyOf(vault.Folders),
		"disabled": anyOf(vault.Disabled),
		"sends":    sendsOf(vault.Sends),
	})
}

func sendsOf(sends map[string]string) map[string]any {
	out := make(map[string]any, len(sends))
	for place, branch := range sends {
		out[place] = branch
	}
	return out
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
				{
					Name:  "dirs",
					Does:  "Every folder of a vault's copy on the box, by its place in the vault. Obsidian's own folders, the ones starting with a dot, are left out.",
					Takes: []*protocol.Param{{Name: "name", Required: true, Says: "The vault's name, as Obsidian Sync names it."}},
					Gives: []*protocol.Field{{Name: "dirs", Says: "Each folder's place in the vault, folders apart by /."}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: vaultPath + "/dirs"},
				},
				{
					Name:  "owners",
					Does:  "Each GitHub user and organization the node's GitHub App is installed on: where a vault's folder can be bound.",
					Gives: []*protocol.Field{{Name: "owners", Says: "One per installation: its login, whether it is a User or an Organization, and the installation's id."}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: vaultPath + "/owners"},
				},
				{
					Name:  "repos",
					Does:  "Each repository one installation of the GitHub App reaches, as the installation itself is answered.",
					Takes: []*protocol.Param{{Name: "installation", Required: true, Kind: sigil.Count, Says: "The installation's id, as owners gives it."}},
					Gives: []*protocol.Field{{Name: "repos", Says: "Each repository as owner/repo."}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: vaultPath + "/repos"},
				},
				{
					Name: "subdirs",
					Does: "The folders directly inside one folder of a repository, on its default branch, asked as the App's installation where the repository is.",
					Takes: []*protocol.Param{
						{Name: "repo", Required: true, Says: "The repository, as owner/repo."},
						{Name: "path", Says: "The folder, from the repository's top. Its top when not sent."},
					},
					Gives: []*protocol.Field{
						{Name: "dirs", Says: "Each folder's path from the repository's top."},
						{Name: "branch", Says: "The repository's default branch, which the folders are read on and a folder is bound to."},
					},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: vaultPath + "/subdirs"},
				},
				{
					Name: "bind",
					Does: "Binds a folder of a vault to a folder of a repository on its default branch, beside the folders it holds. Refused when the vault's folder is not on the box, and when it is another bound folder's, or holds one, or is inside one.",
					Takes: []*protocol.Param{
						{Name: "name", Required: true, Says: "The vault's name, as Obsidian Sync names it."},
						{Name: "place", Required: true, Says: "The vault's folder, by its place in the vault, as dirs gives it."},
						{Name: "repo", Required: true, Says: "The repository, as owner/repo."},
						{Name: "path", Required: true, Says: "The repository's folder, from its top, as subdirs gives it."},
					},
					Gives: []*protocol.Field{{Name: "vaults", Says: "Every vault the node keeps now."}},
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: vaultPath + "/bind"},
				},
				{
					Name: "unbind",
					Does: "Unbinds a folder of a vault: the vault no longer holds the folder of a repository bound there. Nothing in the vault's copy or the repository is touched.",
					Takes: []*protocol.Param{
						{Name: "name", Required: true, Says: "The vault's name, as Obsidian Sync names it."},
						{Name: "place", Required: true, Says: "The vault's folder, by its place in the vault."},
					},
					Gives: []*protocol.Field{{Name: "vaults", Says: "Every vault the node keeps now."}},
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: vaultPath + "/unbind"},
				},
				{
					Name: "disable",
					Does: "Disables a bound folder of a vault: it stays bound and does nothing until it is enabled.",
					Takes: []*protocol.Param{
						{Name: "name", Required: true, Says: "The vault's name, as Obsidian Sync names it."},
						{Name: "place", Required: true, Says: "The vault's folder, by its place in the vault."},
					},
					Gives: []*protocol.Field{{Name: "vaults", Says: "Every vault the node keeps now."}},
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: vaultPath + "/disable"},
				},
				{
					Name: "enable",
					Does: "Enables a disabled bound folder of a vault again.",
					Takes: []*protocol.Param{
						{Name: "name", Required: true, Says: "The vault's name, as Obsidian Sync names it."},
						{Name: "place", Required: true, Says: "The vault's folder, by its place in the vault."},
					},
					Gives: []*protocol.Field{{Name: "vaults", Says: "Every vault the node keeps now."}},
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: vaultPath + "/enable"},
				},
				{
					Name: "send",
					Does: "Sends what a bound folder holds in the vault to a branch of its repository, the branch named, with a pull request into the default branch kept open; or stops sending when no branch is named. A folder sends nothing until it is told to.",
					Takes: []*protocol.Param{
						{Name: "name", Required: true, Says: "The vault's name, as Obsidian Sync names it."},
						{Name: "place", Required: true, Says: "The vault's folder, by its place in the vault."},
						{Name: "branch", Says: "The branch to send to; not the default branch. None stops sending."},
					},
					Gives: []*protocol.Field{{Name: "vaults", Says: "Every vault the node keeps now."}},
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: vaultPath + "/send"},
				},
				{
					Name:  "states",
					Does:  "What each folder a vault holds is now: active, disabled, or invalid and why; one that sends is changes or unchanged. A folder is invalid when its folder in the vault is not on the box, its repository's default branch is no longer the one it was bound to, its repository's folder is not on that branch, or the App cannot reach the repository.",
					Takes: []*protocol.Param{{Name: "name", Required: true, Says: "The vault's name, as Obsidian Sync names it."}},
					Gives: []*protocol.Field{{Name: "folders", Says: "One per folder: the folder as owner/repo@branch:path=place, its place, its state (active, disabled, invalid, changes or unchanged), why when invalid, the branch it sends to, and its open pull request."}},
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: vaultPath + "/states"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"list": s.vaultList, "set": s.vaultSet,
			"dirs": s.vaultDirs, "owners": s.vaultOwners, "repos": s.vaultRepos, "subdirs": s.vaultSubdirs,
			"bind": s.vaultBind, "unbind": s.vaultUnbind, "states": s.vaultStates,
			"disable": s.vaultDisable, "enable": s.vaultEnable, "send": s.vaultSend,
		},
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
	// A place said again keeps being disabled; one no longer said is not.
	disabled := []string{}
	was, refused := s.vaultNamed(name)
	if refused != nil && refused.GetWhy() != sigil.NotFound {
		return nil, refused
	}
	sends := map[string]string{}
	if refused == nil {
		held := func(place string) bool {
			return slices.ContainsFunc(folders, func(f string) bool { return strings.HasSuffix(f, "="+place) })
		}
		for _, place := range was.Disabled {
			if held(place) {
				disabled = append(disabled, place)
			}
		}
		for place, branch := range was.Sends {
			if held(place) {
				sends[place] = branch
			}
		}
	}
	if err := s.nodeRecords().SetVault(actorOf(ctx), Vault{Name: name, Path: filepath.Clean(path), Folders: folders, Disabled: disabled, Sends: sends}); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return s.vaultList(ctx, sent)
}
