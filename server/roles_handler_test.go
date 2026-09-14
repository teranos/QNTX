package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// The roles glyph shows what the lines say, and it is one reading: the same
// words an admission gets, the same reach the mux was built from, the same
// holders a grant settles to. A glyph that worked the lines out for itself
// would be a second answer about who may do what.
func TestTheRolesAreAnsweredWhole(t *testing.T) {
	s := workersNode(t)

	req := httptest.NewRequest(http.MethodGet, "/api/roles", nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), rootOf(s)))
	rec := httptest.NewRecorder()
	s.HandleRoles(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var answer rolesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	require.Equal(t, 2, answer.Count)

	coordinator, worker := answer.Roles[0], answer.Roles[1]
	require.Equal(t, "COORDINATOR", coordinator.Name)
	require.Equal(t, "WORKER", worker.Name)

	assert.Equal(t, []string{"visit:done"}, worker.Write)
	assert.Equal(t, []string{"visit:assigned", "visit:done"}, worker.Read)
	assert.False(t, worker.All)
	assert.Equal(t, []string{"/api/attestations"}, worker.Reach)
	assert.Equal(t, map[string][]string{"default": {"spike", "tim"}}, worker.Holders,
		"the holders are the tokens by name, in the namespace the grant named")

	assert.True(t, coordinator.All, "COORDINATOR's READ line says by all")
	assert.Equal(t, []string{}, coordinator.Write)
	assert.Empty(t, coordinator.Holders, "nobody was handed COORDINATOR")
}

// A revoke takes a holder off the answer the same way it takes the role off
// the token.
func TestARevokedHolderIsNotListed(t *testing.T) {
	s := workersNode(t)
	require.Equal(t, http.StatusCreated, grants(t, s, rootOf(s), revokeFrom("spike", "WORKER")).Code)

	req := httptest.NewRequest(http.MethodGet, "/api/roles", nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), rootOf(s)))
	rec := httptest.NewRecorder()
	s.HandleRoles(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var answer rolesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	assert.Equal(t, map[string][]string{"default": {"tim"}}, answer.Roles[1].Holders)
}
