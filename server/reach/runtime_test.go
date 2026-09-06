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

// The latest line for a path is the whole truth of which roles reach it.
// WORKER is gone at 10:00 because it is not said, not because it was taken
// back.
func TestALaterRuntimeLineSupersedesAnEarlierOne(t *testing.T) {
	at := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	rows := map[string]aRow{}
	addRuntime(rows, Runtime{Lines: []Line{
		{Paths: []string{"/pond"}, Roles: []string{"WORKER", "COORDINATOR"}, At: at},
		{Paths: []string{"/pond"}, Roles: []string{"COORDINATOR"}, At: at.Add(time.Hour)},
	}})
	assert.Equal(t, []string{"COORDINATOR"}, rows["/pond"].reach.Roles())
}

// A ROOT line beats a later line from anyone else, the way a grant does.
func TestARootRuntimeLineOutranksALaterOne(t *testing.T) {
	at := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	rows := map[string]aRow{}
	addRuntime(rows, Runtime{
		Lines: []Line{
			{Paths: []string{"/pond"}, Roles: []string{"WORKER"}, Actor: "root", At: at},
			{Paths: []string{"/pond"}, Roles: []string{"COORDINATOR"}, Actor: "somebody", At: at.Add(time.Hour)},
		},
		IsRoot: func(actor string) bool { return actor == "root" },
	})
	assert.Equal(t, []string{"WORKER"}, rows["/pond"].reach.Roles())
}

// Lines supersede per path. A line naming two paths and a later line naming
// one of them leaves the other as it was.
func TestRuntimeLinesSettlePerPath(t *testing.T) {
	at := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	rows := map[string]aRow{}
	addRuntime(rows, Runtime{Lines: []Line{
		{Paths: []string{"/pond", "/pond/keeper"}, Roles: []string{"WORKER"}, At: at},
		{Paths: []string{"/pond"}, Roles: []string{"COORDINATOR"}, At: at.Add(time.Hour)},
	}})
	assert.Equal(t, []string{"COORDINATOR"}, rows["/pond"].reach.Roles())
	assert.Equal(t, []string{"WORKER"}, rows["/pond/keeper"].reach.Roles())
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
