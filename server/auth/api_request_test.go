package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A comet is fetched by ground, not read by a person: refused, it is told why in JSON, not sent to a login page.
func TestACometIsAskedForByAProgram(t *testing.T) {
	assert.True(t, isAPIRequest(httptest.NewRequest("GET", "/comet/abcd-nl/grove", nil)))
	assert.False(t, isAPIRequest(httptest.NewRequest("GET", "/canvas", nil)))
}
