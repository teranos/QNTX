package forge

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shelf is a forge that is not GitHub: one repository's branches held in
// memory, each a file's content by path, and the changes opened on it. What a
// proposal does not ask of a forge is left to the Forge it embeds, which is
// none, so asking it stops the test.
type shelf struct {
	Forge
	branches map[string]map[string]string
	changes  []proposed
	// refuses is a path the shelf takes no commit to.
	refuses string
	// unlisted is a folder the shelf holds and refuses to list.
	unlisted string
	// listsAtMost is how many entries of a folder the shelf lists, as GitHub lists 1,000; zero is all.
	listsAtMost int
	asked       []string
}

// proposed is a change open on the shelf, with the branches it is between.
type proposed struct {
	Change
	head, base string
}

func shelfBlob(content string) string { return "blob of " + content }

func (s *shelf) Branch(_ context.Context, _ Acting, _ Repo, branch string) (string, error) {
	if _, held := s.branches[branch]; !held {
		return "", Refused{Says: "the shelf has no branch " + branch}
	}
	return "tip of " + branch, nil
}

func (s *shelf) CreateBranch(_ context.Context, _ Acting, _ Repo, branch, from string) error {
	s.asked = append(s.asked, "branch "+branch)
	for name, files := range s.branches {
		if "tip of "+name != from {
			continue
		}
		made := map[string]string{}
		for path, content := range files {
			made[path] = content
		}
		s.branches[branch] = made
		return nil
	}
	return Refused{Says: "the shelf has no commit " + from}
}

func (s *shelf) AddFile(_ context.Context, _ Acting, _ Repo, branch, path string, content []byte, _ string) error {
	s.asked = append(s.asked, "add "+path)
	if _, there := s.branches[branch][path]; there || path == s.refuses {
		return Refused{Says: "the shelf takes no " + path}
	}
	s.branches[branch][path] = string(content)
	return nil
}

func (s *shelf) ReplaceFile(_ context.Context, _ Acting, _ Repo, branch, path string, content []byte, replaces, _ string) error {
	s.asked = append(s.asked, "replace "+path)
	if was, there := s.branches[branch][path]; !there || shelfBlob(was) != replaces || path == s.refuses {
		return Refused{Says: "the shelf holds another " + path}
	}
	s.branches[branch][path] = string(content)
	return nil
}

func (s *shelf) DeleteFile(_ context.Context, _ Acting, _ Repo, branch, path, sha, _ string) error {
	s.asked = append(s.asked, "remove "+path)
	if was, there := s.branches[branch][path]; !there || shelfBlob(was) != sha || path == s.refuses {
		return Refused{Says: "the shelf holds another " + path}
	}
	delete(s.branches[branch], path)
	return nil
}

func (s *shelf) Entries(_ context.Context, _ Acting, _ Repo, ref, dir string) ([]Entry, error) {
	ref = strings.TrimPrefix(ref, "tip of ")
	if _, isFile := s.branches[ref][dir]; isFile {
		return nil, NotADirectory{Kind: "file"}
	}
	var entries []Entry
	folders := map[string]bool{}
	for path, content := range s.branches[ref] {
		in, name := Root, path
		if cut := strings.LastIndex(path, "/"); cut >= 0 {
			in, name = path[:cut], path[cut+1:]
		}
		if in == dir {
			entries = append(entries, Entry{Name: name, Path: path, SHA: shelfBlob(content), Kind: File})
			continue
		}
		// A file deeper down is a folder of dir's, listed once.
		below := path
		if dir != Root {
			if !strings.HasPrefix(path, dir+"/") {
				continue
			}
			below = path[len(dir)+1:]
		}
		folder, _, _ := strings.Cut(below, "/")
		if at := strings.TrimPrefix(dir+"/"+folder, Root+"/"); !folders[at] {
			folders[at] = true
			entries = append(entries, Entry{Name: folder, Path: at, SHA: "tree of " + at, Kind: Dir})
		}
	}
	if len(entries) == 0 {
		return nil, Refused{Says: "the shelf has no " + dir, Absent: true}
	}
	if dir == s.unlisted {
		return nil, Refused{Says: "the shelf does not list " + dir + " for you"}
	}
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	if s.listsAtMost > 0 && len(entries) > s.listsAtMost {
		entries = entries[:s.listsAtMost]
	}
	return entries, nil
}

func (s *shelf) EntryAt(_ context.Context, _ Acting, _ Repo, ref, path string) (Entry, error) {
	ref = strings.TrimPrefix(ref, "tip of ")
	s.asked = append(s.asked, "at "+path)
	if content, isFile := s.branches[ref][path]; isFile {
		return Entry{Path: path, SHA: shelfBlob(content), Kind: File}, nil
	}
	for held := range s.branches[ref] {
		if strings.HasPrefix(held, path+"/") {
			return Entry{Path: path, Kind: Dir}, nil
		}
	}
	return Entry{}, Refused{Says: "the shelf has no " + path, Absent: true}
}

func (s *shelf) ChangeFrom(_ context.Context, _ Acting, _ Repo, head, base string) (Change, bool, error) {
	for _, open := range s.changes {
		if open.head == head && open.base == base {
			return open.Change, true, nil
		}
	}
	return Change{}, false, nil
}

func (s *shelf) NewChange(_ context.Context, _ Acting, repo Repo, head, base, _, _ string) (Change, error) {
	s.asked = append(s.asked, "change "+head+" into "+base)
	number := int64(len(s.changes) + 1)
	made := Change{Number: number, URL: fmt.Sprintf("https://%s/-/merge_requests/%d", repo, number)}
	s.changes = append(s.changes, proposed{Change: made, head: head, base: base})
	return made, nil
}

func shelved(t *testing.T) (*shelf, Proposal) {
	t.Helper()
	repo, err := ParseRepo("gitlab.example.org/group/subgroup/project")
	require.NoError(t, err)
	return &shelf{branches: map[string]map[string]string{
		"trunk": {"README.md": "as trunk has it", "old.md": "to go"},
	}}, Proposal{Repo: repo, Branch: "agent/notes", Into: "trunk"}
}

// "I guess the point that I am making is that it shouldn't matter what forge we use"
func TestAProposalReachesAForgeThatIsNotGitHub(t *testing.T) {
	on, proposal := shelved(t)

	require.NoError(t, proposal.HoldBranch(t.Context(), on, AsApp))
	assert.Equal(t, on.branches["trunk"], on.branches["agent/notes"], "the branch is made from what it is proposed into")
	require.NoError(t, proposal.HoldBranch(t.Context(), on, AsApp))
	assert.Equal(t, []string{"branch agent/notes"}, on.asked, "a branch the forge holds is not made again")

	done, err := proposal.Commit(t.Context(), on, AsApp, []Edit{
		Added("notes/new.md", []byte("new"), "a note"),
		Replaced("README.md", []byte("as proposed"), shelfBlob("as trunk has it"), "the README"),
		Removed("old.md", shelfBlob("to go"), "old is gone"),
	})
	require.NoError(t, err)
	assert.Equal(t, Committed{Put: []string{"notes/new.md", "README.md"}, Removed: []string{"old.md"}}, done)
	assert.Equal(t, map[string]string{"notes/new.md": "new", "README.md": "as proposed"}, on.branches["agent/notes"])
	assert.Equal(t, "as trunk has it", on.branches["trunk"]["README.md"], "what is proposed into is changed by merging, never by proposing")

	_, found, err := proposal.OpenChange(t.Context(), on, AsApp)
	require.NoError(t, err)
	assert.False(t, found)
	made, err := proposal.Propose(t.Context(), on, AsApp, "Notes", "What the agent wrote.")
	require.NoError(t, err)
	assert.Equal(t, int64(1), made.Number)
	again, err := proposal.Propose(t.Context(), on, AsApp, "Notes", "What the agent wrote.")
	require.NoError(t, err)
	assert.Equal(t, made, again)
	assert.Len(t, on.changes, 1, "a branch with a change open is not proposed twice")
}

// A proposal says what the forge refused in the forge's words, and what
// reached the branch before it.
func TestAProposalSaysWhatTheForgeRefused(t *testing.T) {
	on, proposal := shelved(t)
	orphan := Proposal{Repo: proposal.Repo, Branch: "agent/notes", Into: "main"}
	err := orphan.HoldBranch(t.Context(), on, AsApp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the shelf has no branch agent/notes")
	assert.Contains(t, err.Error(), "the shelf has no branch main")

	require.NoError(t, proposal.HoldBranch(t.Context(), on, AsApp))
	on.refuses = "locked.md"
	done, err := proposal.Commit(t.Context(), on, AsApp, []Edit{
		Added("first.md", []byte("first"), "first"),
		Added("locked.md", []byte("no"), "locked"),
		Added("never.md", []byte("never"), "never"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "locked.md was not committed to agent/notes of gitlab.example.org/group/subgroup/project")
	assert.Contains(t, err.Error(), "the shelf takes no locked.md")
	assert.Equal(t, []string{"first.md"}, done.Put)
	assert.NotContains(t, on.asked, "add never.md")
}

// An edit says what it does to its file, or nothing is asked of the forge.
func TestAnEditThatSaysNothingIsNotCommitted(t *testing.T) {
	on, proposal := shelved(t)
	_, err := proposal.Commit(t.Context(), on, AsApp, []Edit{{Path: "README.md"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "README.md")
	assert.Empty(t, on.asked)
}

// What a branch holds at a path is asked of the forge a directory at a time,
// and a directory the forge has none of holds nothing.
func TestAProposalKnowsWhatItsBranchHolds(t *testing.T) {
	on, proposal := shelved(t)
	require.NoError(t, proposal.HoldBranch(t.Context(), on, AsApp))

	held, err := proposal.Holds(t.Context(), on, AsApp, []string{"README.md", "new.md", "notes/new.md"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"README.md": shelfBlob("as trunk has it")}, held)

	_, err = proposal.Holds(t.Context(), on, AsApp, []string{"README.md/inside.md"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "README.md is a file")
}

// Qodo's review of #1515: a path the branch holds a folder at is no file to
// add. Read as absent, it was added after the files sorted before it had
// been committed, and the forge refused it with the branch half changed.
func TestAPathTheBranchHoldsAFolderAtIsSaidBeforeAnyCommit(t *testing.T) {
	on, proposal := shelved(t)
	on.branches["trunk"]["notes/kept.md"] = "kept"
	require.NoError(t, proposal.HoldBranch(t.Context(), on, AsApp))

	_, err := proposal.Holds(t.Context(), on, AsApp, []string{"README.md", "notes"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "notes is a folder")
	assert.Equal(t, []string{"branch agent/notes"}, on.asked, "nothing is committed")
}

// Qodo's review of #1515: a folder the forge refuses to list is not one it has
// none of. Read as absent, every file asked in it was added over what is there.
func TestAFolderTheForgeRefusesToListIsSaidBeforeAnyCommit(t *testing.T) {
	on, proposal := shelved(t)
	on.branches["trunk"]["notes/kept.md"] = "kept"
	on.unlisted = "notes"
	require.NoError(t, proposal.HoldBranch(t.Context(), on, AsApp))

	_, err := proposal.Holds(t.Context(), on, AsApp, []string{"notes/kept.md"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "whether it holds notes/kept.md is not known")
	assert.Contains(t, err.Error(), "the shelf does not list notes for you")
	assert.Equal(t, []string{"branch agent/notes"}, on.asked, "nothing is committed")
}

// Qodo's review of #1515: GitHub lists a folder to its first 1,000 files. A
// file past them was read as absent and added over what the branch holds.
func TestAFileItsFolderIsListedShortOfIsAskedByItself(t *testing.T) {
	on, proposal := shelved(t)
	on.branches["trunk"]["notes/a.md"] = "a"
	on.branches["trunk"]["notes/b.md"] = "b"
	on.listsAtMost = 1
	require.NoError(t, proposal.HoldBranch(t.Context(), on, AsApp))

	held, err := proposal.Holds(t.Context(), on, AsApp, []string{"notes/a.md", "notes/b.md", "notes/new.md"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"notes/a.md": shelfBlob("a"), "notes/b.md": shelfBlob("b")}, held)
	assert.Contains(t, on.asked, "at notes/b.md", "the file past the listing is asked by itself")
	assert.NotContains(t, on.asked, "at notes/a.md", "a file the listing named is not asked again")
}
