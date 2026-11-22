package server

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	errors "github.com/teranos/sacred-error"
)

// "I WANT TO SEE IN OBSIDIAN"
// "THE LIVE STATE OF AGENT MEMORY"
// "AND MY EDIT IN OBSIDIAN"
// "NEEDS TO GO TO QNTX"
// "AND THEN QNTX WRITES IT INTO THE CORRECT MEMORY STORE SPECIFIC PER HARNESS"
// "AND IF A NEW MEMORY GETS WRITTEN"
// "IT GOES TO QNTX"
// "AND IT GOES TO OBSIDIAN"
//
// A harness keeps its agent's memory where that harness keeps it. The node
// holds each memory as it last stood on both sides, and keeps the two in step
// with what it holds: the store of the harness, and the store's folder in the
// vault. What changed in the vault since the node last held it goes to the
// harness; what changed in the harness goes to the vault. Changed on both, the
// vault's stands: it is where the person writes.

// memoryInStep is how often the node brings the two sides in step.
var memoryInStep = 5 * time.Second

// heldMemoryDir is where the node holds an agent's memories as they last stood
// on both sides, under the agent's home, by the harness store they are of.
const heldMemoryDir = "memory-held"

// harnessStore is one store a harness keeps for an agent: where it is on the
// box, and the Memory Store it is in the vault.
type harnessStore struct {
	// dir is the harness's own folder of memories.
	dir string
	// held is where the node holds them as they last stood on both sides.
	held string
	// store is the Memory Store's name, its folder under where memory is kept.
	store string
}

// claudeProjectOf is how Claude Code names the project a directory is: every
// character that is no letter or digit, a dash.
func claudeProjectOf(dir string) string {
	named := []byte(dir)
	for i, c := range named {
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			named[i] = '-'
		}
	}
	return string(named)
}

// claudeStoresOf is every store Claude Code keeps for the agent in home, which
// the node calls called: one per project it has worked in. The store of its
// work directory is the agent's own, by its name; another is named with the
// project it is of.
func claudeStoresOf(home, called string) ([]harnessStore, error) {
	projects := filepath.Join(home, "claude", "projects")
	entries, err := os.ReadDir(projects)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "the projects Claude Code keeps memory for under %s were not read", projects)
	}
	own := claudeProjectOf(filepath.Join(home, "work"))
	stores := []harnessStore{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		store := called
		if entry.Name() != own {
			store = called + " " + strings.Trim(entry.Name(), "-")
		}
		if !aNoteName(store) {
			continue
		}
		stores = append(stores, harnessStore{
			dir:   filepath.Join(projects, entry.Name(), "memory"),
			held:  filepath.Join(home, heldMemoryDir, entry.Name()),
			store: store,
		})
	}
	return stores, nil
}

// notesIn is every note directly in dir, by its file name, with what it says.
// A folder that is not there holds none.
func notesIn(dir string) (map[string][]byte, error) {
	notes := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return notes, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "the memories in %s were not read", dir)
	}
	for _, entry := range entries {
		name, isNote := strings.CutSuffix(entry.Name(), ".md")
		if entry.IsDir() || !isNote || !aNoteName(name) {
			continue
		}
		said, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, errors.Wrapf(err, "the memory %s in %s was not read", name, dir)
		}
		notes[entry.Name()] = said
	}
	return notes, nil
}

// putNote makes the note in dir say said, or removes it when said is nil.
func putNote(dir, note string, said []byte) error {
	at := filepath.Join(dir, note)
	if said == nil {
		if err := os.Remove(at); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return errors.Wrapf(err, "the memory %s was not removed from %s", note, dir)
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.Wrapf(err, "%s was not made to keep the memory %s in", dir, note)
	}
	return errors.Wrapf(os.WriteFile(at, said, 0o644), "the memory %s was not written in %s", note, dir)
}

// inStep brings one store's two sides in step with what the node holds, and
// answers how many memories went to the harness and how many to the vault.
func (h harnessStore) inStep(vault string) (toHarness, toVault int, err error) {
	inHarness, err := notesIn(h.dir)
	if err != nil {
		return 0, 0, err
	}
	inVault, err := notesIn(vault)
	if err != nil {
		return 0, 0, err
	}
	held, err := notesIn(h.held)
	if err != nil {
		return 0, 0, err
	}
	// A vault folder that is gone while the node holds memories of it is the
	// vault's copy not being there, not the person forgetting everything: the
	// folder is written again from what the node holds.
	if _, statErr := os.Stat(vault); errors.Is(statErr, fs.ErrNotExist) && len(held) > 0 {
		inVault = held
		for note, said := range held {
			if err := putNote(vault, note, said); err != nil {
				return 0, 0, err
			}
		}
	}
	names := []string{}
	for _, side := range []map[string][]byte{inHarness, inVault, held} {
		for note := range side {
			if !slices.Contains(names, note) {
				names = append(names, note)
			}
		}
	}
	slices.Sort(names)
	for _, note := range names {
		harness, vaulted, was := inHarness[note], inVault[note], held[note]
		switch {
		// The vault's changed since the node held it: where the person writes.
		case !bytes.Equal(vaulted, was) || (vaulted == nil) != (was == nil):
			if err := putNote(h.dir, note, vaulted); err != nil {
				return toHarness, toVault, err
			}
			if err := putNote(h.held, note, vaulted); err != nil {
				return toHarness, toVault, err
			}
			if !bytes.Equal(harness, vaulted) || (harness == nil) != (vaulted == nil) {
				toHarness++
			}
		// The harness's changed: what the agent wrote, or let go of.
		case !bytes.Equal(harness, was) || (harness == nil) != (was == nil):
			if err := putNote(vault, note, harness); err != nil {
				return toHarness, toVault, err
			}
			if err := putNote(h.held, note, harness); err != nil {
				return toHarness, toVault, err
			}
			toVault++
		}
	}
	return toHarness, toVault, nil
}

// calledInMemory is the name of the Memory Store of the agent in home: the
// name the Node Agent holds, or what the node calls an agent that holds none.
func (s *QNTXServer) calledInMemory(home string) string {
	if s.nodeAgent == nil || home != s.nodeAgent.home {
		// A namespace's agent is kept by its namespace and its model.
		return filepath.Base(filepath.Dir(home)) + " " + filepath.Base(home)
	}
	if names, err := s.agentNames(); err == nil {
		if at := slices.IndexFunc(names, func(kept agentName) bool { return kept.did == s.nodeAgent.did && !kept.let }); at >= 0 {
			return names[at].name
		}
	}
	return "Node Agent"
}

// memoriesInStep brings every store of every agent the node runs in step with
// its folder in the vault, once. A node whose vault backs no memory, or whose
// vault's copy is not on the box, keeps nothing in step, and says why.
func (s *QNTXServer) memoriesInStep() error {
	vault, kept, refused := s.memoryKept()
	if refused != nil {
		return nil
	}
	// The vault's copy not being there is no vault with every memory deleted.
	if info, err := os.Stat(vault.Path); err != nil || !info.IsDir() {
		return errors.Newf("memory is kept in the vault %s, whose copy is not a folder on the box at %s: nothing is brought in step", vault.Name, vault.Path)
	}
	failed := []string{}
	for _, home := range s.underGround.all() {
		stores, err := claudeStoresOf(home, s.calledInMemory(home))
		if err != nil {
			failed = append(failed, err.Error())
			continue
		}
		for _, store := range stores {
			toHarness, toVault, err := store.inStep(filepath.Join(kept, store.store))
			if err != nil {
				failed = append(failed, "the Memory Store "+store.store+" was not brought in step: "+err.Error())
				continue
			}
			if toHarness+toVault > 0 {
				s.logger.Infow("A Memory Store was brought in step", "store", store.store, "to_harness", toHarness, "to_vault", toVault, "harness", store.dir)
			}
		}
	}
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, "; "))
	}
	return nil
}

// keepMemoriesInStep brings them in step until the node stops. What failed is
// said once for as long as it keeps failing the same way.
func (s *QNTXServer) keepMemoriesInStep() {
	tick := time.NewTicker(memoryInStep)
	defer tick.Stop()
	last := ""
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
			err := s.memoriesInStep()
			switch {
			case err == nil:
				last = ""
			case err.Error() != last:
				last = err.Error()
				s.logger.Errorw("Agent memory was not brought in step with the vault", "error", err)
			}
		}
	}
}
