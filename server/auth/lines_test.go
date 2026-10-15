package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The primary writes the lines; a peer holds the ones about the namespaces it
// serves, and keeps them while the primary is down.

// primaryWithLines is a primary serving its routes, holding a grant in Ground,
// in pond and in clean, with a peer declared serving Ground and pond;
// and that peer.
func primaryWithLines(t *testing.T) (served *httptest.Server, peer *Handler) {
	t.Helper()
	primary, _, _ := authorizingHandler(t)
	key, did := otherNode(t)
	require.NoError(t, primary.DeclarePeer(peerName, did, []string{"Ground", "pond"}, elsewhereOrigin, elsewhereOrigin))
	primary.SetRoleReader(&memRoles{
		lines: map[string][]RoleLine{
			"Ground": {{Routes: []string{"GroundToken"}, Roles: []string{"GROUND"}, Granted: true, Actor: mastodonAccount, At: at(1)}},
			"pond":   {{Routes: []string{"sprinkler"}, Roles: []string{"SPRINKLER"}, Granted: true, Actor: mastodonAccount, At: at(1)}},
			"clean":  {{Routes: []string{"clean-datapunt"}, Roles: []string{"DATAPUNT"}, Granted: true, Actor: mastodonAccount, At: at(1)}},
		},
		words: []WordLine{
			{Write: true, Words: []string{"*"}, Roles: []string{"GROUND"}, Actor: mastodonAccount, At: at(2)},
			{Write: true, Words: []string{"sprinkler:read"}, Roles: []string{"SPRINKLER"}, Actor: mastodonAccount, At: at(2)},
			{Write: true, Words: []string{"datapunt:observed"}, Roles: []string{"DATAPUNT"}, Actor: mastodonAccount, At: at(2)},
		},
	})
	mux := http.NewServeMux()
	for path, handle := range primary.Routes() {
		mux.HandleFunc(path, handle)
	}
	served = httptest.NewServer(mux)
	t.Cleanup(served.Close)
	primary.configuredOrigin = served.URL

	peer = handlerWithDoors(t)
	peer.nodeKey = key
	require.NoError(t, peer.SetPrimary(served.URL))
	return served, peer
}

func TestAPeerHoldsTheLinesAboutTheNamespacesItServes(t *testing.T) {
	served, peer := primaryWithLines(t)
	held, fromThePrimary := peer.LinesFromThePrimary()
	require.True(t, fromThePrimary)
	_, err := held.RoleLines("Ground")
	assert.Error(t, err, "lines were answered before the primary said any")

	require.NoError(t, held.Refresh(time.Now()))
	peer.SetRoleReader(held)
	assert.Equal(t, []string{"GROUND"}, peer.RolesOfToken("GroundToken", "Ground"))
	assert.Equal(t, []string{"*"}, peer.WordsOf([]string{"GROUND"}).Write)
	assert.Equal(t, []string{"SPRINKLER"}, peer.RolesOfToken("sprinkler", "pond"))
	assert.Equal(t, []string{"sprinkler:read"}, peer.WordsOf([]string{"SPRINKLER"}).Write)
	assert.Empty(t, peer.RolesOfToken("sprinkler", "Ground"), "a grant in pond held in Ground")
	assert.Empty(t, peer.RolesOfToken("clean-datapunt", "clean"), "a namespace the peer does not serve came across")
	assert.Empty(t, peer.WordsOf([]string{"DATAPUNT"}).Write, "a role nobody holds here came across")

	served.Close()
	assert.Error(t, held.Refresh(time.Now()))
	assert.Equal(t, []string{"GROUND"}, peer.RolesOfToken("GroundToken", "Ground"), "the primary going down took the lines with it")
}

// A node with no primary keeps its own lines.
func TestANodeWithNoPrimaryHoldsNoPrimarysLines(t *testing.T) {
	_, fromThePrimary := handlerWithDoors(t).LinesFromThePrimary()
	assert.False(t, fromThePrimary)
}
