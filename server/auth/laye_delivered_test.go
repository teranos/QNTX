package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "yeah, because right now, i dont even see the outgoing towards node agent in ag right? yeah thats fucked up as well"

func delivering(t *testing.T, h *Handler, signedBy ed25519.PrivateKey, did string, delivered LayeDelivered) *httptest.ResponseRecorder {
	t.Helper()
	own, err := h.ownDID()
	require.NoError(t, err)
	body, err := json.Marshal(delivered)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, layeDeliveredPath, bytes.NewReader(body))
	req.Header.Set("Authorization", signedScheme+carriedBy(t, signedBy, did, did, own))
	w := httptest.NewRecorder()
	h.handleLayeDelivered(w, req)
	return w
}

func TestWhatLayeReleasesForTheNodesAgentIsItsTurn(t *testing.T) {
	h, _, laye, layeDID, _ := behindLaye(t)
	var ran []LayeDelivered
	h.SetLayeDelivered(func(_ context.Context, delivered LayeDelivered) (string, error) {
		ran = append(ran, delivered)
		return "task-1", nil
	})
	sent := LayeDelivered{DID: "did:key:zLamplighter", To: "Lamplighter", From: "laye", Says: "Collected for you: …"}
	w := delivering(t, h, laye, layeDID, sent)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"task":"task-1"}`, w.Body.String())
	require.Len(t, ran, 1)
	assert.Equal(t, sent.Says, ran[0].Says)
}

func TestWhatLayeDidNotSignIsNoTurn(t *testing.T) {
	h, _, _, _, _ := behindLaye(t)
	ran := 0
	h.SetLayeDelivered(func(context.Context, LayeDelivered) (string, error) { ran++; return "", nil })
	_, other, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	w := delivering(t, h, other, EncodeDIDKey(other.Public().(ed25519.PublicKey)), LayeDelivered{DID: "did:key:zLamplighter", Says: "x"})
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.Zero(t, ran, "what another key signed was run as the agent's turn")
}
