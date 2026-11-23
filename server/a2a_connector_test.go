package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// A connector's token speaks as itself, beside its client; a person's own
// session and an agent's own key do not.
func TestAConnectorSpeaksByItsOwnToken(t *testing.T) {
	cases := []struct {
		name      string
		admitted  auth.Admission
		connector bool
	}{
		{"claude.ai's connector", auth.Admission{Identity: "apple:x", ClientDID: "did:key:client", TokenDID: "did:key:token"}, true},
		{"an agent's own key", auth.Admission{ClientDID: "did:key:agent", TokenDID: "did:key:agent"}, false},
		{"a person's own session", auth.Admission{Identity: "apple:x"}, false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/a2a", nil)
		r = r.WithContext(auth.WithAdmission(r.Context(), c.admitted))
		did, connector := aConnectorSpeaks(r)
		if connector != c.connector {
			t.Errorf("%s: connector %v, want %v", c.name, connector, c.connector)
		}
		if c.connector && did != c.admitted.TokenDID {
			t.Errorf("%s: spoke as %q, want its token %q", c.name, did, c.admitted.TokenDID)
		}
	}
}

// What a person said and the node's signature go to laye over HTTPS, or to
// this machine alone (CodeRabbit on #1822).
func TestLayeIsReachedOverHTTPSOrOnThisMachine(t *testing.T) {
	for _, origin := range []string{"https://laye.ground.institute", "http://127.0.0.1:8790", "http://localhost:8790", "http://[::1]:8790"} {
		assert.NoError(t, layeReachable(origin), origin)
	}
	for _, origin := range []string{"http://laye.ground.institute", "ftp://laye.ground.institute", "laye.ground.institute"} {
		assert.Error(t, layeReachable(origin), origin)
	}
}

// What laye is handed for the Node Agent names it by key, whose it is, and
// that laye hands it back (laye-front ConnectorSaid: to_did, person, node,
// quiet); what is said to a name carries none of them.
func TestWhatLayeIsHandedForTheNodesAgentSaysItIsTheNodes(t *testing.T) {
	forNode, err := json.Marshal(layeSaid{To: "Lamplighter", ToDID: "did:key:zL", FromDID: "did:key:zA", From: "Alder", Says: "hi", Person: "google:1", Node: true})
	require.NoError(t, err)
	assert.JSONEq(t, `{"to":"Lamplighter","to_did":"did:key:zL","from_did":"did:key:zA","from":"Alder","says":"hi","context":"","person":"google:1","node":true}`, string(forNode))
	toAName, err := json.Marshal(layeSaid{To: "Cumulus", FromDID: "did:key:zA", From: "Alder", Says: "hi", Context: "c1"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"to":"Cumulus","from_did":"did:key:zA","from":"Alder","says":"hi","context":"c1"}`, string(toAName))
	quiet, err := json.Marshal(layeSaid{To: "Cumulus", ToDID: "did:key:zC", FromDID: "did:key:zN", From: "GitHub", Says: "merged", Quiet: true})
	require.NoError(t, err)
	assert.Contains(t, string(quiet), `"quiet":true`)
}
