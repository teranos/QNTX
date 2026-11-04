package forge

import (
	"context"
	"fmt"
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
	asked   []string
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
	if _, isFile := s.branches[ref][dir]; isFile {
		return nil, NotADirectory{Kind: "file"}
	}
	var entries []Entry
	for path, content := range s.branches[ref] {
		in, name := Root, path
		if cut := strings.LastIndex(path, "/"); cut >= 0 {
			in, name = path[:cut], path[cut+1:]
		}
		if in == dir {
			entries = append(entries, Entry{Name: name, Path: path, SHA: shelfBlob(content), Kind: File})
		}
	}
	if len(entries) == 0 {
		return nil, Refused{Says: "the shelf has no " + dir}
	}
	return entries, nil
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
