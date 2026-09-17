package reach

import (
	"net/http"
	"net/http/httptest"
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

// "similar to ROOT yes, but SUPER can only list and read". The table lets
// SUPER onto the token and User routes and nobody else; that a token mints,
// revokes and switches nothing is the handler's session gate, held by the
// auth package's own tests.
func TestSuperReachesTokensAndUsers(t *testing.T) {
	granted, err := readReaches(reachTable)
	require.NoError(t, err)

	for _, path := range []string{"/auth/tokens", "/auth/tokens/", "/auth/users", "/auth/users/"} {
		row, said := granted[path]
		require.True(t, said, path+" is granted to nobody at all")
		assert.False(t, row.anyone, path+" is served without asking who is calling")
		assert.Equal(t, []auth.Level{auth.LevelSuper}, row.reach.Beyond(), path+" lets in the wrong levels besides ROOT")
	}
}

// SUPER owns namespaces and creates them (ADR-027).
func TestTheTableSaysWhoReachesTheNamespaces(t *testing.T) {
	granted, err := readReaches(reachTable)
	require.NoError(t, err)

	// The list and the making of one, then the switch on one and its ending.
	// Both are named, so dropping the second is a failing test rather than a
	// route nobody reaches.
	for _, path := range []string{"/api/namespaces", "/api/namespaces/"} {
		row, said := granted[path]
		require.True(t, said, path+" is granted to nobody at all")
		assert.False(t, row.anyone, path+" is served without asking who is calling")
		assert.Equal(t, []auth.Level{auth.LevelSuper}, row.reach.Beyond(),
			"ROOT reaches everything; SUPER is the one this line has to name")
	}
}

// A SUPER session's browser saves which windows it minimized, and was refused.
// "add it"
func TestSuperReachesMinimizedWindows(t *testing.T) {
	granted, err := readReaches(reachTable)
	require.NoError(t, err)

	for _, path := range []string{"/api/canvas/minimized-windows", "/api/canvas/minimized-windows/"} {
		row, said := granted[path]
		require.True(t, said, path+" is granted to nobody at all")
		assert.False(t, row.anyone, path+" is served without asking who is calling")
		assert.Equal(t, []auth.Level{auth.LevelSuper}, row.reach.Beyond(), path+" lets in the wrong levels besides ROOT")
	}
}

// Emptying default is the one place data leaves, so SUPER reaching the rest of
// the namespace routes must not carry it here.
func TestTheTableKeepsNukingToRoot(t *testing.T) {
	granted, err := readReaches(reachTable)
	require.NoError(t, err)

	row, said := granted["/api/namespaces/default/nuke"]
	require.True(t, said, "nuking is granted to nobody at all")
	assert.False(t, row.anyone, "nuking is served without asking who is calling")
	assert.Empty(t, row.reach.Beyond(),
		"ROOT reaches everything; naming anyone else here hands them the one place data leaves")
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

// Whoever is logged in may see the User the node resolved them to, and a
// stranger gets the table's refusal rather than a 200 with nothing in it.
func TestWhoeverIsLoggedInReachesTheirOwnUser(t *testing.T) {
	granted, err := readReaches(reachTable)
	require.NoError(t, err)

	row, said := granted["/i/"]
	require.True(t, said, "/i/ is granted to nobody at all")
	assert.False(t, row.anyone, "/i/ answers a stranger")
	assert.ElementsMatch(t,
		[]auth.Level{auth.LevelSuper, auth.LevelToken, auth.LevelAttestor, auth.LevelPublicRegistration},
		row.reach.Beyond(),
		"ROOT reaches everything; every other rung that logs in has to be named")
}

// A connector's token is the person, and ROOT is the only person the MCP
// surface is named for yet.
func TestOnlyRootReachesMCP(t *testing.T) {
	granted, err := readReaches(reachTable)
	require.NoError(t, err)

	for _, path := range []string{"/mcp", "/mcp/"} {
		row, said := granted[path]
		require.True(t, said, path+" is granted to nobody at all")
		assert.False(t, row.anyone, path+" is served without asking who is calling")
		assert.Empty(t, row.reach.Beyond(), path+" lets in a level beside ROOT")
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

// A handler no line names is ROOT's and nobody else's. It is served, behind a
// gate that admits no level beside ROOT, and reported.
func TestAHandlerNoLineNamesIsRoots(t *testing.T) {
	granted, err := readReaches("REACH is '/pond' of ROOT SUPER")
	require.NoError(t, err)

	answering := map[string]Answering{
		"/pond":        {Handler: func(http.ResponseWriter, *http.Request) {}},
		"/pond/keeper": {Handler: func(http.ResponseWriter, *http.Request) {}},
	}
	gated := map[string]auth.Reach{}
	with := plainly()
	with.Gate = func(_ string, re auth.Reach, h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			gated[r.URL.Path] = re
			h(w, r)
		}
	}
	mux, unnamed, err := build(granted, answering, with)
	require.NoError(t, err)

	assert.Equal(t, []string{"/pond/keeper"}, unnamed)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pond/keeper", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, gated["/pond/keeper"].Beyond(), "an unnamed path admitted a level beside ROOT")

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pond", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []auth.Level{auth.LevelSuper}, gated["/pond"].Beyond())
}

// plainly is the wrapping with nothing in it, so a test measures the grant and
// not the rate limiter.
func plainly() Wrapping {
	same := func(h http.HandlerFunc) http.HandlerFunc { return h }
	return Wrapping{
		Gate:     func(_ string, _ auth.Reach, h http.HandlerFunc) http.HandlerFunc { return h },
		Anyone:   same,
		Asked:    same,
		Upgraded: same,
	}
}
