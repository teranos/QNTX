package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func drafting(t *testing.T) *Handler {
	t.Helper()
	h := &Handler{logger: testLogger()}
	h.SetPublicOrigin("https://api.node.example")
	return h
}

// The block is the whole point: somebody asked for a namespace and the node
// says what the door onto it is, rather than leaving them to work it out.
func TestADraftIsABlockYouCanPaste(t *testing.T) {
	h := drafting(t)

	draft, err := h.DraftDoor("garden", []string{"https://portal.garden.test"}, "garden.test")
	require.NoError(t, err)

	assert.Equal(t, "[auth.door.garden]\nrp_id   = \"garden.test\"\norigins = [\"https://portal.garden.test\"]\n", draft.TOML)
}

// The redirect URI is where this node answers, and it is the string a
// provider's console asks to be told. Registering a different one is what makes
// a ceremony fail at the very end.
func TestADraftSaysWhatTheConsoleWillAskFor(t *testing.T) {
	h := drafting(t)

	draft, err := h.DraftDoor("garden", []string{"https://portal.garden.test"}, "")
	require.NoError(t, err)

	assert.Equal(t, "https://api.node.example/auth/binding/callback", draft.RedirectURI)
	assert.Contains(t, draft.ClientTOML, "[auth.door.garden.provider.google]")
	// The secret is a reference wherever it is written. am.toml ships as a
	// world-readable SSM String parameter.
	assert.Contains(t, draft.ClientTOML, "ssm:///q/garden/google/client-secret")
	assert.NotContains(t, draft.TOML, "client_secret")
}

// A door that is one hostname needs nobody to say what its rp id is: a host
// always covers itself.
func TestOneHostnameNeedsNoRPID(t *testing.T) {
	h := drafting(t)

	draft, err := h.DraftDoor("garden", []string{"https://portal.garden.test"}, "")
	require.NoError(t, err)

	assert.Equal(t, "portal.garden.test", draft.RPID)
}

// Two hostnames need the domain they share, and only the person who owns them
// can say which that is.
func TestSeveralHostnamesTakeTheDomainTheyShare(t *testing.T) {
	h := drafting(t)

	draft, err := h.DraftDoor("garden",
		[]string{"https://portal.garden.test", "https://app.garden.test"}, "garden.test")
	require.NoError(t, err)
	assert.Equal(t, "garden.test", draft.RPID)

	// Defaulted, the first origin's host does not reach the second, and the
	// browser is what would refuse it. Said here rather than at that moment.
	_, err = h.DraftDoor("garden",
		[]string{"https://portal.garden.test", "https://app.garden.test"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registrable domain suffix")
}

// A block that would be refused on paste is not a block worth handing over, so
// every rule SetDoors holds a door to is applied while drafting.
func TestADraftIsHeldToWhatSetDoorsHoldsADoorTo(t *testing.T) {
	h := drafting(t)

	_, err := h.DraftDoor("garden", []string{"https://portal.elsewhere.test"}, "garden.test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be reached")

	_, err = h.DraftDoor("garden", nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names nowhere")

	_, err = h.DraftDoor(NamespaceDefault, []string{"https://portal.garden.test"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth.rp_id is already the door")

	_, err = h.DraftDoor(NamespaceSystem, []string{"https://portal.garden.test"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nobody arrives at it")
}

// An origin is what a browser sends, and a bare hostname is not one. A door
// named after something that is not an origin never matches an arrival.
func TestAnOriginCarriesItsScheme(t *testing.T) {
	h := drafting(t)

	_, err := h.DraftDoor("garden", []string{"portal.garden.test"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not an origin")
}

// Refused rather than escaped. Escaping would hand back a block that reads
// differently than it parses, and a door is a handful of hostnames — anything
// in one that needs escaping is a mistake worth seeing.
func TestSomethingThatWouldNotSurviveTheBlockIsRefused(t *testing.T) {
	h := drafting(t)

	for _, name := range []string{
		"garden\"]\n[auth]\nroot_identities = [\"whoever",
		"gar den\n",
		"garden#comment",
		"",
	} {
		_, err := h.DraftDoor(name, []string{"https://portal.garden.test"}, "")
		assert.Error(t, err, "namespace %q was accepted into the block", name)
	}

	_, err := h.DraftDoor("garden", []string{"https://portal.garden.test\", \"https://anywhere.test"}, "")
	assert.Error(t, err, "an origin carrying a quote was accepted into the block")
}

// Nothing is applied. Asking what a door would be does not open one.
func TestDraftingOpensNothing(t *testing.T) {
	h := drafting(t)

	_, err := h.DraftDoor("garden", []string{"https://portal.garden.test"}, "")
	require.NoError(t, err)

	_, standing := h.doors.at("https://portal.garden.test")
	assert.False(t, standing, "drafting a door opened it")
}

// The block a draft says is the block SetDoors takes. Held here so the two
// cannot drift into a draft nobody can use.
func TestWhatIsDraftedIsWhatOpens(t *testing.T) {
	h := drafting(t)

	draft, err := h.DraftDoor("garden",
		[]string{"https://portal.garden.test", "https://app.garden.test"}, "garden.test")
	require.NoError(t, err)

	opening := handlerWithDoors(t)
	require.NoError(t, opening.SetDoors([]Door{{
		Namespace: draft.Namespace,
		RPID:      draft.RPID,
		Origins:   draft.Origins,
	}}))

	for _, origin := range draft.Origins {
		arrived, ok := opening.doors.at(origin)
		require.True(t, ok, origin)
		assert.Equal(t, "garden", arrived.namespace)
	}
	assert.True(t, strings.Contains(draft.TOML, draft.RPID))
}
