package comet

// "the feature i am imagining is called comet"

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "A comet is one sand for one user, one project and one platform."
// ground asks for the sand of its own engine ABI (ground source/comet.d cometUrlInto); a build is asked for without one.
func TestACometIsNamedByItsRepositoryPlatformAndEngineABI(t *testing.T) {
	n, err := Of("/comet/abcd-nl/grove/x86_64-linux/13452661796216880070")
	require.NoError(t, err)
	assert.Equal(t, Name{Owner: "abcd-nl", Repo: "grove", Platform: "x86_64-linux", ABI: "13452661796216880070"}, n)
	assert.Equal(t, "abcd-nl/grove/x86_64-linux/13452661796216880070", n.Key())

	built, err := Of("/comet/abcd-nl/grove/x86_64-linux")
	require.NoError(t, err)
	assert.Equal(t, Name{Owner: "abcd-nl", Repo: "grove", Platform: "x86_64-linux"}, built)
	assert.Equal(t, "abcd-nl/grove/x86_64-linux", built.Key())

	for _, path := range []string{"/comet/", "/comet/abcd-nl", "/comet/abcd-nl/grove", "/comet/abcd-nl/grove/", "/comet/abcd-nl/grove/x86_64-linux/", "/comet/abcd-nl/grove/x86_64-linux/abc", "/comet/abcd-nl/grove/x86_64-linux/1/more", "/comet//grove/x86_64-linux", "/comet/../grove/x86_64-linux", "/comet/abcd-nl/grove/windows"} {
		_, err := Of(path)
		assert.Error(t, err, path)
	}
}

// A comet is make sand's library, kept by the engine ABI it was built for.
func TestACometIsKeptAsTheSandOfItsABI(t *testing.T) {
	t.Setenv("HOME", "/h")
	linux, err := Path(Name{Owner: "abcd-nl", Repo: "grove", Platform: "x86_64-linux", ABI: "7"})
	require.NoError(t, err)
	assert.Equal(t, "/h/.qntx/comets/abcd-nl/grove/x86_64-linux/7/libground_sand.so", linux)
	darwin, err := Path(Name{Owner: "abcd-nl", Repo: "grove", Platform: "aarch64-darwin", ABI: "7"})
	require.NoError(t, err)
	assert.Equal(t, "/h/.qntx/comets/abcd-nl/grove/aarch64-darwin/7/libground_sand.dylib", darwin)

	// The ABI a build is kept under is the one make sand wrote beside the library.
	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "build"), 0o755))
	_, err = BuiltABI(src)
	assert.ErrorContains(t, err, "libground_sand.abi")
	require.NoError(t, os.WriteFile(filepath.Join(src, "build", "libground_sand.abi"), []byte("13452661796216880070\n"), 0o644))
	abi, err := BuiltABI(src)
	require.NoError(t, err)
	assert.Equal(t, "13452661796216880070", abi)
	require.NoError(t, os.WriteFile(filepath.Join(src, "build", "libground_sand.abi"), []byte("not an abi\n"), 0o644))
	_, err = BuiltABI(src)
	assert.ErrorContains(t, err, "not an abi")
}

func TestAmGroundNamesEachCometTheNodeKeeps(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "abcd-nl", "grove", "aarch64-darwin", "7", "libground_sand.dylib")
	require.NoError(t, os.MkdirAll(filepath.Dir(kept), 0o755))
	require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o644))
	require.NoError(t, KeepDigest(kept))
	require.NoError(t, os.WriteFile(kept+".machine", []byte("abcd-nl/l"), 0o644))
	// A build still landing is not a comet yet.
	require.NoError(t, os.WriteFile(kept+".new", []byte("half"), 0o755))

	moved := func(owner, repo string) (int, error) {
		assert.Equal(t, "abcd-nl/grove", owner+"/"+repo)
		return 12, nil
	}
	comets, err := Held(dir, moved)
	require.NoError(t, err)
	require.Len(t, comets, 1)
	sum := sha256.Sum256([]byte("a comet"))
	// As the Ground element draws a comet: the repository, built, observed, and how often main moved.
	assert.Equal(t, "abcd-nl/grove", comets[0].Repo)
	assert.True(t, comets[0].Built)
	assert.Equal(t, "observed", comets[0].State)
	assert.Equal(t, 12, comets[0].Moved)
	assert.InDelta(t, 7.0/(1<<20), comets[0].Size, 1e-12)
	require.Len(t, comets[0].Builds, 1)
	assert.Equal(t, "aarch64-darwin", comets[0].Builds[0].Platform)
	assert.Equal(t, "7", comets[0].Builds[0].ABI)
	assert.Equal(t, hex.EncodeToString(sum[:]), comets[0].Builds[0].Digest)
	assert.Equal(t, int64(7), comets[0].Builds[0].Bytes)

	// A node that has built none keeps none, and says so as an empty list.
	none, err := Held(filepath.Join(dir, "nothing"), moved)
	require.NoError(t, err)
	assert.Empty(t, none)
}

// handler answers as a node does, in JSON, and builds nothing.
func handler() Handler {
	return Handler{
		Refuse: func(w http.ResponseWriter, status int, message string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
		},
		Respond: func(w http.ResponseWriter, status int, data any) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(data)
		},
		Log: func(string, ...any) {},
	}
}

func TestACometIsServedWithItsDigestAndAskedForByIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	kept := filepath.Join(home, ".qntx", "comets", "abcd-nl", "grove", "x86_64-linux", "7", "libground_sand.so")
	require.NoError(t, os.MkdirAll(filepath.Dir(kept), 0o755))
	require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o644))
	require.NoError(t, KeepDigest(kept))
	sum := sha256.Sum256([]byte("a comet"))
	h := handler()

	got := httptest.NewRecorder()
	h.ServeHTTP(got, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/grove/x86_64-linux/7", nil))
	assert.Equal(t, http.StatusOK, got.Code)
	assert.Equal(t, hex.EncodeToString(sum[:]), got.Header().Get("X-Comet-Digest"))
	assert.Equal(t, "a comet", got.Body.String())

	// Asking whether it moved costs a header, not the sand.
	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/comet/abcd-nl/grove/x86_64-linux/7", nil))
	assert.Equal(t, http.StatusOK, head.Code)
	assert.Equal(t, hex.EncodeToString(sum[:]), head.Header().Get("X-Comet-Digest"))
	assert.Empty(t, head.Body.String())

	none := httptest.NewRecorder()
	h.ServeHTTP(none, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/other/x86_64-linux/7", nil))
	assert.Equal(t, http.StatusNotFound, none.Code)

	// An engine of another ABI is told which ABIs are kept, and a read that names none is refused.
	other := httptest.NewRecorder()
	h.ServeHTTP(other, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/grove/x86_64-linux/8?digest", nil))
	assert.Equal(t, http.StatusNotFound, other.Code)
	assert.Contains(t, other.Body.String(), "kept for engine ABI 7")
	bare := httptest.NewRecorder()
	h.ServeHTTP(bare, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/grove/x86_64-linux", nil))
	assert.Equal(t, http.StatusNotFound, bare.Code)
	assert.Contains(t, bare.Body.String(), "engine ABI")

	// The digest alone: what ground's sky asks every five seconds.
	digest := httptest.NewRecorder()
	h.ServeHTTP(digest, httptest.NewRequest(http.MethodGet, "/comet/abcd-nl/grove/x86_64-linux/7?digest", nil))
	assert.Equal(t, http.StatusOK, digest.Code)
	assert.JSONEq(t, `{"digest":"`+hex.EncodeToString(sum[:])+`"}`, digest.Body.String())
}

// A build makes the sand of ground's main, whatever ABI it is: a POST that names one is refused.
func TestABuildIsAskedWithoutAnABI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got := httptest.NewRecorder()
	handler().ServeHTTP(got, httptest.NewRequest(http.MethodPost, "/comet/abcd-nl/grove/x86_64-linux/7?machine=abcd-nl/l", nil))
	assert.Equal(t, http.StatusUnprocessableEntity, got.Code)
	assert.Contains(t, got.Body.String(), "POST /comet/abcd-nl/grove/x86_64-linux")
}

// A binary of ground's is built by the make goal that builds it.
func TestEachOfGroundsBinariesIsBuiltByItsGoal(t *testing.T) {
	assert.Equal(t, "build", goalOf("ground"))
	assert.Equal(t, "ground_hook", goalOf("ground_hook"))
}

// "only the compiled controls being their own thing shipped to the user"
// A sand for another platform is compiled as ground's `make sand` compiles it, and linked with
// nothing: it imports no symbol, so no stubs and no libraries.
func TestASandForAnotherPlatformIsCompiledAloneAndLinkedWithNothing(t *testing.T) {
	assert.Equal(t, []string{"ldc2", "-betterC", "--lowmem", "-mtriple=arm64-apple-macos14.0", "-O", "-release",
		"-fvisibility=hidden", "--function-sections", "--data-sections",
		"-I=source", "-J=.", "-i=builtin_sand", "-i=sandview", "-c", "--singleobj", "-of=build/sand.o", "bin/sand.d"},
		sandCompiled("aarch64-darwin"))
	assert.Contains(t, sandCompiled("x86_64-linux"), "-relocation-model=pic")
	assert.NotContains(t, sandCompiled("aarch64-darwin"), "-relocation-model=pic")

	assert.Equal(t, []string{"ld64.lld", "-dylib", "-arch", "arm64", "-platform_version", "macos", "14.0", "14.0",
		"-dead_strip", "-o", "build/libground_sand.dylib", "build/sand.o"}, sandLinked("aarch64-darwin"))
	assert.Equal(t, []string{"ld.lld", "-shared", "--gc-sections", "-z", "defs", "-o", "build/libground_sand.so", "build/sand.o"},
		sandLinked("aarch64-linux"))
	assert.Equal(t, "libground_sand.dylib", SandFile("x86_64-darwin"))
	assert.Equal(t, "libground_sand.so", SandFile("x86_64-linux"))
}

// "A person configures controls; the model is on the enforced side of them"
func TestACometCarriesTheControlsOfTheMachineAPersonNamed(t *testing.T) {
	named, err := MachineOf("abcd-nl/l")
	require.NoError(t, err)
	assert.Equal(t, "abcd-nl/l", named)
	for _, refused := range []string{"", "abcd-nl", "abcd-nl/", "/l", "abcd-nl/l/more", "../l", "abcd-nl/l?x"} {
		_, err := MachineOf(refused)
		assert.Error(t, err, refused)
	}

	machine := t.TempDir()
	_, err = machineSand(machine, "abcd-nl/l")
	assert.ErrorContains(t, err, "holds no controls/qntx.pbt")
	require.NoError(t, os.MkdirAll(filepath.Join(machine, "controls"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(machine, "controls", "qntx.pbt"), []byte("qntx {\n  url: \"http://127.0.0.1:8780\"\n}\n"), 0o644))
	sand, err := machineSand(machine, "abcd-nl/l")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"machine-qntx.pbt": "qntx {\n  url: \"http://127.0.0.1:8780\"\n}\n"}, sand)

	// A comet that does not say which machine it carries is not rebuilt as if it carried none.
	dir := t.TempDir()
	kept := filepath.Join(dir, "abcd-nl", "grove", "x86_64-linux", "7", "libground_sand.so")
	require.NoError(t, os.MkdirAll(filepath.Dir(kept), 0o755))
	require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o644))
	_, err = Names(dir)
	assert.ErrorContains(t, err, "does not say which machine")
}

func TestAPushRebuildsTheCometsItMoves(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"abcd-nl/grove/x86_64-linux/7/libground_sand.so", "abcd-nl/grove/aarch64-darwin/7/libground_sand.dylib", "abcd-nl/other/x86_64-linux/7/libground_sand.so", "abcd-nl/other/x86_64-linux/6/libground_sand.so"} {
		kept := filepath.Join(dir, filepath.FromSlash(key))
		require.NoError(t, os.MkdirAll(filepath.Dir(kept), 0o755))
		require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o644))
		require.NoError(t, os.WriteFile(kept+".machine", []byte("abcd-nl/l"), 0o644))
	}
	kept, err := Names(dir)
	require.NoError(t, err)
	require.Len(t, kept, 4)

	grove := MovedBy(kept, "abcd-nl/grove", "master", "master")
	assert.ElementsMatch(t, []Name{
		{Owner: "abcd-nl", Repo: "grove", Platform: "x86_64-linux", ABI: "7", Machine: "abcd-nl/l"},
		{Owner: "abcd-nl", Repo: "grove", Platform: "aarch64-darwin", ABI: "7", Machine: "abcd-nl/l"},
	}, grove)
	// A branch that is not the project's default moves nothing.
	assert.Empty(t, MovedBy(kept, "abcd-nl/grove", "a-branch", "master"))
	// Ground's main moves every comet, once per repository and platform whatever ABIs it keeps.
	assert.Len(t, MovedBy(kept, "teranos/ground", "main", "main"), 3)
	assert.Empty(t, MovedBy(kept, "teranos/ground", "a-branch", "main"))
}

// "AS AN A2A AGENT, IT ALSO NEEDS TO SHACKLE ITSELF UNDER GROUND"
func TestAnAgentRunsUnderGroundOnceItsGroundAndSettingsStand(t *testing.T) {
	home := t.TempDir()
	assert.ErrorContains(t, Under(home), "its ground is not built")

	require.NoError(t, os.MkdirAll(filepath.Dir(AgentGround(home)), 0o755))
	require.NoError(t, os.WriteFile(AgentGround(home), []byte("a ground"), 0o755))
	// "ground_hook as the hooks binary"
	// The hooks are answered by ground_hook, sky and rituals by ground: an agent needs both.
	assert.ErrorContains(t, Under(home), "its ground is not built")
	require.NoError(t, os.WriteFile(AgentHook(home), []byte("a ground_hook"), 0o755))
	assert.ErrorContains(t, Under(home), "its ground is not built")
	require.NoError(t, os.WriteFile(AgentSky(home), []byte("a ground_sky"), 0o755))
	assert.ErrorContains(t, Under(home), "its settings are not written")

	// Settings held already keep what they hold; the hooks are ground's.
	require.NoError(t, os.MkdirAll(filepath.Join(home, "claude"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, "claude", "settings.json"), []byte(`{"model":"opus"}`), 0o600))
	require.NoError(t, AgentSettings(home))
	assert.NoError(t, Under(home))
	assert.Equal(t, filepath.Join(home, "bin")+":/usr/bin", AgentPath(home, "/usr/bin"))

	written, err := os.ReadFile(filepath.Join(home, "claude", "settings.json"))
	require.NoError(t, err)
	var settings struct {
		Model string `json:"model"`
		Hooks map[string][]struct {
			Hooks []struct {
				Command     string `json:"command"`
				Timeout     int    `json:"timeout"`
				AsyncRewake bool   `json:"asyncRewake"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(written, &settings))
	assert.Equal(t, "opus", settings.Model)
	assert.Equal(t, AgentHook(home), settings.Hooks["PreToolUse"][0].Hooks[0].Command)
	// Under -p a hook that does not end holds the turn, so the agent's sky is spawned apart.
	stop := settings.Hooks["Stop"][0].Hooks
	require.Len(t, stop, 2)
	assert.Equal(t, `exec 3<&0; `+AgentSky(home)+` "$PWD" <&3 >/dev/null 2>&1 &`, stop[1].Command)
	assert.Zero(t, stop[1].Timeout)
	assert.False(t, stop[1].AsyncRewake)

	// Spawned apart, sky still hears the event it was spawned on: with no session on its stdin it
	// took whichever session's claim was listed first, another session's on k (2026-10-26).
	heard := filepath.Join(home, "heard")
	require.NoError(t, os.WriteFile(AgentSky(home), []byte("#!/bin/sh\ncat > "+heard+".part && mv "+heard+".part "+heard+"\n"), 0o755))
	run := exec.Command("sh", "-c", stop[1].Command)
	run.Dir = home
	run.Stdin = strings.NewReader(`{"session_id":"sess-sky","hook_event_name":"Stop"}`)
	require.NoError(t, run.Run())
	require.Eventually(t, func() bool { _, err := os.Stat(heard); return err == nil }, 5*time.Second, 20*time.Millisecond)
	got, err := os.ReadFile(heard)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"session_id":"sess-sky"`)
}

// A ritual starts its own agent with claude --bg in a tree under the agent's work, which Claude Code
// refuses unless a directory above it is trusted; what the configuration holds already is kept.
func TestAnAgentsWorkIsTrustedForTheRitualsItStarts(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "claude"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, "claude", ".claude.json"),
		[]byte(`{"userID":"kept","projects":{"/elsewhere":{"hasTrustDialogAccepted":false}}}`), 0o600))
	// A checkout is trusted by its own name: on q a trusted work did not reach abcd-nl/grove.
	require.NoError(t, os.MkdirAll(filepath.Join(home, "work", "abcd-nl", "grove", ".git"), 0o700))
	require.NoError(t, AgentSettings(home))

	written, err := os.ReadFile(filepath.Join(home, "claude", ".claude.json"))
	require.NoError(t, err)
	var config struct {
		UserID   string `json:"userID"`
		Projects map[string]struct {
			Trusted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	require.NoError(t, json.Unmarshal(written, &config))
	work, err := filepath.EvalSymlinks(filepath.Join(home, "work"))
	require.NoError(t, err)
	assert.True(t, config.Projects[work].Trusted)
	assert.True(t, config.Projects[filepath.Join(work, "abcd-nl", "grove")].Trusted)
	assert.False(t, config.Projects["/elsewhere"].Trusted)
	assert.Equal(t, "kept", config.UserID)
}

// "That is how the node knows which ground a session runs, and so which comet landed."
func TestACometsRowsSayWhichRevsItWasBuiltFrom(t *testing.T) {
	assert.Equal(t, "comet a798533 sbvh-nl/grove@2f81471\n", Version("a798533c1f2e3d4c5b6a7980", "sbvh-nl/grove@"+short("2f8147100aa")))
}

func TestTheAgentsSandIsItsWorkAndTheNode(t *testing.T) {
	assert.Equal(t, "project {\n  path: \"/a/work\"\n}\n\nqntx {\n  url: \"http://127.0.0.1:8772\"\n}\n", AgentDeclaration("/a/work", "http://127.0.0.1:8772"))
}

func TestACometsDigestIsWrittenOnceBesideIt(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "ground")
	require.NoError(t, os.WriteFile(kept, []byte("a comet"), 0o755))
	sum := sha256.Sum256([]byte("a comet"))

	require.NoError(t, KeepDigest(kept))
	written, err := os.ReadFile(kept + ".digest")
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(sum[:]), string(written))

	// Read from beside it, not hashed again.
	require.NoError(t, os.WriteFile(kept+".digest", []byte("said"), 0o644))
	said, err := Digest(kept)
	require.NoError(t, err)
	assert.Equal(t, "said", said)
}

func TestCometSandIsGroundsControlsAndTheProjectsAlone(t *testing.T) {
	// One project block naming the checkout: wind folds that project's controls and nothing of anyone's.
	// Its origin is the repository wherever it is checked out on the machine the comet lands on.
	assert.Equal(t, "project {\n  path: \"/b/grove\"\n  origin: \"abcd-nl/grove\"\n}\n", Declaration("/b/grove", "abcd-nl", "grove"))
}
