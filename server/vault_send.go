package server

// "5. And the persistent branch is obsidian-[nameofvault] and it always
// contains the abcdsync diff, and people may merge it at times"

// "i dont want that to be automatically opted in,"

// "and i want to set what the name of the branch would be in the obsidian element in the binding."

// The vault reaching its branch (ADR-049): a folder that sends commits what
// the vault holds differently from its branch there, and keeps a pull request
// from that branch into the default branch open for somebody to merge.

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/forge"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// vaultLooksEvery is how often the node reads a sending folder for changes.
const vaultLooksEvery = 20 * time.Second

// vaultRetryAfter is how long a send that failed waits before it is tried
// again with nothing changed in the vault.
const vaultRetryAfter = 5 * time.Minute

// branchRefused is why a name is no branch to send to, or empty.
func branchRefused(name, defaultBranch string) string {
	switch {
	case name == defaultBranch:
		return name + " is the default branch, which changes reach the vault from; a folder sends to a branch of its own"
	case strings.HasPrefix(name, "-"), strings.HasPrefix(name, "/"), strings.HasSuffix(name, "/"),
		strings.HasSuffix(name, ".lock"), strings.HasSuffix(name, "."), strings.Contains(name, ".."),
		strings.Contains(name, "//"), strings.Contains(name, "@{"), name == "@":
		return name + " is no name git takes for a branch"
	}
	for _, r := range name {
		if r <= ' ' || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return name + " is no name git takes for a branch: it holds " + strings.TrimSpace(string(r))
		}
	}
	return ""
}

// "and i want to set what the name of the branch would be in the obsidian element in the binding."
func (s *QNTXServer) vaultSend(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	vault, refused := s.vaultNamed(sent["name"])
	if refused != nil {
		return nil, refused
	}
	place, branch := sent["place"], strings.TrimSpace(sent["branch"])
	var source buildSource
	held := false
	for _, folder := range vault.Folders {
		repo, at, _ := strings.Cut(folder, "=")
		if at != place {
			continue
		}
		read, err := parseBuildSource(repo, true)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "place", Says: err.Error()}
		}
		source, held = read, true
	}
	if !held {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "place", Says: place + " of " + vault.Name + " is bound to no folder of a repository"}
	}
	if branch == "" {
		delete(vault.Sends, place)
	} else {
		if why := branchRefused(branch, source.Branch); why != "" {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "branch", Says: why}
		}
		vault.Sends[place] = branch
	}
	if err := s.nodeRecords().SetVault(actorOf(ctx), vault); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if branch != "" {
		s.sendPlaceSoon(vault.Name, place)
	}
	return s.vaultList(ctx, sent)
}

// "i DONT want the binding to include subdirs"

// vaultNotesAt is every note directly in root, by its name, and its content.
// A folder inside root is not the binding's, nor is a note in one.
func vaultNotesAt(root string) (map[string][]byte, error) {
	notes := map[string][]byte{}
	// A place that is a link is not a folder of the vault: what it points at
	// is somewhere else's, and none of it is sent.
	placed, err := os.Lstat(root)
	if err != nil {
		return notes, errors.Wrapf(err, "the notes in %s were not read", root)
	}
	if placed.Mode()&os.ModeSymlink != 0 {
		return notes, errors.Newf("%s is a link, not a folder of the vault, so nothing in it is sent", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return notes, errors.Wrapf(err, "the notes in %s were not read", root)
	}
	for _, entry := range entries {
		// A note is a file of the vault's own: a link to one is not.
		if !entry.Type().IsRegular() || strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(entry.Name(), noteSuffix) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			return notes, errors.Wrapf(err, "the notes in %s were not read", root)
		}
		notes[entry.Name()] = content
	}
	return notes, nil
}

// vaultSent is what sending one folder did.
type vaultSent struct {
	Folder    string
	Branch    string
	Committed []string
	Removed   []string
	Pull      string
	Refused   error
}

// proposalOf is what a folder that sends proposes: its branch, into the
// default branch the folder is bound to.
func proposalOf(source buildSource, branch string) forge.Proposal {
	return forge.Proposal{Repo: source.Repo, Branch: branch, Into: source.Branch}
}

// branchHeld makes sure branch is on the source's repository, made from the
// default branch when it is not yet.
func (s *QNTXServer) branchHeld(ctx context.Context, source buildSource, branch string) error {
	host, err := s.forges().For(source.Repo)
	if err != nil {
		return err
	}
	return proposalOf(source, branch).HoldBranch(ctx, host, forge.AsApp)
}

// sendVaultFolder commits each note the vault holds differently from branch,
// and removes from branch each note the vault no longer has.
func (s *QNTXServer) sendVaultFolder(ctx context.Context, vault Vault, folder, branch string, notes map[string][]byte) vaultSent {
	sent := vaultSent{Folder: folder, Branch: branch, Committed: []string{}, Removed: []string{}}
	repo, place, _ := strings.Cut(folder, "=")
	source, err := parseBuildSource(repo, true)
	if err != nil {
		sent.Refused = err
		return sent
	}
	host, err := s.forges().For(source.Repo)
	if err != nil {
		sent.Refused = err
		return sent
	}
	defaultBranch, err := s.repoDefaultBranch(ctx, source.Repo)
	if err != nil {
		sent.Refused = err
		return sent
	}
	if defaultBranch != source.Branch {
		sent.Refused = errors.Newf("the default branch of %s is now %s, not %s: bind %s again", source.Repo, defaultBranch, source.Branch, place)
		return sent
	}
	if err := s.branchHeld(ctx, source, branch); err != nil {
		sent.Refused = err
		return sent
	}
	onBranch, err := s.repoNotes(ctx, source.Repo, branch, source.Path)
	if err != nil {
		sent.Refused = err
		return sent
	}
	held := map[string]repoNote{}
	for _, note := range onBranch {
		held[note.under] = note
	}
	unders := make([]string, 0, len(notes))
	for under := range notes {
		unders = append(unders, under)
	}
	slices.Sort(unders)
	edits := make([]forge.Edit, 0, len(unders)+len(onBranch))
	for _, under := range unders {
		content := notes[under]
		was, there := held[under]
		if there && was.sha == gitBlobSHA(content) {
			continue
		}
		file := path.Join(source.Path, under)
		message := vault.Name + ": " + path.Join(place, under)
		if there {
			edits = append(edits, forge.Replaced(file, content, was.sha, message))
		} else {
			edits = append(edits, forge.Added(file, content, message))
		}
	}
	for _, note := range onBranch {
		if _, kept := notes[note.under]; kept {
			continue
		}
		edits = append(edits, forge.Removed(note.path, note.sha, vault.Name+": "+path.Join(place, note.under)+" is gone from the vault"))
	}
	done, err := proposalOf(source, branch).Commit(ctx, host, forge.AsApp, edits)
	sent.Committed, sent.Removed = done.Put, done.Removed
	if err != nil {
		sent.Refused = err
		return sent
	}
	pull, err := s.pullHeld(ctx, source, branch, vault.Name, place, len(sent.Committed)+len(sent.Removed) > 0)
	sent.Pull, sent.Refused = pull, err
	return sent
}

// pullHeld is the open pull request from branch into the default branch,
// opened when the vault just sent something and none is open.
func (s *QNTXServer) pullHeld(ctx context.Context, source buildSource, branch, vault, place string, sentSomething bool) (string, error) {
	host, err := s.forges().For(source.Repo)
	if err != nil {
		return "", err
	}
	proposal := proposalOf(source, branch)
	if !sentSomething {
		open, _, err := proposal.OpenChange(ctx, host, forge.AsApp)
		return open.URL, err
	}
	held, err := proposal.Propose(ctx, host, forge.AsApp,
		"Obsidian "+vault+": "+place,
		"What the vault "+vault+" holds in "+place+", sent to "+branch+" (QNTX ADR-049). It stays open, and is merged when somebody merges it.")
	return held.URL, err
}

// vaultLooked is what a sending folder held when it was last sent, and when
// a send of it last failed.
type vaultLooked struct {
	notes  map[string]string
	failed time.Time
	// refused is why the last send failed, said whole wherever the folder's state is.
	refused string
}

// lastRefused is why the last send of folder to branch failed, or empty.
func lastRefused(vault, folder, branch string) string {
	vaultLookedMu.Lock()
	defer vaultLookedMu.Unlock()
	return vaultLookedAt[vault+"\x00"+folder+"\x00"+branch].refused
}

var (
	vaultLookedMu sync.Mutex
	vaultLookedAt = map[string]vaultLooked{}
)

// shasOf is each note's git name.
func shasOf(notes map[string][]byte) map[string]string {
	out := make(map[string]string, len(notes))
	for under, content := range notes {
		out[under] = gitBlobSHA(content)
	}
	return out
}

// sendVault sends each folder of the vault that sends, is enabled, and that
// changed since it was last sent; only says which to send now.
func (s *QNTXServer) sendVault(ctx context.Context, name string, only func(place string) bool) []vaultSent {
	vaultFilling.Lock()
	defer vaultFilling.Unlock()
	logger := s.logger.Named("vault")
	vault, refused := s.vaultNamed(name)
	if refused != nil {
		logger.Errorw("A vault was to be sent and the node does not hold it", "vault", name, "refused", refused.GetSays())
		return nil
	}
	var done []vaultSent
	for _, folder := range vault.Folders {
		_, place, named := strings.Cut(folder, "=")
		branch, sends := vault.Sends[place]
		if !named || !sends || slices.Contains(vault.Disabled, place) || !only(place) {
			continue
		}
		root := filepath.Join(vault.Path, filepath.FromSlash(place))
		notes, err := vaultNotesAt(root)
		if err != nil {
			logger.Errorw("A vault's folder was not read to be sent", "vault", vault.Name, "folder", folder, "error", err)
			continue
		}
		key := vault.Name + "\x00" + folder + "\x00" + branch
		now := shasOf(notes)
		vaultLookedMu.Lock()
		last, looked := vaultLookedAt[key]
		vaultLookedMu.Unlock()
		unchanged := looked && mapsEqual(last.notes, now)
		if unchanged && (last.failed.IsZero() || time.Since(last.failed) < vaultRetryAfter) {
			continue
		}
		sent := s.sendVaultFolder(ctx, vault, folder, branch, notes)
		record := vaultLooked{notes: now}
		if sent.Refused != nil {
			record.failed, record.refused = time.Now(), sent.Refused.Error()
			logger.Errorw("A vault's folder was not sent to its branch", "vault", vault.Name, "folder", folder, "branch", branch,
				"committed", sent.Committed, "removed", sent.Removed, "error", sent.Refused)
		} else {
			logger.Infow("A vault's folder was sent to its branch", "vault", vault.Name, "folder", folder, "branch", branch,
				"committed", sent.Committed, "removed", sent.Removed, "pull", sent.Pull)
		}
		vaultLookedMu.Lock()
		vaultLookedAt[key] = record
		vaultLookedMu.Unlock()
		done = append(done, sent)
	}
	return done
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, held := b[k]; !held || w != v {
			return false
		}
	}
	return true
}

func everyPlace(string) bool { return true }

// sendPlaceSoon sends the one folder at place, once it is set to send.
func (s *QNTXServer) sendPlaceSoon(name, place string) {
	goFill("vault.send."+name, func() {
		s.sendVault(s.lifetime(), name, func(at string) bool { return at == place })
	})
}

// SendVaults reads every sending folder of every vault for changes, and sends
// what changed, for as long as the node runs.
func (s *QNTXServer) SendVaults() {
	sacred.Go("vault.send", func() {
		ticker := time.NewTicker(vaultLooksEvery)
		defer ticker.Stop()
		for {
			select {
			case <-s.lifetime().Done():
				return
			case <-ticker.C:
			}
			vaults, err := s.nodeRecords().Vaults()
			if err != nil {
				s.logger.Named("vault").Errorw("The vaults were not read, so none is sent", "error", err)
				continue
			}
			for _, vault := range vaults {
				if len(vault.Sends) > 0 {
					s.sendVault(s.lifetime(), vault.Name, everyPlace)
				}
			}
		}
	})
}

// sendState is what a folder that sends is now: its branch, whether that
// branch holds anything the default branch does not, and its open pull request.
func (s *QNTXServer) sendState(ctx context.Context, source buildSource, branch string) (state, pull string, err error) {
	host, err := s.forges().For(source.Repo)
	if err != nil {
		return "", "", err
	}
	_, err = host.Branch(ctx, forge.AsApp, source.Repo, branch)
	var absent forge.Refused
	if errors.As(err, &absent) {
		// Nothing was sent yet, so the branch is still to be made.
		return vaultUnchanged, "", nil
	}
	if err != nil {
		return "", "", err
	}
	compared, err := host.Compare(ctx, forge.AsApp, source.Repo, source.Branch, branch)
	var refused forge.Refused
	if errors.As(err, &refused) {
		return "", "", errors.Newf("%s and %s of %s were not compared: %s", source.Branch, branch, source.Repo, refused.Says)
	}
	if err != nil {
		return "", "", err
	}
	pull, err = s.pullHeld(ctx, source, branch, "", "", false)
	if err != nil {
		return "", "", err
	}
	if compared.Files > 0 {
		return vaultChanges, pull, nil
	}
	return vaultUnchanged, pull, nil
}
