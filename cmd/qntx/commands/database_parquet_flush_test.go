//go:build cgo && rustduckdb && !quickdev

package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A flush that keeps failing is tried less often, never more than every
// flushBackoffCap, and one that works is back to the base at once.
func TestWatcherFlushBacksOffWhileFailingAndRecoversAtOnce(t *testing.T) {
	base := 5 * time.Second
	wait := base
	var waits []time.Duration
	for range 10 {
		wait = nextFlushWait(wait, base, true)
		waits = append(waits, wait)
	}
	assert.Equal(t, 10*time.Second, waits[0])
	assert.Equal(t, 20*time.Second, waits[1])
	assert.Equal(t, flushBackoffCap, waits[len(waits)-1])
	assert.Equal(t, base, nextFlushWait(wait, base, false))
}
