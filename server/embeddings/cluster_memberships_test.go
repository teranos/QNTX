package embeddings

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClusterMemberships_MethodNotAllowed(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodPost, "/api/embeddings/clusters/memberships", nil)
	rec := httptest.NewRecorder()

	h.HandleClusterMemberships(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
