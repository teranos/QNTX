package embeddings

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClusterMemberships_NilStore_Returns503(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodGet, "/api/embeddings/clusters/memberships?ids=a,b", nil)
	rec := httptest.NewRecorder()

	h.HandleClusterMemberships(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestClusterMemberships_MethodNotAllowed(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodPost, "/api/embeddings/clusters/memberships", nil)
	rec := httptest.NewRecorder()

	h.HandleClusterMemberships(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
