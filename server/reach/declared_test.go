package reach

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// "open /a2a/ and /am/statusline/{name} to GROUND on f42d's node"
//
// A line am.toml says (auth.reach) is a runtime line by an actor that is not
// ROOT, stamped at no time: it opens a path the tablet names to a role, and a
// line ROOT writes in the store about the same pair outranks it.
func TestALineAmTomlSaysOpensAPathToARoleAndROOTsLineOutranksIt(t *testing.T) {
	isRoot := func(actor string) bool { return actor == "did:key:root" }
	said, err := ReadLine([]string{Subject}, []string{"/a2a/"}, []string{"GROUND"}, []string{"am.toml"}, time.Time{})
	require.NoError(t, err)
	assert.Empty(t, said.Unopenable(nil), "a role was refused as if it were a level")

	rows, err := readReaches("REACH is '/a2a' '/a2a/' of ROOT")
	require.NoError(t, err)
	addRuntime(rows, Runtime{Lines: []Line{said}, IsRoot: isRoot})
	assert.Equal(t, []string{"GROUND"}, rows["/a2a/"].reach.Roles())
	assert.Empty(t, rows["/a2a"].reach.Roles(), "a path the line does not name was opened")

	// ROOT takes it back in the store, earlier on the clock than nothing is not.
	taken, err := ReadLine([]string{Subject}, []string{"/a2a/", auth.PredicateReachRevoked}, []string{"GROUND"}, []string{"did:key:root"}, time.Unix(1, 0))
	require.NoError(t, err)
	rows, err = readReaches("REACH is '/a2a' '/a2a/' of ROOT")
	require.NoError(t, err)
	addRuntime(rows, Runtime{Lines: []Line{said, taken}, IsRoot: isRoot})
	assert.Empty(t, rows["/a2a/"].reach.Roles(), "am.toml's line outranked ROOT's")
}
