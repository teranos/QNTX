package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
)

func askedAs(level auth.Level) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "https://node.example/a2a/extendedAgentCard", nil)
	return r.WithContext(auth.WithAdmission(r.Context(), auth.Admitted(level)))
}

// A caller is shown only what they reach over A2A. A line that names no
// surface is about every surface, so SUPER is shown the signa its lines reach,
// staands among them, and not parity or reach, which are ROOT's alone. Calling
// A2A at all is /a2a/'s line, ROOT's alone (TestA2AIsServedToRootAlone).
func TestTheCardShowsWhatTheCallerReaches(t *testing.T) {
	srv, _ := pluginServingServer(t, "fake")
	names := func(card a2a.Card) []string {
		var named []string
		for _, skill := range card.Skills {
			named = append(named, skill.Name)
		}
		return named
	}

	root := srv.a2aCard(askedAs(auth.LevelRoot))
	for _, skill := range root.Skills {
		assert.Equal(t, skill.Name, skill.ID, "a skill's id is its signum")
	}
	for _, signum := range []string{"staands", "parity", "reach"} {
		assert.Contains(t, names(root), signum, "ROOT's card lacks "+signum)
	}
	// "make am the card, not a skill on it"
	assert.NotContains(t, names(root), amSignumName, "am is the card, not a skill on it")
	assert.Equal(t, "https://node.example/a2a", root.URL)
	mcpAt, _ := root.Extensions[0].Params["mcp"].(map[string]any)
	assert.Equal(t, "https://node.example/mcp", mcpAt["url"], "the node extension says where the MCP answers")
	assert.Equal(t, mcpProtocolVersion, mcpAt["protocolVersion"])

	super := srv.a2aCard(askedAs(auth.LevelSuper))
	assert.Contains(t, names(super), "staands", "SUPER's lines reach staands")
	for _, signum := range []string{"parity", "reach"} {
		assert.NotContains(t, names(super), signum, "SUPER was shown "+signum+", which is ROOT's alone")
	}
}

// The card is an lf.a2a.v1.AgentCard, and what it leaves empty that the spec
// requires is named: the node's name and description when am.toml says none.
// The modes are JSON, and staands, first among the signa, says what it is for.
func TestTheCardSaysWhatItLacks(t *testing.T) {
	srv, _ := pluginServingServer(t, "fake")
	card, err := srv.a2aCard(askedAs(auth.LevelRoot)).Message()
	require.NoError(t, err)

	missing := a2a.Missing(card)
	for _, want := range []string{"AgentCard.name", "AgentCard.description"} {
		assert.Contains(t, missing, want)
	}
	for _, said := range []string{
		"AgentCard.supported_interfaces", "AgentCard.capabilities", "AgentCard.version", "AgentCard.skills",
		"AgentCard.default_input_modes", "AgentCard.default_output_modes",
		"AgentCard.skills[0].id", "AgentCard.skills[0].name", "AgentCard.skills[0].description", "AgentCard.skills[0].tags",
	} {
		assert.NotContains(t, missing, said, said+" is on the card")
	}
	assert.False(t, slices.ContainsFunc(missing, func(m string) bool { return m == "AgentCard.supported_interfaces[0].url" }))

	// An interface is a way to speak A2A (§8.3.1): the node's MCP is not one.
	interfaces := card.Get(card.Descriptor().Fields().ByName("supported_interfaces")).List()
	for i := 0; i < interfaces.Len(); i++ {
		entry := interfaces.Get(i).Message()
		assert.Equal(t, "HTTP+JSON", entry.Get(entry.Descriptor().Fields().ByName("protocol_binding")).String(), "supported_interfaces[%d]", i)
	}
}

// "it lets agents figure out MCP surface amongst other things": the card names
// the MCP the node speaks, which is what a client asking for the newest is
// answered in. A go-sdk that speaks a newer one fails here, not on the card.
func TestTheCardNamesTheMCPTheNodeSpeaks(t *testing.T) {
	ctx := context.Background()
	server := servedForTest(t).mcpServerFor(httptest.NewRequest(http.MethodPost, "/mcp/", nil))
	clientSide, serverSide := mcp.NewInMemoryTransports()
	serving, err := server.Connect(ctx, serverSide, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serving.Close() })
	asking, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientSide, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = asking.Close() })

	assert.Equal(t, mcpProtocolVersion, asking.InitializeResult().ProtocolVersion)
}
