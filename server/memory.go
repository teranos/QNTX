package server

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// 「[Opus 5.5] Agent Memory (ADR-055).」
//
// "「[Opus 5.5] AND QNTX NEEDS TO HOLD IT」"
// "「[Opus 5.5] NOT THE LAPTOP」"
// "「[Opus 5.5] I EDIT」"
// "「[Opus 5.5] ITS IN MEMORY」"
//
// 「[Opus 5.5] The node holds a Memory Store per agent, and a store is a folder of the」
// 「[Opus 5.5] vault: a memory is a note in it. What a person writes in the note is the」
// 「[Opus 5.5] memory, and a note a person deletes is a memory the agent holds no more. No」
// 「[Opus 5.5] harness keeps memory of its own beside it: each reaches its store through」
// 「[Opus 5.5] these sigils.」

const memoryPath = "/api/memory"

// 「[Opus 5.5] The marks an agent's words stand inside (ADR-055): U+300C and U+300D, the」
// 「[Opus 5.5] agent's name in square brackets first.」
const (
	agentQuoteOpens  = "「"
	agentQuoteCloses = "」"
)

func (s *QNTXServer) memorySignum() sigil.Signum {
	store := &protocol.Param{Name: "store", Required: true, Says: "The Memory Store, by its name: the name its agent holds (i name), or what the node calls an agent that holds none."}
	memory := &protocol.Param{Name: "memory", Required: true, Says: "The memory, by its name: the note's, without .md."}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "memory",
			Description: "The Memory Store the node holds for each agent (ADR-055): a folder of the vault, each memory a note in it.",
			Tags:        []string{"memory", "agents", "obsidian"},
			Oasf:        []string{"ai_ml_engineering/agent_development/agent_memory"},
			Sigils: []*protocol.Sigil{
				{
					Name:   "stores",
					Does:   "Every Memory Store the node holds, the vault that backs them and the place in it.",
					Answer: "protocol.MemoryStores",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: memoryPath},
				},
				{
					Name: "keep",
					Does: "Says where agents' memory is kept: a place in a vault the node keeps a copy of, one folder per Memory Store under it. One vault backs memory; said of another, the first backs it no more. Nothing already written moves.",
					Takes: []*protocol.Param{
						{Name: "name", Required: true, Says: "The vault's name, as Obsidian Sync names it."},
						{Name: "place", Required: true, Says: "The place in the vault, as a folder's path in it."},
					},
					Answer: "protocol.MemoryStores",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: memoryPath + "/keep"},
				},
				{
					Name:   "list",
					Does:   "Every memory of a store, by name, with when each was last written.",
					Takes:  []*protocol.Param{store},
					Answer: "protocol.Memories",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: memoryPath + "/{store}"},
				},
				{
					Name:   "read",
					Does:   "One memory, whole, as the note reads now.",
					Takes:  []*protocol.Param{store, memory},
					Answer: "protocol.Memory",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: memoryPath + "/{store}/{memory}"},
				},
				{
					Name: "write",
					Does: "Writes one memory whole, as a note in the store. Written by an agent, it stands inside 「 」 with the agent's name first, and the node sees to that; written by a person, it is as they wrote it.",
					Takes: []*protocol.Param{store, memory,
						{Name: "text", Required: true, Says: "What the memory says, whole."},
					},
					Answer: "protocol.Memory",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: memoryPath + "/{store}/{memory}"},
				},
				{
					Name:   "forget",
					Does:   "Forgets one memory: its note is removed from the store, and the agent holds it no more.",
					Takes:  []*protocol.Param{store, memory},
					Answer: "protocol.Memories",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: memoryPath + "/{store}/{memory}/forget"},
				},
				{
					Name: "merge",
					Does: "Merges another store into this one: every memory of it becomes this store's, and the other is gone. Refused, with nothing moved, when both hold a memory by one name.",
					Takes: []*protocol.Param{store,
						{Name: "from", Required: true, Says: "The store that is merged in, by its name."},
					},
					Answer: "protocol.Memories",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: memoryPath + "/{store}/merge"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"stores": s.memoryStores, "keep": s.memoryKeep, "list": s.memoryList, "read": s.memoryRead,
			"write": s.memoryWrite, "forget": s.memoryForget, "merge": s.memoryMerge,
		},
	}
}

// 「[Opus 5.5] memoryKept is the vault that backs memory and the folder on the box the」
// 「[Opus 5.5] stores are under.」
func (s *QNTXServer) memoryKept() (Vault, string, *protocol.Refusal) {
	vaults, err := s.nodeRecords().Vaults()
	if err != nil {
		return Vault{}, "", &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	for _, vault := range vaults {
		if vault.Memory != "" {
			return vault, filepath.Join(vault.Path, filepath.FromSlash(vault.Memory)), nil
		}
	}
	return Vault{}, "", &protocol.Refusal{Why: sigil.NotFound, Says: "no vault backs memory on this node: memory keep says the vault and the place in it"}
}

// 「[Opus 5.5] aNoteName is whether name is one note's or one folder's own name: no path,」
// 「[Opus 5.5] and nothing of Obsidian's own, which starts with a dot.」
func aNoteName(name string) bool {
	return name != "" && name == strings.TrimSpace(name) && !strings.HasPrefix(name, ".") &&
		!strings.ContainsAny(name, `/\`) && filepath.IsLocal(name)
}

// 「[Opus 5.5] storeAt is the folder of the store sent names, under where memory is kept.」
func (s *QNTXServer) storeAt(sent sigil.Sent, param string) (string, string, *protocol.Refusal) {
	name := sent[param]
	if !aNoteName(name) {
		return "", "", &protocol.Refusal{Why: sigil.Invalid, Param: param, Says: "a Memory Store is named as one folder is, and this said " + name}
	}
	_, kept, refused := s.memoryKept()
	if refused != nil {
		return "", "", refused
	}
	return name, filepath.Join(kept, name), nil
}

// 「[Opus 5.5] memoryAt is the note of the memory sent names, in its store.」
func (s *QNTXServer) memoryAt(sent sigil.Sent) (store, name, note string, refused *protocol.Refusal) {
	store, at, refused := s.storeAt(sent, "store")
	if refused != nil {
		return "", "", "", refused
	}
	name = sent["memory"]
	if !aNoteName(name) {
		return "", "", "", &protocol.Refusal{Why: sigil.Invalid, Param: "memory", Says: "a memory is named as one note is, and this said " + name}
	}
	return store, name, filepath.Join(at, name+".md"), nil
}

// 「[Opus 5.5] memoriesIn is every memory of the store at folder, by name, without its text.」
func memoriesIn(store, folder string) ([]*protocol.Memory, error) {
	entries, err := os.ReadDir(folder)
	if errors.Is(err, fs.ErrNotExist) {
		return []*protocol.Memory{}, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "the Memory Store %s at %s was not read", store, folder)
	}
	memories := []*protocol.Memory{}
	for _, entry := range entries {
		name, isNote := strings.CutSuffix(entry.Name(), ".md")
		if entry.IsDir() || !isNote || !aNoteName(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, errors.Wrapf(err, "the memory %s of %s was not read", name, store)
		}
		memories = append(memories, &protocol.Memory{Store: store, Name: name, Modified: info.ModTime().UTC().Format(time.RFC3339)})
	}
	slices.SortFunc(memories, func(a, b *protocol.Memory) int { return strings.Compare(a.GetName(), b.GetName()) })
	return memories, nil
}

func (s *QNTXServer) memoryStores(context.Context, sigil.Sent) (any, *protocol.Refusal) {
	vault, kept, refused := s.memoryKept()
	if refused != nil {
		return nil, refused
	}
	answer := &protocol.MemoryStores{Vault: vault.Name, Place: vault.Memory, Stores: []*protocol.MemoryStore{}}
	entries, err := os.ReadDir(kept)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "where memory is kept, " + kept + ", was not read: " + err.Error()}
	}
	for _, entry := range entries {
		if !entry.IsDir() || !aNoteName(entry.Name()) {
			continue
		}
		memories, err := memoriesIn(entry.Name(), filepath.Join(kept, entry.Name()))
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		answer.Stores = append(answer.Stores, &protocol.MemoryStore{
			Name: entry.Name(), Memories: int32(len(memories)), Place: vault.Memory + "/" + entry.Name(),
		})
	}
	return answer, nil
}

func (s *QNTXServer) memoryKeep(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	vault, refused := s.vaultNamed(strings.TrimSpace(sent["name"]))
	if refused != nil {
		return nil, refused
	}
	place := strings.TrimSpace(sent["place"])
	if place == "" || filepath.IsAbs(place) || !filepath.IsLocal(place) {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "place", Says: "where memory is kept is a place inside the vault, and this said " + place}
	}
	place = filepath.ToSlash(filepath.Clean(place))
	vaults, err := s.nodeRecords().Vaults()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	// 「[Opus 5.5] One vault backs memory: a store is in one place.」
	for _, other := range vaults {
		if other.Name == vault.Name || other.Memory == "" {
			continue
		}
		other.Memory = ""
		if err := s.nodeRecords().SetVault(actorOf(ctx), other); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
	}
	vault.Memory = place
	if err := s.nodeRecords().SetVault(actorOf(ctx), vault); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return s.memoryStores(ctx, sent)
}

func (s *QNTXServer) memoryList(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	store, at, refused := s.storeAt(sent, "store")
	if refused != nil {
		return nil, refused
	}
	memories, err := memoriesIn(store, at)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return &protocol.Memories{Store: store, Memories: memories}, nil
}

func (s *QNTXServer) memoryRead(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	store, name, note, refused := s.memoryAt(sent)
	if refused != nil {
		return nil, refused
	}
	return readMemory(store, name, note)
}

// 「[Opus 5.5] readMemory is the memory name of store, as its note reads now.」
func readMemory(store, name, note string) (*protocol.Memory, *protocol.Refusal) {
	text, err := os.ReadFile(note)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "memory", Says: store + " holds no memory named " + name}
	}
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the memory " + name + " of " + store + " was not read: " + err.Error()}
	}
	info, err := os.Stat(note)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the memory " + name + " of " + store + " was not read: " + err.Error()}
	}
	return &protocol.Memory{Store: store, Name: name, Text: string(text), Modified: info.ModTime().UTC().Format(time.RFC3339)}, nil
}

// inItsOwnQuotation is what an agent wrote, standing inside 「 」 with its name
// 「[Opus 5.5] first. What already stands so, by any agent, is as it was written.」
func inItsOwnQuotation(called, text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, agentQuoteOpens+"[") && strings.HasSuffix(text, agentQuoteCloses) {
		return text + "\n"
	}
	return agentQuoteOpens + "[" + called + "] " + text + agentQuoteCloses + "\n"
}

// 「[Opus 5.5] writtenBy is the name of the agent asking, and whether an agent asks: a」
// 「[Opus 5.5] person's words are theirs, and stand as they wrote them.」
func (s *QNTXServer) writtenBy(ctx context.Context) (string, bool, *protocol.Refusal) {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return "", false, &protocol.Refusal{Why: sigil.NotAllowed, Says: "a memory is written by whoever the node admitted, and this asking came through no gate"}
	}
	did, refusal := itsOwnDID(admitted)
	if refusal != nil {
		return "", false, nil
	}
	names, err := s.agentNames()
	if err != nil {
		return "", false, &protocol.Refusal{Why: sigil.Failed, Says: "the name of the agent writing was not read: " + err.Error()}
	}
	if at := slices.IndexFunc(names, func(kept agentName) bool { return kept.did == did && !kept.let }); at >= 0 {
		return names[at].name, true, nil
	}
	return did, true, nil
}

func (s *QNTXServer) memoryWrite(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	store, name, note, refused := s.memoryAt(sent)
	if refused != nil {
		return nil, refused
	}
	text := sent["text"]
	if strings.TrimSpace(text) == "" {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "text", Says: "a memory says something, and this said nothing: memory forget removes one"}
	}
	called, anAgent, refused := s.writtenBy(ctx)
	if refused != nil {
		return nil, refused
	}
	if anAgent {
		text = inItsOwnQuotation(called, text)
	}
	if err := os.MkdirAll(filepath.Dir(note), 0o755); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the Memory Store " + store + " was not made: " + err.Error()}
	}
	if err := os.WriteFile(note, []byte(text), 0o644); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the memory " + name + " of " + store + " was not written: " + err.Error()}
	}
	return readMemory(store, name, note)
}

func (s *QNTXServer) memoryForget(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	store, name, note, refused := s.memoryAt(sent)
	if refused != nil {
		return nil, refused
	}
	err := os.Remove(note)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "memory", Says: store + " holds no memory named " + name}
	}
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the memory " + name + " of " + store + " was not forgotten: " + err.Error()}
	}
	return s.memoryList(ctx, sent)
}

func (s *QNTXServer) memoryMerge(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	store, into, refused := s.storeAt(sent, "store")
	if refused != nil {
		return nil, refused
	}
	other, from, refused := s.storeAt(sent, "from")
	if refused != nil {
		return nil, refused
	}
	if other == store {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "from", Says: store + " is merged into another store, not into itself"}
	}
	coming, err := memoriesIn(other, from)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	if len(coming) == 0 {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "from", Says: other + " holds no memory to merge"}
	}
	held, err := memoriesIn(store, into)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	both := []string{}
	for _, memory := range coming {
		if slices.ContainsFunc(held, func(kept *protocol.Memory) bool { return kept.GetName() == memory.GetName() }) {
			both = append(both, memory.GetName())
		}
	}
	if len(both) > 0 {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "from",
			Says: store + " and " + other + " both hold " + strings.Join(both, ", ") + ": nothing was merged"}
	}
	if err := os.MkdirAll(into, 0o755); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the Memory Store " + store + " was not made: " + err.Error()}
	}
	for _, memory := range coming {
		note := memory.GetName() + ".md"
		if err := os.Rename(filepath.Join(from, note), filepath.Join(into, note)); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the memory " + memory.GetName() + " of " + other + " was not merged into " + store + ": " + err.Error()}
		}
	}
	// 「[Opus 5.5] The store merged in is gone when nothing else was in its folder.」
	if err := os.Remove(from); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.logger.Infow("A Memory Store merged into another keeps its folder: it holds more than memories", "store", other, "folder", from, "because", err.Error())
	}
	return s.memoryList(ctx, sent)
}
