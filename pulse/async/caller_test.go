package async

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A job a caller's schedule made names where that caller acted; one no caller
// made says so, rather than standing in a namespace named nothing.
func TestAJobNamesItsCallerOrWhyItHasNone(t *testing.T) {
	made := &Job{ID: "JB1", HandlerName: "datapunt/observe", UserID: "US-1", Namespace: "Clean"}
	caller, err := made.Caller()
	require.NoError(t, err)
	assert.Equal(t, "US-1", caller.UserID)
	assert.Equal(t, "Clean", caller.Namespace.String())

	_, err = (&Job{ID: "JB2", HandlerName: "datapunt/observe"}).Caller()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JB2")
	assert.Contains(t, err.Error(), "no caller")
}
