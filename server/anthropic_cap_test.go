package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// 「[Opus 5.5] What ROOT gives is handed to Anthropic in whole cents, read as text: no float rounds it.」
func TestACapIsHandedToAnthropicInWholeCents(t *testing.T) {
	for given, cents := range map[string]string{"200": "20000", "12.5": "1250", "12.50": "1250", "0.01": "1", "007": "700", " 30 ": "3000"} {
		got, err := centsOf(given)
		require.NoError(t, err, given)
		assert.Equal(t, cents, got, given)
	}
	for _, given := range []string{"", "0", "0.00", "-5", "1.234", "12.", ".5", "ten", "1,5"} {
		_, err := centsOf(given)
		assert.Error(t, err, given)
	}
}

// "what  is max cap? like set it in the ui and make default of 200"
// 「[Opus 5.5] Set none, the cap is the node's default; ROOT's is kept on the ANTHROPIC line,」
// 「[Opus 5.5] and the order kept beside it is left as it was.」
func TestRootSetsTheCapOnASessionAtAnthropicAndTheDefaultIs200(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	before := rootAsksAnthropic(t, s, "am", sigil.Sent{})
	assert.Equal(t, "200", before.GetCap())
	assert.Equal(t, anthropicCapCurrency, before.GetCapCurrency())
	assert.True(t, before.GetCapDefault())

	rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicConsole, "then": "true"})
	set := rootAsksAnthropic(t, s, "cap", sigil.Sent{"amount": "150"})
	assert.Equal(t, "150", set.GetCap())
	assert.False(t, set.GetCapDefault())
	assert.Equal(t, anthropicConsole, set.GetFirst(), "the order is kept with the cap")
	assert.True(t, set.GetThen())

	ordered := rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicSession})
	assert.Equal(t, "150", ordered.GetCap(), "the cap is kept with a new order")

	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = "https://example.org/root"
	asked := httptest.NewRequest(http.MethodPost, "/api/anthropic/cap", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
	for _, amount := range []string{"0", "ten", "1.234"} {
		_, refused := s.anthropicSignum().Answers["cap"](sigil.WithCaller(asked.Context(), asked), sigil.Sent{"amount": amount})
		require.NotNil(t, refused, amount)
		assert.Equal(t, "amount", refused.GetParam())
	}
	assert.Equal(t, "150", rootAsksAnthropic(t, s, "am", sigil.Sent{}).GetCap(), "a refused cap changes nothing")
}

// Qodo's review of #1519: once ROOT had set a cap, nothing took the node back
// to its default. An amount left out does.
func TestRootTakesTheCapBackToTheDefault(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicConsole, "then": "true"})
	rootAsksAnthropic(t, s, "cap", sigil.Sent{"amount": "150"})

	back := rootAsksAnthropic(t, s, "cap", sigil.Sent{})
	assert.Equal(t, anthropicDefaultCap, back.GetCap())
	assert.True(t, back.GetCapDefault())
	assert.Equal(t, anthropicConsole, back.GetFirst(), "the order is kept")
	assert.True(t, back.GetThen())
	again := rootAsksAnthropic(t, s, "cap", sigil.Sent{"amount": "  "})
	assert.True(t, again.GetCapDefault(), "an amount of nothing but spaces is left out too")
}
