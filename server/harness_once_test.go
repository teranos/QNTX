package server

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	errors "github.com/teranos/sacred-error"
)

// "Arrange for Claude Code to be held when a namespace Claude agent needs it" — "Yes"
//
// 「[Opus 5.5] A harness the node does not hold is fetched the first time it is
// needed, once however many ask at once; one already held is not fetched again,
// and a fetch that cannot begin holds nothing.」
func TestAHarnessIsFetchedOnceWhenFirstNeeded(t *testing.T) {
	s := &QNTXServer{}
	var fetches atomic.Int32
	fetch := func() (*harnessHeld, error) {
		fetches.Add(1)
		return &harnessHeld{name: "Claude Code", fetched: make(chan struct{})}, nil
	}
	var wg sync.WaitGroup
	got := make([]*harnessHeld, 20)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			held, err := s.holdHarnessOnce("claude", fetch)
			require.NoError(t, err)
			got[i] = held
		}(i)
	}
	wg.Wait()
	assert.Equal(t, int32(1), fetches.Load())
	for _, held := range got {
		assert.Same(t, got[0], held)
	}
	assert.Same(t, got[0], s.harnessHeldBy("claude"))

	_, err := s.holdHarnessOnce("codex", func() (*harnessHeld, error) { return nil, errors.New("no pin") })
	require.Error(t, err)
	assert.Nil(t, s.harnessHeldBy("codex"))
}
