package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A file a peer did not take goes behind the others waiting for that peer:
// once the peer is tried again, the next one is brought first, so one that
// fails again and again keeps none of the rest from it (CodeRabbit on #1812).
func TestAFileThatWasNotTakenWaitsBehindTheRest(t *testing.T) {
	first := toBring{asset: "sha256:aa", fileID: "a", filename: "a.m4a", to: "k"}
	second := toBring{asset: "sha256:bb", fileID: "b", filename: "b.m4a", to: "k"}
	c := &fileCarrier{waiting: []toBring{first, second}}
	now := time.Now()

	got, ready, _ := c.next(now)
	require.True(t, ready)
	assert.Equal(t, first, got)

	again := c.refused(first, now)
	_, ready, _ = c.next(now)
	assert.False(t, ready, "a peer that did not take a file is left alone for its backoff")

	got, ready, _ = c.next(now.Add(again))
	require.True(t, ready)
	assert.Equal(t, second, got, "the file that was not taken came first again")
}
