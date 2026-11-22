package server

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
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
