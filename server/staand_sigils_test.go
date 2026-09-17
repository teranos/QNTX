package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
)

// sigilOf is one sigil of a signum, by name.
func sigilOf(t *testing.T, signum sigil.Signum, name string) sigil.Sigil {
	t.Helper()
	for _, found := range signum.Sigils {
		if found.Name == name {
			return found
		}
	}
	t.Fatalf("%s holds no sigil %s", signum.Name, name)
	return sigil.Sigil{}
}

// Staands is the first signum (ADR-039): six things the node does with a
// stand, each its own sigil, and all of them said in full.
func TestStaandsIsASignum(t *testing.T) {
	s, _, _ := standServer(t, "clean")
	staands := s.staandsSignum()
	require.NoError(t, staands.Check())

	var held []string
	for _, found := range staands.Sigils {
		held = append(held, found.Name)
	}
	require.Equal(t, []string{"list", "create", "take-down", "metrics", "activity", "visits"}, held)
}

// A connector asked for a breakdown five times and was refused five times:
// nothing said it takes market, slug and type, and the refusal listed the
// values of type without saying type. The sigil says all three, and refuses by
// naming the one that is wrong.
func TestABreakdownIsRefusedByNamingType(t *testing.T) {
	s, _, _ := standServer(t, "clean")
	metrics := sigilOf(t, s.staandsSignum(), "metrics")

	missing := metrics.Refuses(map[string]string{"market": "clean", "slug": "boutique"})
	require.NotNil(t, missing)
	require.Equal(t, "type", missing.Param)
	require.True(t, strings.HasPrefix(missing.Says, "metrics needs type: "), missing.Says)

	wrong := metrics.Refuses(map[string]string{"market": "clean", "slug": "boutique", "type": "eyecolour"})
	require.NotNil(t, wrong)
	require.Equal(t, sigil.NotOneOf, wrong.Why)
	require.Equal(t, "type", wrong.Param)

	// What type takes is what a stand answers by, from the one list both read:
	// a dimension added to a stand is one the sigil says, with nothing to keep
	// in step.
	var takes []string
	for _, param := range metrics.Takes {
		if param.Name == "type" {
			takes = param.OneOf
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
	of := "?market=clean&slug=boutique"
	for _, asked := range []struct {
		sigil string
		query string
		body  string
	}{
		{sigil: "list"},
		{sigil: "metrics", query: of + "&type=page"},
		{sigil: "activity", query: of},
		{sigil: "visits", query: of},
		{sigil: "create", body: `{"market":"clean","slug":"second"}`},
		{sigil: "take-down", query: "?market=clean&slug=second"},
	} {
		t.Run(asked.sigil, func(t *testing.T) {
			found := sigilOf(t, staands, asked.sigil)
			rec := httptest.NewRecorder()
			found.Answer(rec, httptest.NewRequest(found.HTTP.Method, found.HTTP.Path+asked.query, strings.NewReader(asked.body)))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.NoError(t, found.Holds(rec.Body.Bytes()), rec.Body.String())
		})
	}
}
