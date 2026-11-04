package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

func askCodeForge(t *testing.T, s *QNTXServer, sent sigil.Sent) (any, *protocol.Refusal) {
	t.Helper()
	got, refused := s.codeForgeSignum().Answers["propose"](asRoot(), sent)
	if refused == nil {
		holds(t, s.codeForgeSignum(), "propose", got)
	}
	return got, refused
}

// "Can an agent submit a PR via sigil ?"
// Whoever asks sends the files and holds no credential: the node makes the
// branch, commits to it and opens the change, as the App's installation.
func TestAChangeIsProposedThroughASigil(t *testing.T) {
	s, _ := vaultBindingServer(t)
	asked := sigil.Sent{
		"repo":    "github.com/abcd-nl/clean",
		"branch":  "agent-adr-003",
		"title":   "ADR-003",
		"body":    "What the agent wrote.",
		"files":   `{"docs/adr/ADR-003.md": "# ADR-003\n", "docs/adr/ADR-001.md": "# ADR-001\n\nAs the agent has it.\n"}`,
		"removes": `["docs/adr/old/ADR-000.md"]`,
	}

	got, refused := askCodeForge(t, s, asked)
	require.Nil(t, refused, "%v", refused)
	proposed := got.(map[string]any)
	assert.Equal(t, int64(1), proposed["number"])
	assert.Equal(t, "https://github.com/abcd-nl/clean/pull/1", proposed["url"])
	assert.Equal(t, "agent-adr-003", proposed["branch"])
	assert.Equal(t, "main", proposed["into"], "left out, a change is proposed into the default branch")
	assert.Equal(t, []string{"docs/adr/ADR-001.md", "docs/adr/ADR-003.md"}, proposed["committed"])
	assert.Equal(t, []string{"docs/adr/old/ADR-000.md"}, proposed["removed"])

	branch := stand.branches["agent-adr-003"]
	assert.Equal(t, "# ADR-003\n", branch["docs/adr/ADR-003.md"])
	assert.Equal(t, "# ADR-001\n\nAs the agent has it.\n", branch["docs/adr/ADR-001.md"])
	assert.NotContains(t, branch, "docs/adr/old/ADR-000.md")
	assert.Equal(t, "not a note", branch["docs/adr/diagram.png"], "what was not sent is left as the branch has it")
	assert.Equal(t, "# ADR-001\n\nAs main has it.\n", mainNotes["docs/adr/ADR-001.md"], "main is changed by merging, never by proposing")
	require.Len(t, stand.pulls, 1)
	assert.Equal(t, "ADR-003", stand.pulls[0]["title"])
	assert.Equal(t, []string{"agent-adr-003: ADR-003", "agent-adr-003: ADR-003", "agent-adr-003: ADR-003"}, stand.commits,
		"left out, each commit says the title")

	// Asked again, the branch holds everything as given: nothing is committed
	// and the change that is open is the one given back.
	got, refused = askCodeForge(t, s, asked)
	require.Nil(t, refused, "%v", refused)
	again := got.(map[string]any)
	assert.Equal(t, proposed["url"], again["url"])
	assert.Empty(t, again["committed"])
	assert.Empty(t, again["removed"])
	assert.Len(t, stand.commits, 3)
	assert.Len(t, stand.pulls, 1)
}

// A change is proposed from a branch of its own: never the branch it is
// proposed into, and never the default branch, whatever it is proposed into.
func TestAChangeIsProposedFromABranchOfItsOwn(t *testing.T) {
	s, _ := vaultBindingServer(t)
	stand.branches["release"] = map[string]string{}
	for name, sent := range map[string]sigil.Sent{
		"the default branch":               {"branch": "main"},
		"the branch it is proposed into":   {"branch": "release", "into": "release"},
		"the default, into another branch": {"branch": "main", "into": "release"},
	} {
		sent["repo"], sent["title"], sent["files"] = "github.com/abcd-nl/clean", "x", `{"docs/adr/x.md": "x"}`
		_, refused := askCodeForge(t, s, sent)
		require.NotNil(t, refused, name)
		assert.Equal(t, "branch", refused.GetParam(), name)
	}
	assert.Empty(t, stand.commits)
	assert.Empty(t, stand.pulls)
	assert.Len(t, stand.branches, 2, "a branch was made for a proposal that was refused")
}

// What does not read is said by the param to change, and the forge is asked
// to commit nothing.
func TestAProposalSaysWhatToChange(t *testing.T) {
	s, _ := vaultBindingServer(t)
	for name, c := range map[string]struct {
		sent  sigil.Sent
		param string
	}{
		"files that are not JSON":         {sigil.Sent{"files": `docs/a.md=x`}, "files"},
		"a file's text that is not":       {sigil.Sent{"files": `{"docs/a.md": 1}`}, "files"},
		"a path out of the repository":    {sigil.Sent{"files": `{"../a.md": "x"}`}, "files"},
		"a path from the root":            {sigil.Sent{"files": `{"/etc/a.md": "x"}`}, "files"},
		"git's own":                       {sigil.Sent{"files": `{".git/config": "x"}`}, "files"},
		"nothing to change":               {sigil.Sent{}, "files"},
		"removes that are not a list":     {sigil.Sent{"removes": `docs/a.md`}, "removes"},
		"held and not held":               {sigil.Sent{"files": `{"docs/a.md": "x"}`, "removes": `["docs/a.md"]`}, "removes"},
		"no name git takes":               {sigil.Sent{"files": `{"docs/a.md": "x"}`, "branch": "two words"}, "branch"},
		"a forge the node does not reach": {sigil.Sent{"files": `{"docs/a.md": "x"}`, "repo": "gitlab.com/group/sub/project"}, "repo"},
		"a repository with no host":       {sigil.Sent{"files": `{"docs/a.md": "x"}`, "repo": "abcd-nl/clean"}, "repo"},
	} {
		sent := sigil.Sent{"repo": "github.com/abcd-nl/clean", "branch": "agent-x", "title": "x"}
		for param, value := range c.sent {
			sent[param] = value
		}
		_, refused := askCodeForge(t, s, sent)
		require.NotNil(t, refused, name)
		assert.Equal(t, c.param, refused.GetParam(), name)
	}
	assert.Empty(t, stand.commits)
	assert.Len(t, stand.branches, 1, "a branch was made for a proposal that was refused")
}
