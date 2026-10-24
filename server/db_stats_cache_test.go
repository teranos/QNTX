package server

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// On k one refresh took longer than the 30 s it was ticked at, so the disk was
// read without a pause. A refresh that took a minute now waits ten.
func TestNextRefreshIn_PacesByWhatTheLastTook(t *testing.T) {
	assert.Equal(t, 30*time.Second, nextRefreshIn(time.Second, 30*time.Second))
	assert.Equal(t, 30*time.Second, nextRefreshIn(3*time.Second, 30*time.Second))
	assert.Equal(t, 10*time.Minute, nextRefreshIn(time.Minute, 30*time.Second))
}

// A refresh never starts before the last one returned, and the gap after it is
// what it took, paced.
func TestPaceRefreshes_NeverOverlapsItself(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	const took = 10 * time.Millisecond
	inFlight := 0
	var starts []time.Time
	paceRefreshes(ctx, time.Millisecond, func() time.Duration {
		inFlight++
		require.Equal(t, 1, inFlight, "a refresh started while another ran")
		starts = append(starts, time.Now())
		time.Sleep(took)
		inFlight--
		return took
	})

	require.GreaterOrEqual(t, len(starts), 2)
	for at := 1; at < len(starts); at++ {
		assert.GreaterOrEqual(t, starts[at].Sub(starts[at-1]), took+nextRefreshIn(took, time.Millisecond))
	}
}

// "that stat, it wasnt even usefull to me"
// "cant you just ask it to kill it"
func TestLanding_SaysOnlyWhatIsReadWithoutAScan(t *testing.T) {
	out, err := json.Marshal(Landing{Namespace: "Ground", Path: "namespaces/ground.db", Bytes: 1, WalBytes: 2, Attestations: 3})
	require.NoError(t, err)
	var said map[string]any
	require.NoError(t, json.Unmarshal(out, &said))
	assert.ElementsMatch(t,
		[]string{"namespace", "path", "bytes", "wal_bytes", "attestations"},
		slices.Collect(maps.Keys(said)))
}

// A refresh that overran says so once, with what it took, when the next one is,
// and the landing whose count took longest.
func TestSayOverran_NamesTheSlowestLanding(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	node := bareNode()
	node.logger = zap.New(core).Sugar()

	node.sayOverran(time.Minute, []Landing{
		{Namespace: "default", Path: "namespaces/default.db", CountTook: time.Second},
		{Namespace: "Ground", Path: "namespaces/ground.db", CountTook: 50 * time.Second},
	})

	said := logs.FilterMessage("A database stats refresh took longer than its interval").All()
	require.Len(t, said, 1)
	fields := said[0].ContextMap()
	assert.Equal(t, time.Minute, fields["took"])
	assert.Equal(t, 10*time.Minute, fields["next_in"])
	assert.Equal(t, "Ground", fields["landing"])
	assert.Equal(t, "namespaces/ground.db", fields["landing_path"])
	assert.Equal(t, 50*time.Second, fields["landing_count_took"])
}
