package named

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNothingIsRefusedNamingWhatWasEmpty(t *testing.T) {
	_, err := TextOf("index", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "index")

	text, err := TextOf("index", "attestations")
	require.NoError(t, err)
	assert.Equal(t, "attestations", text.String())
}

func TestFewerThanOneIsRefused(t *testing.T) {
	for _, n := range []int{0, -1} {
		_, err := CountOf("dimensions", n)
		require.Error(t, err, "%d", n)
		assert.Contains(t, err.Error(), "dimensions")
	}
	count, err := CountOf("dimensions", 384)
	require.NoError(t, err)
	assert.Equal(t, 384, count.Int())
}
