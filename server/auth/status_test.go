package auth

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func statusOf(t *testing.T, h *Handler) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	h.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/auth/status", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

// The owner DID is what a person is, so the UI can show it instead of leaving
// the operator guessing whether an identity was ever established.
func TestStatusReportsTheOwnerDID(t *testing.T) {
	h := handlerWithCreds(t)

	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	did := EncodeDIDKey(pub)
	require.NoError(t, h.creds.save(credential("laptop"), did, ""))

	body := statusOf(t, h)
	assert.Equal(t, true, body["registered"])
	assert.Equal(t, did, body["owner_did"])
}

// A credential registered before PRF has no owner. Reporting empty is the
// honest answer and makes the gap visible rather than implied.
func TestStatusReportsAnEmptyOwnerWhenNoneWasEstablished(t *testing.T) {
	h := handlerWithCreds(t)
	require.NoError(t, h.creds.save(credential("legacy"), "", ""))

	body := statusOf(t, h)
	assert.Equal(t, true, body["registered"])
	assert.Equal(t, "", body["owner_did"])
}

// Nothing registered at all still answers, so the UI can tell "no passkey"
// apart from "passkey without an identity".
func TestStatusWithNoCredentials(t *testing.T) {
	h := handlerWithCreds(t)

	body := statusOf(t, h)
	assert.Equal(t, false, body["registered"])
	assert.Equal(t, "", body["owner_did"])
}
