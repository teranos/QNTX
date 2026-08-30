package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// handlerWithDoors builds a node whose own relying party is q.sbvh.nl — the
// door onto default — and gives it the doors named after it.
func handlerWithDoors(t *testing.T, doors ...Door) *Handler {
	t.Helper()

	h, err := New(
		qntxtest.CreateTestDB(t),
		"q.sbvh.nl",
		[]string{"https://q.sbvh.nl"},
		8770,
		8820,
		24,
		testLogger(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		nil,
		nil,
		false,
		[]string{mastodonAccount},
		nil,
	)
	require.NoError(t, err)
	require.NoError(t, h.SetDoors(doors))
	return h
}

func garden() Door {
	return Door{
		Namespace: "garden",
		RPID:      "garden.test",
		Origins:   []string{"https://portal.garden.test", "https://app.garden.test"},
	}
}

func arrivingFrom(origin string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/auth/register/begin", nil)
	r.Header.Set("Origin", origin)
	return r
}

// The origin a request came from is what selects the relying party. Where a
// request acts is a property of the caller, and a door names a namespace — so
// choosing one by anything the caller could type would be choosing a namespace
// by asking.
func TestTheOriginPicksTheDoor(t *testing.T) {
	h := handlerWithDoors(t, garden())

	arrived, ok := h.doorFor(arrivingFrom("https://portal.garden.test"))
	require.True(t, ok)
	assert.Equal(t, "garden", arrived.namespace)
	assert.Equal(t, "garden.test", arrived.rp.Config.RPID)
}

// One rp id can stand behind several hostnames, which is the browser's
// registrable-suffix rule and not one this node invents. Both reach the same
// door.
func TestADoorWithSeveralOriginsIsOneDoor(t *testing.T) {
	h := handlerWithDoors(t, garden())

	first, ok := h.doorFor(arrivingFrom("https://portal.garden.test"))
	require.True(t, ok)
	second, ok := h.doorFor(arrivingFrom("https://app.garden.test"))
	require.True(t, ok)

	assert.Equal(t, first.namespace, second.namespace)
	assert.Same(t, first.rp, second.rp)
}

// The relying party a node already has is the door onto default. Nothing
// moves and nothing is migrated.
func TestTheNodesOwnRelyingPartyIsTheDefaultDoor(t *testing.T) {
	h := handlerWithDoors(t, garden())

	arrived, ok := h.doorFor(arrivingFrom("https://q.sbvh.nl"))
	require.True(t, ok)
	assert.Equal(t, NamespaceDefault, arrived.namespace)
	assert.Equal(t, "q.sbvh.nl", arrived.rp.Config.RPID)
}

// An origin no door claims reaches no door. A ceremony that ran anyway would
// run against a relying party the browser never agreed to.
func TestAnOriginNoDoorClaimsReachesNoDoor(t *testing.T) {
	h := handlerWithDoors(t, garden())

	_, ok := h.doorFor(arrivingFrom("https://somewhere.else.example"))
	assert.False(t, ok)
}

// A browser asking for a page sends no Origin, and the host it asked is the
// same fact by another name.
func TestTheHostStandsInWhenThereIsNoOrigin(t *testing.T) {
	h := handlerWithDoors(t, garden())

	page := httptest.NewRequest(http.MethodGet, "https://portal.garden.test/auth/login", nil)
	page.Header.Del("Origin")

	arrived, ok := h.doorFor(page)
	require.True(t, ok)
	assert.Equal(t, "garden", arrived.namespace)
}

// Two namespaces cannot be reached through one origin: the second claim would
// silently decide which namespace a registration lands in.
func TestTwoDoorsCannotClaimOneOrigin(t *testing.T) {
	clash := Door{
		Namespace: "pond",
		RPID:      "garden.test",
		Origins:   []string{"https://portal.garden.test"},
	}

	h, err := New(
		qntxtest.CreateTestDB(t),
		"q.sbvh.nl",
		[]string{"https://q.sbvh.nl"},
		8770, 8820, 24,
		testLogger(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		nil, nil, false,
		[]string{mastodonAccount},
		nil,
	)
	require.NoError(t, err)

	err = h.SetDoors([]Door{garden(), clash})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https://portal.garden.test")
}

// A door whose rp id is not a registrable suffix of one of its origins is a
// ceremony every browser refuses. Saying so at startup beats finding out when
// somebody arrives.
func TestADoorWhoseRPIDDoesNotCoverItsOriginIsRefused(t *testing.T) {
	h, err := New(
		qntxtest.CreateTestDB(t),
		"q.sbvh.nl",
		[]string{"https://q.sbvh.nl"},
		8770, 8820, 24,
		testLogger(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		nil, nil, false,
		[]string{mastodonAccount},
		nil,
	)
	require.NoError(t, err)

	err = h.SetDoors([]Door{{
		Namespace: "garden",
		RPID:      "example.com",
		Origins:   []string{"https://portal.garden.test"},
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "example.com")
}
