package sigil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
)

// gateAdmitted is how the test gate says who it let in, read by the sigil the
// way an admission is read in production: off the context the gate hands on.
type gateAdmitted struct{}

func breakdown() *protocol.Sigil {
	return &protocol.Sigil{
		Name: "metrics",
		Takes: []*protocol.Param{
			{Name: "market", Says: "The market.", Required: true},
			{Name: "type", Says: "What to group by.", Required: true, OneOf: []string{"page", "event"}},
			{Name: "limit", Says: "How many.", Kind: Count},
		},
		Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/staands/metrics"},
	}
}

// Asking a sigil is one thing on every surface: the gate first, then what
// arrived is read and refused, then the function answers. There is no way to
// an answer around the gate, because the gate is inside the asking.
func TestAskingIsGatedThenReadThenAnswered(t *testing.T) {
	var sentToIt Sent
	var askedBy any
	answer := func(ctx context.Context, sent Sent) (any, *protocol.Refusal) {
		sentToIt, askedBy = sent, ctx.Value(gateAdmitted{})
		return map[string]any{"type": sent["type"]}, nil
	}
	caller := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	caller.Header.Set("Authorization", "Bearer qntx_caller")
	reaching := auth.Also(auth.LevelSuper)

	var gatedRoute, gatedCredential string
	var gatedReach auth.Reach
	admits := func(route string, re auth.Reach, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			gatedRoute, gatedReach, gatedCredential = route, re, r.Header.Get("Authorization")
			next(w, r.WithContext(context.WithValue(r.Context(), gateAdmitted{}, "tim")))
		}
	}
	asking := Asking{
		Surface: "mcp", Signum: "staands", Sigil: breakdown(), Answer: answer,
		Gate: admits, Reaching: reaching, Caller: caller,
	}

	asked := asking.Ask(context.Background(), map[string]any{"market": "clean", "type": "page", "limit": 5})
	require.Nil(t, asked.Refusal)
	require.Nil(t, asked.Rejected)
	require.Equal(t, map[string]any{"type": "page"}, asked.Answer)
	require.Equal(t, Sent{"market": "clean", "type": "page", "limit": "5"}, sentToIt)
	require.Equal(t, "tim", askedBy, "the function was not handed who the gate admitted")
	require.Equal(t, "mcp:staands:metrics", gatedRoute, "the gate was not told which sigil over which surface")
	require.Equal(t, reaching.Beyond(), gatedReach.Beyond())
	require.Equal(t, "Bearer qntx_caller", gatedCredential)

	// What the sigil refuses is refused after the gate and before the function.
	sentToIt = nil
	asked = asking.Ask(context.Background(), map[string]any{"market": "clean"})
	require.NotNil(t, asked.Refusal)
	require.Equal(t, "type", asked.Refusal.GetParam())
	require.Nil(t, sentToIt, "the function answered something the sigil refuses")

	asked = asking.Ask(context.Background(), map[string]any{"market": "clean", "type": "page", "limit": "many"})
	require.NotNil(t, asked.Refusal)
	require.Equal(t, "limit", asked.Refusal.GetParam())
}

// Somebody the gate turns away reaches nothing: not the function, not the
// sigil's refusals. What the gate said is carried back whole, because it is
// the gate's answer and each surface hands it on in its own form.
func TestSomebodyTheGateTurnsAwayReachesNothing(t *testing.T) {
	answered := false
	turnsAway := func(string, auth.Reach, http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="/.well-known/oauth-protected-resource"`)
			http.Error(w, "no session", http.StatusUnauthorized)
		}
	}
	asking := Asking{
		Surface: "http", Signum: "staands", Sigil: breakdown(),
		Answer: func(context.Context, Sent) (any, *protocol.Refusal) { answered = true; return nil, nil },
		Gate:   turnsAway, Reaching: auth.Also(auth.LevelSuper),
		Caller: httptest.NewRequest(http.MethodGet, "/api/staands/metrics", nil),
	}

	asked := asking.Ask(context.Background(), map[string]any{})
	require.False(t, answered)
	require.Nil(t, asked.Refusal, "the gate's no was turned into the sigil's")
	require.NotNil(t, asked.Rejected)
	require.Equal(t, http.StatusUnauthorized, asked.Rejected.Status)
	require.Contains(t, asked.Rejected.Body, "no session")
	require.Equal(t, `Bearer resource_metadata="/.well-known/oauth-protected-resource"`, asked.Rejected.Header.Get("WWW-Authenticate"))
}

// A sigil the lines serve to anyone is asked without the gate.
func TestWhatIsServedToAnyoneIsNotGated(t *testing.T) {
	gated := false
	gate := func(_ string, _ auth.Reach, next http.HandlerFunc) http.HandlerFunc {
		gated = true
		return next
	}
	asking := Asking{
		Surface: "http", Signum: "staands", Sigil: breakdown(),
		Answer: func(_ context.Context, sent Sent) (any, *protocol.Refusal) { return sent["type"], nil },
		Gate:   gate, Anyone: true,
		Caller: httptest.NewRequest(http.MethodGet, "/api/staands/metrics", nil),
	}
	asked := asking.Ask(context.Background(), map[string]any{"market": "clean", "type": "event"})
	require.False(t, gated)
	require.Equal(t, "event", asked.Answer)
}
