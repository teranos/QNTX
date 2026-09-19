package reach

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// The const table is the floor and never shrinks. A line written at runtime
// adds roles, PUBLIC_REGISTRATION and ANYONE; one naming another level is
// refused.
func TestARuntimeLineNamingALevelIsRefused(t *testing.T) {
	for _, context := range []string{"ROOT", "SUPER", "TOKEN", "ATTESTOR", "root"} {
		_, err := ReadLine([]string{"REACH"}, []string{"/pond"}, []string{context}, nil, time.Now())
		assert.Error(t, err, context)
	}
}

const aPluginRoute = "/api/hello-world/{path...}"

func thatPluginOnly(path string) bool { return path == aPluginRoute }

// "Should runtime lines be allowed to name PUBLIC_REGISTRATION, yeah, this
// would allow me to do this in the QNTX web ui right?"
//
// "this is for plugin routes, the runtime configurable part, the reach table
// remains static"
func TestARuntimeLineOpensAPluginRouteToPublicRegistration(t *testing.T) {
	line, err := ReadLine([]string{"REACH"}, []string{aPluginRoute}, []string{"public_registration"}, nil, time.Now())
	require.NoError(t, err)

	rows, err := readReaches("REACH is '/pond' of ROOT SUPER")
	require.NoError(t, err)
	addRuntime(rows, Runtime{Lines: []Line{line}, Plugin: thatPluginOnly})

	assert.Equal(t, []auth.Level{auth.LevelPublicRegistration}, rows[aPluginRoute].reach.Beyond())
	assert.Empty(t, rows[aPluginRoute].reach.Roles(), "a level was read as a role of that name")
	assert.Empty(t, line.Unopenable(thatPluginOnly))
}

// "and runtime can never supersede the coompiled in reach table"
func TestARuntimeLevelNeverOpensWhatTheCompiledTableNames(t *testing.T) {
	everything := func(string) bool { return true }
	line := Line{Paths: []string{"/pond"}, Roles: []string{"PUBLIC_REGISTRATION"}, At: time.Now()}

	rows, err := readReaches("REACH is '/pond' of ROOT SUPER")
	require.NoError(t, err)
	addRuntime(rows, Runtime{Lines: []Line{line}, Plugin: everything})
	assert.False(t, rows["/pond"].reach.Admits(auth.Admitted(auth.LevelPublicRegistration)),
		"a runtime line opened a path the compiled table names")

	onTheNode := Line{Paths: []string{"/api/staands"}, Roles: []string{"PUBLIC_REGISTRATION"}}
	assert.Equal(t, []string{"/api/staands"}, onTheNode.Unopenable(everything), "a write over the compiled table was not refused")
}

// A path that is no plugin's is the node's own, and stays static.
func TestARuntimeLevelNeverOpensTheNodesOwnPaths(t *testing.T) {
	line := Line{Paths: []string{"/pond"}, Roles: []string{"PUBLIC_REGISTRATION"}, At: time.Now()}
	rows := map[string]aRow{}
	addRuntime(rows, Runtime{Lines: []Line{line}, Plugin: thatPluginOnly})

	assert.False(t, rows["/pond"].reach.Admits(auth.Admitted(auth.LevelPublicRegistration)))
	assert.Equal(t, []string{"/pond"}, line.Unopenable(thatPluginOnly))
	assert.Equal(t, []string{"/pond"}, line.Unopenable(nil), "a node with no plugins opened something")
}

// A line naming roles only is not about levels, and nothing on it is refused.
func TestARoleLineIsNeverUnopenable(t *testing.T) {
	line := Line{Paths: []string{"/pond"}, Roles: []string{"WORKER"}}
	assert.Empty(t, line.Unopenable(nil))
}

// "book/new is actually more open than PUBLIC_REGISTRATION, its pretty much
// PUBLIC"
func TestARuntimeLineOpensAPluginPathToAnyone(t *testing.T) {
	const aPluginPath = "/api/hello-world/book/new"
	plugin := func(path string) bool { return path == aPluginPath }
	line, err := ReadLine([]string{"REACH"}, []string{aPluginPath}, []string{"anyone"}, nil, time.Now())
	require.NoError(t, err)

	rows := map[string]aRow{}
	addRuntime(rows, Runtime{Lines: []Line{line}, Plugin: plugin})
	assert.True(t, rows[aPluginPath].anyone, "the plugin path asks who is calling")
	assert.Empty(t, rows[aPluginPath].reach.Roles(), "ANYONE was read as a role of that name")

	compiled, err := readReaches("REACH is '/pond' of ROOT SUPER")
	require.NoError(t, err)
	onTheNode := Line{Paths: []string{"/pond"}, Roles: []string{"ANYONE"}, At: time.Now()}
	addRuntime(compiled, Runtime{Lines: []Line{onTheNode}, Plugin: func(string) bool { return true }})
	assert.False(t, compiled["/pond"].anyone, "a runtime line opened a path the compiled table names")
	assert.Equal(t, []string{"/pond"}, onTheNode.Unopenable(plugin))
}

// Revoking takes back what a runtime line opened.
func TestRevokingPublicRegistrationClosesThePluginRoute(t *testing.T) {
	at := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	opened := Line{Paths: []string{aPluginRoute}, Roles: []string{"PUBLIC_REGISTRATION"}, At: at}
	closed := Line{Paths: []string{aPluginRoute}, Roles: []string{"PUBLIC_REGISTRATION"}, Revoked: true, At: at.Add(time.Minute)}

	rows := map[string]aRow{}
	addRuntime(rows, Runtime{Lines: []Line{opened, closed}, Plugin: thatPluginOnly})
	assert.False(t, rows[aPluginRoute].reach.Admits(auth.Admitted(auth.LevelPublicRegistration)), "the revoke did not hold")
}

// A line about anything but REACH, or naming no path, or naming no role, is
// not a reach line.
func TestARuntimeLineThatSaysNothingIsRefused(t *testing.T) {
	now := time.Now()
	_, err := ReadLine([]string{"GRANT"}, []string{"/pond"}, []string{"WORKER"}, nil, now)
	assert.Error(t, err, "about something else")
	_, err = ReadLine([]string{"REACH"}, nil, []string{"WORKER"}, nil, now)
	assert.Error(t, err, "no path")
	_, err = ReadLine([]string{"REACH"}, []string{"/pond"}, nil, nil, now)
	assert.Error(t, err, "no role")
}

// A role is any upper-case word a runtime line names. Written lower, read
// upper: a role is a subject and a subject is canonicalised.
func TestARoleOnARuntimeLineIsInTheRow(t *testing.T) {
	line, err := ReadLine([]string{"REACH"}, []string{"/pond"}, []string{"worker"}, []string{"https://geology.club/@golem", "COORDINATOR"}, time.Now())
	require.NoError(t, err)
	assert.Equal(t, []string{"WORKER"}, line.Roles)
	assert.Equal(t, "https://geology.club/@golem", line.Actor)
	assert.Equal(t, []string{"COORDINATOR"}, line.By, "what came after by is kept for phase 4")

	rows, err := readReaches("REACH is '/pond' of ROOT")
	require.NoError(t, err)
	addRuntime(rows, Runtime{Lines: []Line{line}})
	assert.Equal(t, []string{"WORKER"}, rows["/pond"].reach.Roles())
}

// A runtime line for a path the const names adds roles to that row. The
// levels the const granted stay.
func TestARuntimeLineAddsToAConstRow(t *testing.T) {
	rows, err := readReaches("REACH is '/pond' of ROOT SUPER")
	require.NoError(t, err)
	addRuntime(rows, Runtime{Lines: []Line{{Paths: []string{"/pond"}, Roles: []string{"WORKER"}, At: time.Now()}}})
	assert.Equal(t, []auth.Level{auth.LevelSuper}, rows["/pond"].reach.Beyond())
	assert.Equal(t, []string{"WORKER"}, rows["/pond"].reach.Roles())
}

// "REACH is /api/namespaces of WORKER and REACH is /api/namespaces of NOBODY
// and REACH is /api/namespaces of ANOTHERWORKER should all work
// simultaneously and not 'overwrite' each other". Lines about different
// pairs stand together; a later line about another role takes nothing.
func TestRuntimeLinesAboutDifferentRolesStandTogether(t *testing.T) {
	at := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	rows := map[string]aRow{}
	addRuntime(rows, Runtime{Lines: []Line{
		{Paths: []string{"/pond"}, Roles: []string{"WORKER"}, At: at},
		{Paths: []string{"/pond"}, Roles: []string{"NOBODY"}, At: at.Add(time.Minute)},
		{Paths: []string{"/pond"}, Roles: []string{"ANOTHERWORKER"}, At: at.Add(2 * time.Minute)},
	}})
	assert.ElementsMatch(t, []string{"WORKER", "NOBODY", "ANOTHERWORKER"}, rows["/pond"].reach.Roles())
}

// "but also, i need to be able to attest the inverse somehow". The inverse is
// a line with reach:revoked beside the paths: the latest line about the pair
// wins, and a revoked pair is gone. The other pairs are untouched.
func TestARevokedLineTakesThePairAway(t *testing.T) {
	at := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	rows := map[string]aRow{}
	addRuntime(rows, Runtime{Lines: []Line{
		{Paths: []string{"/pond", "/pond/keeper"}, Roles: []string{"WORKER"}, At: at},
		{Paths: []string{"/pond"}, Roles: []string{"NOBODY"}, At: at.Add(time.Minute)},
		{Paths: []string{"/pond"}, Roles: []string{"NOBODY"}, Revoked: true, At: at.Add(2 * time.Minute)},
	}})
	assert.Equal(t, []string{"WORKER"}, rows["/pond"].reach.Roles(), "NOBODY was revoked and WORKER was not")
	assert.Equal(t, []string{"WORKER"}, rows["/pond/keeper"].reach.Roles())

	line, err := ReadLine([]string{"REACH"}, []string{auth.PredicateReachRevoked, "/pond"}, []string{"NOBODY"}, nil, at)
	require.NoError(t, err)
	assert.True(t, line.Revoked)
	assert.Equal(t, []string{"/pond"}, line.Paths, "the marker is not a path")
	_, err = ReadLine([]string{"REACH"}, []string{auth.PredicateReachRevoked}, []string{"NOBODY"}, nil, at)
	assert.Error(t, err, "a revoke of nothing names no path")
}

// A ROOT line beats a later line from anyone else about the same pair, the
// way a grant does.
func TestARootRuntimeLineOutranksALaterOne(t *testing.T) {
	at := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	rows := map[string]aRow{}
	addRuntime(rows, Runtime{
		Lines: []Line{
			{Paths: []string{"/pond"}, Roles: []string{"WORKER"}, Actor: "root", At: at},
			{Paths: []string{"/pond"}, Roles: []string{"WORKER"}, Revoked: true, Actor: "somebody", At: at.Add(time.Hour)},
		},
		IsRoot: func(actor string) bool { return actor == "root" },
	})
	assert.Equal(t, []string{"WORKER"}, rows["/pond"].reach.Roles(), "somebody's revoke beat ROOT's grant")
}

// The const table reads as before with no runtime lines at all.
func TestTheConstTableReadsAloneWithNoRuntime(t *testing.T) {
	rows, err := readReaches(reachTable)
	require.NoError(t, err)
	before := len(rows)
	addRuntime(rows, Runtime{})
	assert.Equal(t, before, len(rows))
	for _, row := range rows {
		assert.Empty(t, row.reach.Roles())
	}
}
