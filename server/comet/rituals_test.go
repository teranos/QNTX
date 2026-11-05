package comet

// "another thing i want is that rituals run on the box from now on, not locally on my machine per se."

// "so it shoul"

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	errors "github.com/teranos/sacred-error"
)

// willow is a grove ritual as grove's controls/grove.pbt declares one.
const willow = "rites orchard {\n  START { eval: \"test -f WILLOW.md\" }\n}\n\nproject {\n  path: \"/sbvh-nl/grove\"\n\n  ritual willow {\n    orchard\n  }\n}\n"

// groveCheckout puts a grove checkout in the agent's work, its controls holding pbt.
func groveCheckout(t *testing.T, home string, pbt map[string]string) {
	t.Helper()
	controls := filepath.Join(home, "work", "sbvh-nl", "grove", "controls")
	require.NoError(t, os.MkdirAll(controls, 0o755))
	for name, text := range pbt {
		require.NoError(t, os.WriteFile(filepath.Join(controls, name), []byte(text), 0o644))
	}
}

// ADR-050, Not done: what a ritual needs stands in the Node Agent's grove checkout. The agent's
// sand declares the rituals of that checkout beside its work and its node.
func TestTheAgentsSandDeclaresTheRitualsOfItsGroveCheckout(t *testing.T) {
	home := t.TempDir()
	groveCheckout(t, home, map[string]string{"grove.pbt": willow, "moon.pbt": "project {\n  path: \"/sbvh-nl/grove\"\n  ritual moon {\n    orchard\n  }\n}\n", "README.md": "not a control"})

	sand, err := AgentSand(home, "http://127.0.0.1:8772")
	require.NoError(t, err)
	assert.Equal(t, AgentDeclaration(filepath.Join(home, "work"), "http://127.0.0.1:8772"), sand["comet.pbt"])
	assert.Equal(t, willow, sand["grove-grove.pbt"])
	assert.Contains(t, sand["grove-moon.pbt"], "ritual moon")
	assert.Len(t, sand, 3)
}

// An agent with no grove checkout declares no ritual: its sand is its work and its node.
func TestAnAgentWithoutAGroveCheckoutDeclaresNoRitual(t *testing.T) {
	home := t.TempDir()
	sand, err := AgentSand(home, "http://127.0.0.1:8772")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"comet.pbt": AgentDeclaration(filepath.Join(home, "work"), "http://127.0.0.1:8772")}, sand)
}

// agentNode builds whatever make is asked for, in the tree it is asked in.
type agentNode struct{ builds string }

func (agentNode) Latest(context.Context, Source) (string, error) { return "0123456789abcdef", nil }

func (agentNode) Fetch(context.Context, Source, string, string) error { return nil }

func (a agentNode) BuildsDir() (string, error) { return a.builds, nil }

func (agentNode) Run(_ context.Context, dir string, _ []string, name string, args ...string) error {
	if name != "nice" || !slices.Contains(args, "make") {
		return nil
	}
	goal := args[len(args)-1]
	if goal == "sand" {
		if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "build", "libground_sand.abi"), []byte("7\n"), 0o644); err != nil {
			return err
		}
		platform, err := Native()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "build", SandFile(platform)), []byte("the agent's sand"), 0o644)
	}
	if goal == "build" {
		goal = "ground"
	}
	return os.WriteFile(filepath.Join(dir, goal), []byte(goal), 0o755)
}

// The engine an agent runs is built from ground's floor alone (ground dub.json -d-version=Floor), and
// holds a local sand from ~/.local/share/ground/sand beside it (ground source/sand/load.d holdLanded).
// So the rituals are built into the agent's sand, and that sand is kept where its ground holds it.
func TestTheAgentsRitualsAreBuiltIntoTheSandItsGroundHolds(t *testing.T) {
	carried := t.TempDir()
	t.Setenv("HOME", carried)
	home := t.TempDir()
	groveCheckout(t, home, map[string]string{"grove.pbt": willow})
	builds := t.TempDir()

	require.NoError(t, Builder{Node: agentNode{builds: builds}, NixBin: "/nix/bin"}.BuildAgent(context.Background(), home, "http://127.0.0.1:8772"))

	// The sand the build compiled from declares the ritual.
	built, err := os.ReadFile(filepath.Join(builds, "comet-agent-"+filepath.Base(home), "ground", "controls", "local", "grove-grove.pbt"))
	require.NoError(t, err)
	assert.Contains(t, string(built), "ritual willow")

	platform, err := Native()
	require.NoError(t, err)
	held, err := os.ReadFile(filepath.Join(carried, ".local", "share", "ground", "sand", SandFile(platform)))
	require.NoError(t, err)
	assert.Equal(t, "the agent's sand", string(held))
	for _, binary := range []string{AgentGround(home), AgentHook(home), AgentSky(home), AgentDrive(home)} {
		assert.FileExists(t, binary)
	}
}

// An agent with no rituals keeps no sand: the one its ground holds is left as it is.
func TestAnAgentWithoutRitualsKeepsNoSand(t *testing.T) {
	carried := t.TempDir()
	t.Setenv("HOME", carried)
	home := t.TempDir()

	require.NoError(t, Builder{Node: agentNode{builds: t.TempDir()}, NixBin: "/nix/bin"}.BuildAgent(context.Background(), home, "http://127.0.0.1:8772"))

	platform, err := Native()
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(carried, ".local", "share", "ground", "sand", SandFile(platform)))
}

// buildAgent builds the agent in home on a node where every build succeeds.
func buildAgent(t *testing.T, home string) {
	t.Helper()
	require.NoError(t, Builder{Node: agentNode{builds: t.TempDir()}, NixBin: "/nix/bin"}.BuildAgent(context.Background(), home, "http://127.0.0.1:8772"))
}

// Qodo on #1535: an agent whose ground stands was never built again, so its rituals never came.
// Its sand stands only as the controls of its grove checkout are now: one that arrived, changed or
// grew since the last build does not stand, and is built again.
func TestAnAgentsSandStandsOnlyForTheControlsItWasBuiltFrom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()
	const url = "http://127.0.0.1:8772"
	require.NoError(t, SandStands(home, url), "an agent with no grove checkout needs no sand")

	groveCheckout(t, home, map[string]string{"grove.pbt": willow})
	require.Error(t, SandStands(home, url), "a grove checkout that arrived after the build")

	buildAgent(t, home)
	require.NoError(t, SandStands(home, url))

	groveCheckout(t, home, map[string]string{"grove.pbt": willow + "\n# changed\n"})
	require.Error(t, SandStands(home, url), "a control that changed")
	buildAgent(t, home)
	require.NoError(t, SandStands(home, url))

	groveCheckout(t, home, map[string]string{"moon.pbt": "project {\n  path: \"/sbvh-nl/grove\"\n}\n"})
	require.Error(t, SandStands(home, url), "a control that was added")
}

// Qodo on #1535: a build with no rituals left removes the sand this agent's build kept, and only that.
func TestRemovedRitualsLeaveNoSandTheAgentKept(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()
	groveCheckout(t, home, map[string]string{"grove.pbt": willow})
	buildAgent(t, home)
	platform, err := Native()
	require.NoError(t, err)
	held, err := AgentLocalSand(platform)
	require.NoError(t, err)
	require.FileExists(t, held)

	require.NoError(t, os.RemoveAll(AgentGrove(home)))
	require.Error(t, SandStands(home, "http://127.0.0.1:8772"), "a sand still held for rituals that are gone")
	buildAgent(t, home)
	assert.NoFileExists(t, held)
	require.NoError(t, SandStands(home, "http://127.0.0.1:8772"))
}

// Another agent's build, with no rituals of its own, leaves the sand it did not build.
func TestAnotherAgentsBuildLeavesTheSandItDidNotKeep(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	node, other := t.TempDir(), t.TempDir()
	groveCheckout(t, node, map[string]string{"grove.pbt": willow})
	buildAgent(t, node)
	buildAgent(t, other)

	platform, err := Native()
	require.NoError(t, err)
	held, err := AgentLocalSand(platform)
	require.NoError(t, err)
	assert.FileExists(t, held)
	require.NoError(t, SandStands(node, "http://127.0.0.1:8772"))
	require.NoError(t, SandStands(other, "http://127.0.0.1:8772"))
}

// failingSandNode builds every binary and fails the sand, as a grove control that does not parse would.
type failingSandNode struct{ agentNode }

func (f failingSandNode) Run(ctx context.Context, dir string, env []string, name string, args ...string) error {
	if name == "nice" && slices.Contains(args, "sand") {
		return errors.New("a grove control does not parse")
	}
	return f.agentNode.Run(ctx, dir, env, name, args...)
}

// Qodo on #1535: a build that failed on these controls is not started again by every turn. The
// controls changing starts one; until then, the agent's turns are said on the sand it holds.
func TestASandThatFailedToBuildIsNotBuiltAgainForTheSameControls(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home := t.TempDir()
	const url = "http://127.0.0.1:8772"
	groveCheckout(t, home, map[string]string{"grove.pbt": willow})
	require.True(t, BuildWanted(home, url), "a checkout whose rituals no sand holds")

	ended := make(chan struct{})
	Builder{Node: failingSandNode{agentNode{builds: t.TempDir()}}, NixBin: "/nix/bin"}.GroundAgent(context.Background(), home, url,
		func(string) error { return nil }, func(string, ...any) { close(ended) })
	<-ended
	require.Error(t, Under(home), "the failure is said")
	assert.False(t, BuildWanted(home, url), "the same controls, after their build failed")

	groveCheckout(t, home, map[string]string{"grove.pbt": willow + "\n# fixed\n"})
	assert.True(t, BuildWanted(home, url), "controls that changed since the build failed")
}
