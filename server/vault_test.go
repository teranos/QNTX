package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// "name both ends"

// A repository's folder, read as a build's input is, and where in the vault it is.
func TestAVaultHoldsFoldersNamedAtBothEnds(t *testing.T) {
	folders, err := vaultFolders("abcd-nl/clean@main:cdr=ABCD/clean/cdr  abcd-nl/clean-business@main:WEEK_41.md=ABCD/weeks/WEEK_41.md")
	require.NoError(t, err)
	assert.Equal(t, []string{"abcd-nl/clean@main:cdr=ABCD/clean/cdr", "abcd-nl/clean-business@main:WEEK_41.md=ABCD/weeks/WEEK_41.md"}, folders)

	none, err := vaultFolders("")
	require.NoError(t, err)
	assert.Equal(t, []string{}, none)

	// A place is kept as it reads, without what says nothing.
	tidy, err := vaultFolders("abcd-nl/clean@main:cdr=ABCD//clean/./cdr/")
	require.NoError(t, err)
	assert.Equal(t, []string{"abcd-nl/clean@main:cdr=ABCD/clean/cdr"}, tidy)
}

// A folder missing either end is refused, and with it the whole vault.
func TestAVaultRefusesAFolderThatDoesNotRead(t *testing.T) {
	for _, said := range []string{
		"abcd-nl/clean@main:cdr",
		"abcd-nl/clean@main:cdr=",
		"abcd-nl/clean@main=ABCD/clean",
		"abcd-nl/clean:cdr=ABCD/clean",
		"clean@main:cdr=ABCD/clean",
		"abcd-nl/clean@main:cdr=ABCD/clean abcd-nl/clean@main:docs",
	} {
		_, err := vaultFolders(said)
		assert.Error(t, err, said)
	}
}

// A place is inside the vault.
func TestAVaultRefusesAPlaceOutsideIt(t *testing.T) {
	for _, said := range []string{
		"abcd-nl/clean@main:cdr=/etc/cdr",
		"abcd-nl/clean@main:cdr=../cdr",
		"abcd-nl/clean@main:cdr=ABCD/../../cdr",
	} {
		_, err := vaultFolders(said)
		assert.Error(t, err, said)
	}
}

// A note is one folder's: no two places are one, and none holds another.
func TestAVaultRefusesTwoFoldersAtOnePlace(t *testing.T) {
	for _, said := range []string{
		"abcd-nl/clean@main:cdr=ABCD/docs abcd-nl/q@main:docs=ABCD/docs",
		"abcd-nl/clean@main:cdr=ABCD abcd-nl/q@main:docs=ABCD/docs",
		"abcd-nl/clean@main:cdr=ABCD/docs/cdr abcd-nl/q@main:docs=ABCD/docs",
	} {
		_, err := vaultFolders(said)
		assert.Error(t, err, said)
	}
	// Two places that only share a beginning are two places.
	_, err := vaultFolders("abcd-nl/clean@main:cdr=ABCD/doc abcd-nl/q@main:docs=ABCD/docs")
	assert.NoError(t, err)
}

func TestTheVaultSignumSaysWhatItHolds(t *testing.T) {
	s := &QNTXServer{}
	require.NoError(t, s.vaultSignum().Check())
}

// A vault's folders are read from its copy, and Obsidian's own are left out.
func TestAVaultsFoldersAreReadFromTheBox(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"ABCD/lttr", "Course Material", ".obsidian/themes", ".trash/old"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "ABCD", "note.md"), []byte("x"), 0o644))

	dirs, err := vaultDirsAt(root)
	require.NoError(t, err)
	assert.Equal(t, []string{"ABCD", "ABCD/lttr", "Course Material"}, dirs)
}

// A place may hold a space when the folders are read one by one.
func TestAPlaceMayHoldASpace(t *testing.T) {
	folders, err := vaultFoldersOf([]string{"abcd-nl/clean@main:cdr=Course Material"})
	require.NoError(t, err)
	assert.Equal(t, []string{"abcd-nl/clean@main:cdr=Course Material"}, folders)
}

// mainNotes is what docs/adr of abcd-nl/clean holds on main, by path.
var mainNotes map[string]string

// vaultBindingServer is a node holding the App and the vault abcd, whose
// GitHub is a stand-in: the App installed on abcd-nl, reaching abcd-nl/clean,
// whose main holds cdr and docs/adr.
func vaultBindingServer(t *testing.T) (*QNTXServer, Vault) {
	t.Helper()
	s := githubKnowingServer(t)
	mainNotes = map[string]string{
		"docs/adr/ADR-001.md":     "# ADR-001\n\nAs main has it.\n",
		"docs/adr/old/ADR-000.md": "# ADR-000\n",
		"docs/adr/diagram.png":    "not a note",
	}
	// A filling runs before what started it returns, so a test reads what it wrote.
	goFill = func(_ string, fn func()) { fn() }
	seenDir, keptSeen := t.TempDir(), vaultsSeenDir
	vaultsSeenDir = func() (string, error) { return seenDir, nil }
	t.Cleanup(func() { goFill, vaultsSeenDir = sacred.Go, keptSeen })
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	s.authHandler.SetGitHubApp("Iv23li-the-app", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))

	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented := r.Header.Get("Authorization")
		ownedByTheApp := strings.HasSuffix(r.URL.Path, "/installation") || strings.HasPrefix(r.URL.Path, "/app/")
		give := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		switch {
		case ownedByTheApp && !strings.HasPrefix(presented, "Bearer ey"),
			!ownedByTheApp && presented != "Bearer ghs_minted":
			w.WriteHeader(http.StatusUnauthorized)
			give(map[string]string{"message": "Bad credentials"})
		case r.URL.Path == "/app/installations":
			give([]any{map[string]any{"id": 7, "account": map[string]any{"login": "abcd-nl", "type": "Organization"}}})
		case r.URL.Path == "/repos/abcd-nl/clean/installation":
			give(map[string]any{"id": 7, "app_slug": "the-app"})
		case r.URL.Path == "/app/installations/7/access_tokens":
			w.WriteHeader(http.StatusCreated)
			give(map[string]any{"token": "ghs_minted", "expires_at": "2099-01-01T00:00:00Z"})
		case r.URL.Path == "/installation/repositories":
			give(map[string]any{"total_count": 1, "repositories": []any{map[string]any{"full_name": "abcd-nl/clean"}}})
		case r.URL.Path == "/repos/abcd-nl/clean/contents/." && r.URL.Query().Get("ref") == "main":
			give([]any{
				map[string]any{"type": "dir", "path": "docs"},
				map[string]any{"type": "dir", "path": "cdr"},
				map[string]any{"type": "file", "path": "README.md"},
			})
		case r.URL.Path == "/repos/abcd-nl/clean/contents/docs" && r.URL.Query().Get("ref") == "main":
			give([]any{map[string]any{"type": "dir", "path": "docs/adr"}})
		case r.URL.Path == "/repos/abcd-nl/clean/contents/cdr" && r.URL.Query().Get("ref") == "main":
			give([]any{map[string]any{"type": "file", "path": "cdr/CDR-001.md"}})
		case r.URL.Path == "/repos/abcd-nl/clean/contents/cdr/CDR-001.md":
			give(map[string]any{"type": "file", "path": "cdr/CDR-001.md"})
		case strings.HasPrefix(r.URL.Path, "/repos/abcd-nl/clean/contents/docs/adr") && r.URL.Query().Get("ref") == "main":
			// docs/adr on main is mainNotes: a note is given with its content, a folder lists what is directly in it.
			asked := strings.TrimPrefix(r.URL.Path, "/repos/abcd-nl/clean/contents/")
			if content, held := mainNotes[asked]; held {
				give(map[string]any{"type": "file", "name": path.Base(asked), "path": asked, "sha": gitBlobSHA([]byte(content)),
					"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))})
				return
			}
			entries, listed := []any{}, map[string]bool{}
			for held, content := range mainNotes {
				rest, inside := strings.CutPrefix(held, asked+"/")
				if !inside {
					continue
				}
				if dir, _, deeper := strings.Cut(rest, "/"); deeper {
					if !listed[dir] {
						listed[dir] = true
						entries = append(entries, map[string]any{"type": "dir", "name": dir, "path": asked + "/" + dir})
					}
					continue
				}
				entries = append(entries, map[string]any{"type": "file", "name": rest, "path": held, "sha": gitBlobSHA([]byte(content))})
			}
			give(entries)
		default:
			w.WriteHeader(http.StatusNotFound)
			give(map[string]string{"message": "Not Found"})
		}
	}))
	t.Cleanup(github.Close)
	s.gitHubService().SetBaseURL(github.URL)

	vault := Vault{Name: "abcd", Path: t.TempDir(), Folders: []string{}}
	for _, dir := range []string{"ABCD/lttr", "Course Material"} {
		require.NoError(t, os.MkdirAll(filepath.Join(vault.Path, dir), 0o755))
	}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))
	return s, vault
}

func askVault(t *testing.T, s *QNTXServer, name string, sent sigil.Sent) (any, *protocol.Refusal) {
	t.Helper()
	got, refused := s.vaultSignum().Answers[name](asRoot(), sent)
	if refused == nil {
		for _, sg := range s.vaultSignum().GetSigils() {
			if sg.GetName() == name {
				raw, err := json.Marshal(got)
				require.NoError(t, err)
				require.NoError(t, sigil.Holds(sg, raw))
			}
		}
	}
	return got, refused
}

// "left click a dir: - Bind to repository md folder - gh user or orgs, click
// one - see repos, click one - see subdirs, click one"
func TestAVaultFolderIsBoundByClicking(t *testing.T) {
	s, vault := vaultBindingServer(t)

	got, refused := askVault(t, s, "dirs", sigil.Sent{"name": "abcd"})
	require.Nil(t, refused)
	assert.Equal(t, []string{"ABCD", "ABCD/lttr", "Course Material"}, got.(map[string]any)["dirs"])

	got, refused = askVault(t, s, "owners", sigil.Sent{})
	require.Nil(t, refused, "%v", refused)
	assert.Equal(t, []vaultOwner{{Login: "abcd-nl", Type: "Organization", Installation: 7}}, got.(map[string]any)["owners"])

	got, refused = askVault(t, s, "repos", sigil.Sent{"installation": "7"})
	require.Nil(t, refused, "%v", refused)
	assert.Equal(t, []string{"abcd-nl/clean"}, got.(map[string]any)["repos"])

	got, refused = askVault(t, s, "subdirs", sigil.Sent{"repo": "abcd-nl/clean"})
	require.Nil(t, refused, "%v", refused)
	assert.Equal(t, []string{"cdr", "docs"}, got.(map[string]any)["dirs"])
	got, refused = askVault(t, s, "subdirs", sigil.Sent{"repo": "abcd-nl/clean", "path": "docs"})
	require.Nil(t, refused, "%v", refused)
	assert.Equal(t, []string{"docs/adr"}, got.(map[string]any)["dirs"])

	_, refused = askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Course Material", "repo": "abcd-nl/clean", "path": "docs/adr"})
	require.Nil(t, refused, "%v", refused)
	vaults, err := s.nodeRecords().Vaults()
	require.NoError(t, err)
	require.Len(t, vaults, 1)
	assert.Equal(t, vault.Path, vaults[0].Path)
	assert.Equal(t, []string{"abcd-nl/clean@main:docs/adr=Course Material"}, vaults[0].Folders)
}

// A folder bound, or inside one, or holding one, is not bound again; nor is a
// folder the vault's copy does not have.
func TestABindingIsRefusedWhereANoteWouldBeTwoFolders(t *testing.T) {
	s, _ := vaultBindingServer(t)
	_, refused := askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "ABCD", "repo": "abcd-nl/clean", "path": "cdr"})
	require.Nil(t, refused, "%v", refused)

	for _, place := range []string{"ABCD", "ABCD/lttr"} {
		_, refused = askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": place, "repo": "abcd-nl/clean", "path": "docs"})
		require.NotNil(t, refused, place)
		assert.Contains(t, refused.GetSays(), "one holds the other", place)
	}
	_, refused = askVault(t, s, "bind", sigil.Sent{"name": "abcd", "place": "Nowhere", "repo": "abcd-nl/clean", "path": "docs"})
	require.NotNil(t, refused)
	assert.Equal(t, "place", refused.GetParam())
	_, refused = askVault(t, s, "dirs", sigil.Sent{"name": "efgh"})
	require.NotNil(t, refused)
	assert.Equal(t, "name", refused.GetParam())
}

// A folder unbound is no longer held, and the others stay as they were.
func TestABoundFolderIsUnbound(t *testing.T) {
	s, vault := vaultBindingServer(t)
	vault.Folders = []string{"abcd-nl/clean@main:docs/adr=Course Material", "abcd-nl/clean@main:cdr=ABCD"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))

	_, refused := askVault(t, s, "unbind", sigil.Sent{"name": "abcd", "place": "Course Material"})
	require.Nil(t, refused, "%v", refused)
	vaults, err := s.nodeRecords().Vaults()
	require.NoError(t, err)
	assert.Equal(t, []string{"abcd-nl/clean@main:cdr=ABCD"}, vaults[0].Folders)

	_, refused = askVault(t, s, "unbind", sigil.Sent{"name": "abcd", "place": "Course Material"})
	require.NotNil(t, refused)
	assert.Equal(t, "place", refused.GetParam())
}

// "and another button to simply disable it, but the bind is still there, it just doesnt do anything"
func TestADisabledFolderStaysBound(t *testing.T) {
	s, vault := vaultBindingServer(t)
	vault.Folders = []string{"abcd-nl/clean@main:docs/adr=Course Material"}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))

	_, refused := askVault(t, s, "disable", sigil.Sent{"name": "abcd", "place": "Course Material"})
	require.Nil(t, refused, "%v", refused)
	vaults, err := s.nodeRecords().Vaults()
	require.NoError(t, err)
	assert.Equal(t, vault.Folders, vaults[0].Folders)
	assert.Equal(t, []string{"Course Material"}, vaults[0].Disabled)
	got, refused := askVault(t, s, "states", sigil.Sent{"name": "abcd"})
	require.Nil(t, refused, "%v", refused)
	assert.Equal(t, vaultDisabled, got.(map[string]any)["folders"].([]vaultFolderState)[0].State)

	_, refused = askVault(t, s, "enable", sigil.Sent{"name": "abcd", "place": "Course Material"})
	require.Nil(t, refused, "%v", refused)
	got, _ = askVault(t, s, "states", sigil.Sent{"name": "abcd"})
	assert.Equal(t, vaultActive, got.(map[string]any)["folders"].([]vaultFolderState)[0].State)

	_, refused = askVault(t, s, "disable", sigil.Sent{"name": "abcd", "place": "ABCD"})
	require.NotNil(t, refused)
	assert.Equal(t, "place", refused.GetParam())
}

// "what should a valid binding show? that its active, green dot,"
// A folder is invalid, and says why, when either end is not there.
func TestEachBoundFolderSaysWhetherItIsActive(t *testing.T) {
	s, vault := vaultBindingServer(t)
	require.NoError(t, os.MkdirAll(filepath.Join(vault.Path, "EFGH"), 0o755))
	vault.Folders = []string{
		"abcd-nl/clean@main:docs/adr=Course Material",
		"abcd-nl/clean@main:gone=ABCD/lttr",
		"abcd-nl/clean@main:cdr/CDR-001.md=EFGH",
		"abcd-nl/clean@main:docs=Gone",
	}
	require.NoError(t, s.nodeRecords().SetVault(rootAccount, vault))

	got, refused := askVault(t, s, "states", sigil.Sent{"name": "abcd"})
	require.Nil(t, refused, "%v", refused)
	states := got.(map[string]any)["folders"].([]vaultFolderState)
	require.Len(t, states, 4)
	assert.Equal(t, vaultActive, states[0].State)
	assert.Empty(t, states[0].Why)
	assert.Equal(t, vaultInvalid, states[1].State)
	assert.Contains(t, states[1].Why, "Not Found")
	assert.Equal(t, vaultInvalid, states[2].State)
	assert.Contains(t, states[2].Why, "cdr/CDR-001.md is a file")
	assert.Equal(t, vaultInvalid, states[3].State)
	assert.Contains(t, states[3].Why, "Gone is no folder of abcd")
}
