package reach

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// The table the node ships with reads. It is parsed at startup, and a node
// whose table does not read does not start.
func TestTheTableReads(t *testing.T) {
	_, err := readReaches(reachTable)
	require.NoError(t, err)
}

// A path is case-sensitive and an attestation subject is not. The parser
// uppercases subjects, so a line has to give back the path as written.
func TestAPathKeepsItsCase(t *testing.T) {
	granted, err := readReaches(reachTable)
	require.NoError(t, err)

	_, said := granted["/.well-known/did.json"]
	assert.True(t, said, "the path came back uppercased")
}

// The two anchors: the endpoint that handed the node's config to anything
// holding a token, and the mint that let a public registration name its level.
func TestConfigAndMintingAreRootsAlone(t *testing.T) {
	granted, err := readReaches(reachTable)
	require.NoError(t, err)

	for _, path := range []string{"/api/config", "/auth/tokens", "/auth/tokens/"} {
		row, said := granted[path]
		require.True(t, said, path+" is granted to nobody at all")
		assert.False(t, row.anyone, path+" is served without asking who is calling")
		assert.Empty(t, row.reach.Beyond(), path+" lets in somebody besides ROOT")
	}
}

// SUPER owns namespaces and creates them (ADR-027).
func TestTheTableSaysWhoReachesTheNamespaces(t *testing.T) {
	granted, err := readReaches(reachTable)
	require.NoError(t, err)

	row, said := granted["/api/namespaces"]
	require.True(t, said, "/api/namespaces is granted to nobody at all")
	assert.False(t, row.anyone, "/api/namespaces is served without asking who is calling")
	assert.Equal(t, []auth.Level{auth.LevelSuper}, row.reach.Beyond(),
		"ROOT reaches everything; SUPER is the one this line has to name")
}

// Logging in cannot ask you to be logged in, and that is a line rather than an
// absence of one.
func TestTheCeremonyIsGrantedToAnyone(t *testing.T) {
	granted, err := readReaches(reachTable)
	require.NoError(t, err)

	for _, path := range []string{"/auth/login", "/auth/login/begin", "/auth/laye/verify"} {
		row, said := granted[path]
		require.True(t, said, path+" is granted to nobody at all")
		assert.True(t, row.anyone, path+" asks who is calling before letting them log in")
	}
}

// A line that does not read is a lie about what the node serves.
func TestALineThatDoesNotReadIsRefused(t *testing.T) {
	for _, line := range []string{
		"REACH is '/pond'",                                    // names no level
		"REACH is '/pond' of NOBODY",                          // not a level
		"GRANT is '/pond' of ROOT",                            // not this file's claim
		"REACH is '/pond' of ROOT\nREACH is '/pond' of SUPER", // twice
		"REACH is '/pond' of ROOT by somebody",                // by means nothing yet
	} {
		_, err := readReaches(line)
		assert.Error(t, err, line)
	}
}

// A grant naming a path nothing answers stops the node.
func TestAGrantToNowhereIsRefused(t *testing.T) {
	granted, err := readReaches("REACH is '/pond' of ROOT")
	require.NoError(t, err)

	_, _, err = build(granted, map[string]Answering{}, plainly())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/pond")
}

// A handler no line names is not served. Not refused — absent.
func TestAHandlerNoLineNamesIsAbsent(t *testing.T) {
	granted, err := readReaches("REACH is '/pond' of ROOT")
	require.NoError(t, err)

	answering := map[string]Answering{
		"/pond":        {Handler: func(http.ResponseWriter, *http.Request) {}},
		"/pond/keeper": {Handler: func(http.ResponseWriter, *http.Request) {}},
	}
	_, unreachable, err := build(granted, answering, plainly())
	require.NoError(t, err)

	assert.Equal(t, []string{"/pond/keeper"}, unreachable)
}

// plainly is the wrapping with nothing in it, so a test measures the grant and
// not the rate limiter.
func plainly() Wrapping {
	same := func(h http.HandlerFunc) http.HandlerFunc { return h }
	return Wrapping{
		Gate:     func(_ auth.Reach, h http.HandlerFunc) http.HandlerFunc { return h },
		Anyone:   same,
		Asked:    same,
		Upgraded: same,
	}
}
