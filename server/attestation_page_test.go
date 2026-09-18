package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// "the node knows it truncated; saying so is a field or a header"

// A full page looks the same whether it is the whole of what exists or the
// front of more. The store is asked for one row past the limit, and whether
// that row came is what X-QNTX-More says.
func TestAPageThatWasCutSaysSo(t *testing.T) {
	s := rootKnowingServer(t)
	root := rootOf(s)
	for _, subject := range []string{"one", "two", "three"} {
		require.Equal(t, http.StatusCreated, grants(t, s, root,
			`{"subjects":["`+subject+`"],"predicates":["seen"],"contexts":["default"]}`).Code)
	}

	page := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/attestations?predicate=seen"+query, nil)
		req = req.WithContext(auth.WithAdmission(req.Context(), root))
		rec := httptest.NewRecorder()
		s.handleGetAttestations(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return rec
	}

	cut := page("&limit=2")
	assert.Equal(t, "2", cut.Header().Get("X-QNTX-Limit"))
	assert.Equal(t, "true", cut.Header().Get("X-QNTX-More"), "three rows exist, two were given, and nothing said so")
	assert.Len(t, reads(t, s, root, "predicate=seen&limit=2"), 2, "the row asked to learn of the cut was given to the caller")

	whole := page("&limit=3")
	assert.Equal(t, "false", whole.Header().Get("X-QNTX-More"), "a page that is the whole of what exists said there was more")

	beyond := page("&limit=10")
	assert.Equal(t, "false", beyond.Header().Get("X-QNTX-More"))
}

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
