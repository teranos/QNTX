package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

func askingForADraft(t *testing.T, level auth.Level, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/doors/draft", strings.NewReader(body))
	return r.WithContext(auth.WithAdmission(r.Context(), auth.Admission{Level: level}))
}

// A node that keeps no namespaces has nothing to draft a door onto, and says
// which store is running rather than refusing without a reason.
func TestDraftingNeedsANodeThatKeepsNamespaces(t *testing.T) {
	s := &QNTXServer{store: "sqlite"}

	w := httptest.NewRecorder()
	s.HandleDoorDraft(w, askingForADraft(t, auth.LevelRoot, `{"namespace":"garden"}`))

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	assert.Contains(t, w.Body.String(), "parquet")
}

// Same gate as creating the namespace this door would open onto. What a draft
// says out loud is the deployment describing itself.
func TestDraftingIsGatedTheWayCreatingIs(t *testing.T) {
	s := &QNTXServer{namespaces: nil, store: "sqlite"}

	// Below the gate, the backend answer comes first and names no door — the
	// same order superNamespaces already answers in.
	w := httptest.NewRecorder()
	s.HandleDoorDraft(w, askingForADraft(t, auth.LevelPublicRegistration, `{"namespace":"garden"}`))
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// Only POST. A draft is asked for with a body, and a GET carrying one is a
// caller who has misread the route.
func TestDraftingIsPostOnly(t *testing.T) {
	s := &QNTXServer{}

	r := httptest.NewRequest(http.MethodGet, "/api/doors/draft", nil)
	w := httptest.NewRecorder()
	s.HandleDoorDraft(w, r)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// The shape the UI reads. Held here so a field cannot be renamed out from under
// the thing that renders it.
func TestTheDraftShapeIsWhatTheGlyphReads(t *testing.T) {
	said, err := json.Marshal(auth.DoorDraft{
		Namespace:   "garden",
		RPID:        "garden.test",
		Origins:     []string{"https://portal.garden.test"},
		RedirectURI: "https://api.node.example/auth/binding/callback",
		TOML:        "[auth.door.garden]\n",
		ClientTOML:  "[auth.door.garden.provider.google]\n",
	})
	require.NoError(t, err)

	var read map[string]any
	require.NoError(t, json.Unmarshal(said, &read))
	for _, field := range []string{"namespace", "rp_id", "origins", "redirect_uri", "toml", "client_toml"} {
		assert.Contains(t, read, field)
	}
}
