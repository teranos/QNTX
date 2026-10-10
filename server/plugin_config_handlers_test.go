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
// plugin's record; {} is a plugin configured with no keys.
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
