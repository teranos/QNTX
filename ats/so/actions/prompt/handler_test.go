package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "nil is nil"
func TestAPromptNamingNoProviderIsRefused(t *testing.T) {
	named, err := ProviderNamed("openrouter-qntx")
	require.NoError(t, err)
	assert.Equal(t, "openrouter-qntx", named)

	_, err = ProviderNamed("")
	assert.Error(t, err, "a provider nobody named was made up")
}
