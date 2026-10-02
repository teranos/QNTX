package sacred

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAGroupNamesWhatItWaitsOn(t *testing.T) {
	var g Group
	hub := g.Add("server.hub")
	first := g.Add("ws.readPump")
	second := g.Add("ws.readPump")
	assert.Equal(t, []string{"server.hub", "ws.readPump ×2"}, g.Running())

	first()
	first()
	assert.Equal(t, []string{"server.hub", "ws.readPump"}, g.Running(), "a done called twice counted twice")

	second()
	hub()
	assert.Empty(t, g.Running())
	g.Wait()
}

func TestAGroupGoroutineIsNamedUntilItReturns(t *testing.T) {
	var g Group
	release := make(chan struct{})
	g.Go("pulse.poller", func() { <-release })
	assert.Equal(t, []string{"pulse.poller"}, g.Running())

	close(release)
	g.Wait()
	assert.Empty(t, g.Running())
}
