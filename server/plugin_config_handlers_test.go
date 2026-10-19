package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "nil is nil": a body naming no config, or naming null, writes nothing to a
// plugin's record; {} names no key, so the record keeps every key it holds.
func TestAConfigNamingNothingIsRefused(t *testing.T) {
	s := rootKnowingServer(t)
	_, err := s.pluginRecords().AddPlugin(rootAccount, "https://github.com/teranos/pyre")
	require.NoError(t, err)

	put := func(method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/plugins/pyre/config", bytes.NewBufferString(body))
		r.SetPathValue("name", "pyre")
		w := httptest.NewRecorder()
		s.HandlePluginConfig(w, r)
		return w
	}

	assert.Equal(t, http.StatusBadRequest, put(http.MethodPut, `{}`).Code, "a body naming no config")
	assert.Equal(t, http.StatusBadRequest, put(http.MethodPut, `{"config":null}`).Code, "a config of null")
	assert.Equal(t, http.StatusOK, put(http.MethodPut, `{"config":{}}`).Code, "a config with no keys")

	refused := put(http.MethodPost, `{"config":{}}`)
	assert.Equal(t, http.StatusMethodNotAllowed, refused.Code)
	assert.Equal(t, "GET, PUT", refused.Header().Get("Allow"))
}

// An MCP caller sent each plugin's schema keys alone, and every build key was
// dropped, so a push had nothing to build. A PUT keeps every key of the record
// its body does not name, and a key sent holding nothing is removed.
func TestAPutKeepsTheKeysItDoesNotName(t *testing.T) {
	s := rootKnowingServer(t)
	records := s.pluginRecords()
	_, err := records.AddPlugin(rootAccount, "https://github.com/teranos/staands")
	require.NoError(t, err)
	require.NoError(t, records.ConfigurePlugin(rootAccount, "staands", map[string]string{
		buildCore:   "github.com/teranos/staands@main",
		"namespace": "Clean",
		"bucket":    "media",
	}))

	put := func(body string) {
		r := httptest.NewRequest(http.MethodPut, "/api/plugins/staands/config", bytes.NewBufferString(body))
		r.SetPathValue("name", "staands")
		w := httptest.NewRecorder()
		s.HandlePluginConfig(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	held := func() map[string]string {
		record, found, err := records.Plugin("staands")
		require.NoError(t, err)
		require.True(t, found)
		return record.Config
	}

	put(`{"config":{"bucket":"media.cleanamsterdam.nl"}}`)
	assert.Equal(t, map[string]string{
		buildCore:   "github.com/teranos/staands@main",
		"namespace": "Clean",
		"bucket":    "media.cleanamsterdam.nl",
	}, held(), "a PUT naming only bucket")

	put(`{"config":{"build.core":""}}`)
	assert.Equal(t, map[string]string{
		"namespace": "Clean",
		"bucket":    "media.cleanamsterdam.nl",
	}, held(), "build.core sent holding nothing")
}
