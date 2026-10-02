package server

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
)

// am version and am node answer Go structs for their json tags, and proto
// declares their shape (ADR-006): the two are held to the same fields here.

func TestAmVersionIsTheShapeProtoDeclares(t *testing.T) {
	declared := fieldsOf(t, watcherProto, "VersionInfo")
	tagged := jsonNamesOf(t, version.Info{})
	if !slices.Equal(declared, tagged) {
		t.Errorf("VersionInfo declares %v, and version.Info is tagged %v", declared, tagged)
	}
}

func TestAmNodeIsTheShapeProtoDeclares(t *testing.T) {
	declared := fieldsOf(t, "../plugin/grpc/protocol/sigil.proto", "Node")
	tagged := jsonNamesOf(t, amNode{})
	if !slices.Equal(declared, tagged) {
		t.Errorf("Node declares %v, and amNode is tagged %v", declared, tagged)
	}
}

// am card is the A2A card the asker would be given, read through the pinned
// spec, and what it leaves empty that the spec requires.
func TestAmCardIsTheCardTheAskerWouldGet(t *testing.T) {
	srv, _ := pluginServingServer(t, "fake")
	signum := srv.amSignum()
	asked := askedAs(auth.LevelRoot)
	answer, refused := signum.Answers["card"](sigil.WithCaller(asked.Context(), asked), nil)
	require.Nil(t, refused)
	holds(t, signum, "card", answer)

	body, err := json.Marshal(answer)
	require.NoError(t, err)
	var got struct {
		Card struct {
			SupportedInterfaces []struct {
				URL             string `json:"url"`
				ProtocolBinding string `json:"protocolBinding"`
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"supportedInterfaces"`
			Skills []struct {
				Name string `json:"name"`
			} `json:"skills"`
		} `json:"card"`
		Missing []string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.Len(t, got.Card.SupportedInterfaces, 2)
	assert.Equal(t, "https://node.example/a2a", got.Card.SupportedInterfaces[0].URL)
	assert.Equal(t, "https://node.example/mcp", got.Card.SupportedInterfaces[1].URL)
	assert.Equal(t, "MCP", got.Card.SupportedInterfaces[1].ProtocolBinding)
	assert.Equal(t, mcpProtocolVersion, got.Card.SupportedInterfaces[1].ProtocolVersion)
	assert.NotEmpty(t, got.Card.Skills)
	assert.Contains(t, got.Missing, "AgentCard.name")
	assert.Contains(t, got.Missing, "AgentCard.skills[0].id")
}

func TestAmCardIsRootsAlone(t *testing.T) {
	compiled, err := reach.Reached()
	require.NoError(t, err)
	assert.Equal(t, []string{"ROOT"}, compiled["/am/card"])
}

// am node says no name it was not given, and lists the signa it serves.
func TestAmNodeGivesWhatItHas(t *testing.T) {
	s := &QNTXServer{}
	signum := s.amSignum()
	answer, refused := signum.Answers["node"](context.Background(), nil)
	if refused != nil {
		t.Fatalf("node refused: %s", refused.GetSays())
	}
	node := answer.(amNode)
	if node.Name != "" || node.Description != "" {
		t.Errorf("a node with no name was given %q, %q", node.Name, node.Description)
	}
	var names []string
	for _, held := range node.Signa {
		names = append(names, held.GetName())
	}
	if !slices.Contains(names, "am") || !slices.Contains(names, "parity") {
		t.Errorf("node lists %v", names)
	}
	holds(t, signum, "node", answer)
}
