package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
)

// sigilOf is one sigil of a signum, by name, with what answers it.
func sigilOf(t *testing.T, signum sigil.Signum, name string) heldBy {
	t.Helper()
	for _, found := range signum.GetSigils() {
		if found.GetName() == name {
			return heldBy{signum: signum.GetName(), sigil: found, answer: signum.Answers[name]}
		}
	}
	t.Fatalf("%s holds no sigil %s", signum.GetName(), name)
	return heldBy{}
}

// Staands is the first signum (ADR-039): six things the node does with a
// stand, each its own sigil, and all of them said in full.
func TestStaandsIsASignum(t *testing.T) {
	s, _, _ := standServer(t, "clean")
	staands := s.staandsSignum()
	require.NoError(t, staands.Check())

	var held []string
	for _, found := range staands.GetSigils() {
		held = append(held, found.GetName())
	}
	require.Equal(t, []string{"list", "create", "take-down", "metrics", "activity", "visits"}, held)
}

// A connector asked for a breakdown five times and was refused five times:
// nothing said it takes market, slug and type, and the refusal listed the
// values of type without saying type. The sigil says all three, and refuses by
// naming the one that is wrong.
func TestABreakdownIsRefusedByNamingType(t *testing.T) {
	s, _, _ := standServer(t, "clean")
	metrics := sigilOf(t, s.staandsSignum(), "metrics").sigil

	missing := sigil.Refuses(metrics, sigil.Sent{"market": "clean", "slug": "boutique"})
	require.NotNil(t, missing)
	require.Equal(t, "type", missing.GetParam())
	require.True(t, strings.HasPrefix(missing.GetSays(), "metrics needs type: "), missing.GetSays())

	wrong := sigil.Refuses(metrics, sigil.Sent{"market": "clean", "slug": "boutique", "type": "eyecolour"})
	require.NotNil(t, wrong)
	require.Equal(t, sigil.NotOneOf, wrong.GetWhy())
	require.Equal(t, "type", wrong.GetParam())

	// What type takes is what a stand answers by, from the one list both read:
	// a dimension added to a stand is one the sigil says, with nothing to keep
	// in step.
	var takes []string
	for _, param := range metrics.GetTakes() {
		if param.GetName() == "type" {
			takes = param.GetOneOf()
		}
	}
	require.Equal(t, staandDimensions(), takes)
	for _, dim := range takes {
		require.True(t, staandKnownDimension(dim), dim)
	}
	require.False(t, staandKnownDimension("eyecolour"))
}

// What a sigil gives is said beside the code that writes the answer, so each
// real answer is held to it. A field added to an answer and not to its sigil
// fails here by name.
func TestAStaandAnswerIsWhatItsSigilGives(t *testing.T) {
	s, sys, _ := standServer(t, "clean")
	define(t, sys, "clean", "boutique", time.Now())
	fire(s, "/s/clean/boutique?page=/&v=WHO-000001&s=SIT-000001", "https://clean.example/")
	fire(s, "/s/clean/boutique?page=/prices&v=WHO-000001&s=SIT-000001", "https://clean.example/")

	staands := s.staandsSignum()
	for _, asked := range []struct {
		sigil string
		sent  sigil.Sent
	}{
		{sigil: "list"},
		{sigil: "metrics", sent: sigil.Sent{"market": "clean", "slug": "boutique", "type": "page"}},
		{sigil: "activity", sent: sigil.Sent{"market": "clean", "slug": "boutique"}},
		{sigil: "visits", sent: sigil.Sent{"market": "clean", "slug": "boutique"}},
		{sigil: "create", sent: sigil.Sent{"market": "clean", "slug": "second"}},
		{sigil: "take-down", sent: sigil.Sent{"market": "clean", "slug": "second"}},
	} {
		t.Run(asked.sigil, func(t *testing.T) {
			found := sigilOf(t, staands, asked.sigil)
			answer, refusal := found.answer(context.Background(), asked.sent)
			require.Nil(t, refusal)
			said, err := json.Marshal(answer)
			require.NoError(t, err)
			require.NoError(t, sigil.Holds(found.sigil, said), string(said))
		})
	}
}

// What is wrong with what was sent is refused by naming it, and a market the
// node does not serve is not found rather than a breakdown of nothing.
func TestAStaandRefusesInItsOwnTerms(t *testing.T) {
	s, _, _ := standServer(t, "clean")
	metrics := sigilOf(t, s.staandsSignum(), "metrics")

	for _, tc := range []struct {
		name    string
		arrived map[string]any
		why     string
		param   string
	}{
		{"a limit that is not a count", map[string]any{"market": "clean", "slug": "boutique", "type": "page", "limit": "many"}, sigil.Invalid, "limit"},
		{"a since that is not a time", map[string]any{"market": "clean", "slug": "boutique", "type": "page", "since": "whenever-ish-99"}, sigil.Invalid, "since"},
		{"a market nobody serves", map[string]any{"market": "nowhere", "slug": "boutique", "type": "page"}, sigil.NotFound, "market"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, refusal := askedOf(context.Background(), metrics.sigil, metrics.answer, tc.arrived)
			require.NotNil(t, refusal)
			require.Equal(t, tc.why, refusal.GetWhy())
			require.Equal(t, tc.param, refusal.GetParam())
		})
	}

	create := sigilOf(t, s.staandsSignum(), "create")
	_, refusal := create.answer(context.Background(), sigil.Sent{"market": "system", "slug": "home"})
	require.NotNil(t, refusal)
	require.Equal(t, "market", refusal.GetParam())
	require.Equal(t, http.StatusBadRequest, sigil.Status(refusal))
}
