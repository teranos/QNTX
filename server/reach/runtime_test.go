package reach

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// The const table is the floor and never shrinks. A line written at runtime
// adds roles only, so one naming a level or ANYONE is refused.
func TestARuntimeLineNamingALevelIsRefused(t *testing.T) {
	for _, context := range []string{"ROOT", "SUPER", "ANYONE", "PUBLIC_REGISTRATION", "root"} {
		_, err := ReadLine([]string{"REACH"}, []string{"/pond"}, []string{context}, nil, time.Now())
		assert.Error(t, err, context)
	}
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
