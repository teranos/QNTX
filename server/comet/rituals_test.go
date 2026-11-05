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
