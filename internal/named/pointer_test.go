package named

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type quota struct{ max float64 }

func TestAPointerAtNothingIsRefusedNamingWhat(t *testing.T) {
	_, err := Pointed[quota]("the sending quota", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the sending quota")

	held, err := Pointed("the sending quota", &quota{max: 0})
	require.NoError(t, err)
	assert.Equal(t, quota{max: 0}, held, "a pointer at a zero value is that value")
}
