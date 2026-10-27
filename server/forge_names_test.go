package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
)

func TestOnlyOwnerSlashRepoIsAGitHubRepositoryWrittenBeforeForgesWereNamed(t *testing.T) {
	for said, want := range map[string]string{
		"teranos/QNTX":          "github.com/teranos/QNTX",
		" abcd-nl/q.abcd.nl ":   "github.com/abcd-nl/q.abcd.nl",
		"github.com/teranos/x":  "",
		"gitlab.com/g/sub/proj": "",
		"a/b/c":                 "",
		"QNTX":                  "",
		"/QNTX":                 "",
	} {
		got, legacy := legacyRepo(said)
		assert.Equal(t, want != "", legacy, said)
		if legacy {
			assert.Equal(t, want, got, said)
		}
	}
}

func TestAPluginsBuildRepositoriesAreNamedByTheirForgeOnce(t *testing.T) {
	s, _ := vaultBindingServer(t)
	records := s.pluginRecords()
	require.NoError(t, records.pluginLine(rootAccount, grpcplugin.PluginRecord{
		Name: "datapunt", Repo: "https://github.com/teranos/datapunt", Enabled: true,
		Config: map[string]string{
			buildCore:      "teranos/datapunt@main",
			buildInputs:    "abcd-nl/clean@main:competitor.cue github.com/abcd-nl/q.abcd.nl@master:company.cue",
			buildInputsEnv: "DATAPUNT_SCHEMAS",
		},
	}, map[string]any{}))
	require.NoError(t, records.pluginLine(rootAccount, grpcplugin.PluginRecord{
		Name: "named", Repo: "https://github.com/teranos/named", Enabled: true,
		Config: map[string]string{buildCore: "github.com/teranos/named@main"},
	}, map[string]any{}))
	namedBefore, err := records.newest()
	require.NoError(t, err)

	require.NoError(t, s.nameForges())

	got, found, err := records.Plugin("datapunt")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "github.com/teranos/datapunt@main", got.Config[buildCore])
	assert.Equal(t, "github.com/abcd-nl/clean@main:competitor.cue github.com/abcd-nl/q.abcd.nl@master:company.cue", got.Config[buildInputs])
	assert.Equal(t, "DATAPUNT_SCHEMAS", got.Config[buildInputsEnv])
	assert.True(t, got.Enabled)
	assert.Equal(t, "https://github.com/teranos/datapunt", got.Repo)

	namedAfter, err := records.newest()
	require.NoError(t, err)
	assert.Equal(t, namedBefore["named"].ID, namedAfter["named"].ID, "a plugin already naming its forge was written again")

	require.NoError(t, s.nameForges())
	again, err := records.newest()
	require.NoError(t, err)
	assert.Equal(t, namedAfter["datapunt"].ID, again["datapunt"].ID, "naming a second time wrote a second line")
}

func TestAVaultsFoldersAreNamedByTheirForge(t *testing.T) {
	s, vault := vaultBindingServer(t)
	vault.Folders = []string{"abcd-nl/clean@main:docs/adr=Course Material", "github.com/abcd-nl/q@main:docs=ABCD/docs"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))

	require.NoError(t, s.nameForges())

	vaults, err := s.nodeRecords().Vaults()
	require.NoError(t, err)
	require.Len(t, vaults, 1)
	assert.Equal(t, []string{"github.com/abcd-nl/clean@main:docs/adr=Course Material", "github.com/abcd-nl/q@main:docs=ABCD/docs"}, vaults[0].Folders)
}

func TestAFollowIsNamedByItsForgeAndTheOldLineIsRetired(t *testing.T) {
	s, _ := vaultBindingServer(t)
	require.NoError(t, s.nodeRecords().SetFollow(rootAccount, GitHubFollow{
		Repo: "teranos/QNTX", Branch: "main", Dispatches: "sbvh-nl/q.sbvh.nl", Workflow: "deploy.yml", Ref: "master", Enabled: true,
	}))

	require.NoError(t, s.nameForges())

	follows, err := s.nodeRecords().Follows()
	require.NoError(t, err)
	require.Len(t, follows, 1, "the retired line is still read")
	assert.Equal(t, "github.com/teranos/QNTX", follows[0].Repo)
	assert.Equal(t, "github.com/sbvh-nl/q.sbvh.nl", follows[0].Dispatches)
	assert.Equal(t, "deploy.yml", follows[0].Workflow)
	assert.True(t, follows[0].Enabled)
	assert.Equal(t, "follow:github.com/teranos/QNTX@main>github.com/sbvh-nl/q.sbvh.nl/deploy.yml", follows[0].line())

	require.NoError(t, s.nameForges())
	again, err := s.nodeRecords().Follows()
	require.NoError(t, err)
	assert.Equal(t, follows, again)
}

// "k should follow QNTX main through webhooks"
// One push dispatches every workflow that follows its repo and branch.
func TestARepoAndBranchIsFollowedByEachWorkflowItDispatches(t *testing.T) {
	s, _ := vaultBindingServer(t)
	for _, f := range []GitHubFollow{
		{Repo: "github.com/teranos/QNTX", Branch: "main", Dispatches: "github.com/sbvh-nl/q.sbvh.nl", Workflow: "deploy.yml", Ref: "master", Enabled: true},
		{Repo: "github.com/teranos/QNTX", Branch: "main", Dispatches: "github.com/sbvh-nl/k.sbvh.nl", Workflow: "kapteyn-switch.yml", Ref: "main", Enabled: true},
	} {
		require.NoError(t, s.nodeRecords().SetFollow(rootAccount, f))
	}
	follows, err := s.nodeRecords().Follows()
	require.NoError(t, err)
	require.Len(t, follows, 2)
	assert.Equal(t, "github.com/sbvh-nl/k.sbvh.nl", follows[0].Dispatches)
	assert.Equal(t, "github.com/sbvh-nl/q.sbvh.nl", follows[1].Dispatches)
}

// A follow kept under the line it had before it named what it dispatches is
// kept under its new line, and the old one retired, so it dispatches once.
func TestAFollowIsNamedByWhatItDispatchesAndTheOldLineIsRetired(t *testing.T) {
	s, _ := vaultBindingServer(t)
	require.NoError(t, s.nodeRecords().nodeRecord(rootAccount, githubSubject, "follow:github.com/teranos/QNTX@main", "_", map[string]any{
		"repo": "github.com/teranos/QNTX", "branch": "main", "dispatches": "github.com/sbvh-nl/q.sbvh.nl",
		"workflow": "deploy.yml", "ref": "master", "inputs": map[string]any{}, "enabled": true,
	}))

	require.NoError(t, s.nameForges())

	follows, err := s.nodeRecords().Follows()
	require.NoError(t, err)
	require.Len(t, follows, 1, "the old line is retired, not read beside the new one")
	assert.Equal(t, "follow:github.com/teranos/QNTX@main>github.com/sbvh-nl/q.sbvh.nl/deploy.yml", follows[0].line())
	assert.True(t, follows[0].Enabled)
}
