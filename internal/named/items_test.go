package named

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoItemsAreRefusedNamingWhatWasEmpty(t *testing.T) {
	for _, none := range [][]string{nil, {}} {
		_, err := ItemsOf("texts", none)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "texts")
	}

	items, err := ItemsOf("texts", []string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, items.All())
	assert.Equal(t, "a", items.First())
}
