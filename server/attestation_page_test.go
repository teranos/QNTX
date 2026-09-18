package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Asking for nothing is asking for a page.
func TestNoLimitIsThePageDefault(t *testing.T) {
	limit, errMsg := pageOf("")

	require.Empty(t, errMsg)
	assert.Equal(t, pageDefault, limit)
}

// A caller that names a size under the ceiling gets exactly that size.
func TestALimitUnderTheCeilingIsTheLimit(t *testing.T) {
	limit, errMsg := pageOf("37")

	require.Empty(t, errMsg)
	assert.Equal(t, 37, limit)
}

// Above the ceiling the answer is the ceiling. The caller is not refused, and
// X-QNTX-Limit is where it reads what it actually got.
func TestALimitAboveTheCeilingBecomesTheCeiling(t *testing.T) {
	limit, errMsg := pageOf("5000")

	require.Empty(t, errMsg)
	assert.Equal(t, pageMost, limit)
}

// Zero, negative and non-numeric are not sizes, and the refusal names what was
// sent rather than substituting a number nobody asked for.
func TestALimitThatIsNotASizeIsRefusedByName(t *testing.T) {
	_, errMsg := pageOf("0")
	assert.Contains(t, errMsg, "0")

	_, errMsg = pageOf("-1")
	assert.Contains(t, errMsg, "-1")

	_, errMsg = pageOf("all")
	assert.Contains(t, errMsg, "all")
}
