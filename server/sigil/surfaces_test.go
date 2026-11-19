package sigil

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// A sigil naming no surfaces is asked over every one; one naming some, over
// those alone. Not everything needs to be a sigil an LLM calls.
func TestASigilIsAskedOverTheSurfacesItNames(t *testing.T) {
	every := &protocol.Sigil{Name: "every"}
	assert.True(t, On(every, "mcp"))
	assert.True(t, On(every, "subsystem"))

	some := &protocol.Sigil{Name: "some", Surfaces: []string{"http", "subsystem"}}
	assert.True(t, On(some, "subsystem"))
	assert.False(t, On(some, "mcp"))
}

// Within asks a sigil with no request and no gate, and reads what arrived as
// every surface does: a list as its JSON, a missing param refused.
func TestWithinReadsWhatArrivedAndAnswers(t *testing.T) {
	held := &protocol.Sigil{Name: "add", Takes: []*protocol.Param{
		{Name: "index", Required: true, Says: "the index"},
		{Name: "ids", Required: true, Kind: List, Says: "the ids"},
	}}
	var got Sent
	answer := func(ctx context.Context, sent Sent) (any, *protocol.Refusal) {
		got = sent
		assert.Nil(t, Caller(ctx), "no request carries a subsystem's asking")
		return "answered", nil
	}

	answered, refusal := Within(context.Background(), held, answer, map[string]any{"index": "i", "ids": []string{"a", "b"}})
	require.Nil(t, refusal)
	assert.Equal(t, "answered", answered)
	assert.Equal(t, `["a","b"]`, got["ids"])

	_, refusal = Within(context.Background(), held, answer, map[string]any{"ids": []string{"a"}})
	require.NotNil(t, refusal)
	assert.Equal(t, Missing, refusal.GetWhy())

	_, refusal = Within(context.Background(), held, answer, map[string]any{"index": "i", "ids": "a"})
	require.NotNil(t, refusal)
	assert.Equal(t, Invalid, refusal.GetWhy())
}
