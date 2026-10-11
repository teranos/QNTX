package async

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A job a caller's schedule made names where that caller acted; one no caller
// made says so, rather than standing in a namespace named nothing.
func TestAJobNamesItsCallerOrWhyItHasNone(t *testing.T) {
	made, err := (&Job{ID: "JB1", HandlerName: "datapunt/observe", MadeBy: MadeByCaller, UserID: "US-1", Namespace: "Clean"}).Made()
	require.NoError(t, err)
	caller, isCaller := made.(Caller)
	require.True(t, isCaller, "a caller's job was made by %#v", made)
	assert.Equal(t, "US-1", caller.UserID)
	assert.Equal(t, "Clean", caller.Namespace.String())

	made, err = (&Job{ID: "JB2", HandlerName: "datapunt/observe", MadeBy: MadeByNoCaller}).Made()
	require.NoError(t, err)
	assert.Equal(t, NoCaller{}, made)

	_, err = (&Job{ID: "JB3", HandlerName: "datapunt/observe", MadeBy: MadeByCaller}).Made()
	require.Error(t, err, "a caller's job naming no namespace stood")
	assert.Contains(t, err.Error(), "JB3")

	_, err = (&Job{ID: "JB4", HandlerName: "datapunt/observe"}).Made()
	require.Error(t, err, "a job naming neither stood")
	assert.Contains(t, err.Error(), "JB4")
}
