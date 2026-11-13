package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A turn a verified delivery started is said by the app that wrote the
// comment; a request nobody was admitted on and no delivery started says nobody.
func TestADeliverysTurnIsSaidByTheApp(t *testing.T) {
	said, err := http.NewRequestWithContext(withSpeaker(context.Background(), "github-app:qodo-code-review"), http.MethodPost, "/github/comments", nil)
	require.NoError(t, err)
	assert.Equal(t, "github-app:qodo-code-review", spokenBy(said))

	plain, err := http.NewRequest(http.MethodPost, "/anything", nil)
	require.NoError(t, err)
	assert.Equal(t, "", spokenBy(plain))
}
