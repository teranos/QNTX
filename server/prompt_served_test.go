package server

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/teranos/QNTX/internal/ai/provider"
)

// Qodo's review of #1511: a fallback the provider reports is in the
// prompt-result attestation when it reports no attempt count. Written only
// beside a count above zero, it was left out while the HTTP report said it.
func TestAFallbackWithNoAttemptCountIsAttested(t *testing.T) {
	attrs := servedAttributes(&provider.Served{UpstreamProvider: "secondary", Fallback: true})
	assert.Equal(t, true, attrs["fallback"])
	assert.NotContains(t, attrs, "attempt_count")

	attrs = servedAttributes(&provider.Served{AttemptCount: 1})
	assert.Equal(t, 1, attrs["attempt_count"])
	assert.Equal(t, false, attrs["fallback"], "a count reported says the provider reported whether it fell back")

	assert.NotContains(t, servedAttributes(&provider.Served{UpstreamProvider: "x"}), "fallback")
}
