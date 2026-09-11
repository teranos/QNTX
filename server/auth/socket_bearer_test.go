package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A browser's WebSocket sets no header of its own. An app that holds its
// session as a bearer offers it as a subprotocol, and the node reads it the
// way it reads an Authorization header.
func TestASocketCarriesItsBearerAsASubprotocol(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "bearer.abc123, something-else")

	raw, ok := bearerToken(r)
	assert.True(t, ok)
	assert.Equal(t, "abc123", raw)

	proto, ok := SocketBearer(r)
	assert.True(t, ok)
	assert.Equal(t, "bearer.abc123", proto)
}

// A handshake that offers no bearer is nobody's, and the header wins when both
// are present.
func TestASocketWithoutABearerOffersNone(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Sec-WebSocket-Protocol", "bearer., other")
	_, ok := bearerToken(r)
	assert.False(t, ok)

	r.Header.Set("Authorization", "Bearer fromheader")
	raw, ok := bearerToken(r)
	assert.True(t, ok)
	assert.Equal(t, "fromheader", raw)
}
