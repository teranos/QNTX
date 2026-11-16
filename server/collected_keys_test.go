package server

import (
	"maps"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/statusline"
	errors "github.com/teranos/sacred-error"
)

// "「[Opus 5.5] our gap is kv」"

// 「[Opus 5.5] keysHeld stands in for etcd: what the collection set it to last,
// 「[Opus 5.5] or a refusal it was told to give.」」
type keysHeld struct {
	mu      sync.Mutex
	held    map[string][]byte
	refuses error
}

func (k *keysHeld) Set(whole map[string][]byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.refuses != nil {
		return k.refuses
	}
	k.held = maps.Clone(whole)
	return nil
}

func (k *keysHeld) now() map[string][]byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	return maps.Clone(k.held)
}

// 「[Opus 5.5] What waits for an agent is kept at a key of the agent's DID, as
// 「[Opus 5.5] collected list says it, and the key is gone once nothing waits.」」
func TestWhatIsCollectedIsKeptAtAKeyForEachAgent(t *testing.T) {
	srv, _ := namedOverA2A(t)
	keys := &keysHeld{}
	srv.collectedKeys = keys
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")

	all, refused := askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	require.Len(t, all.GetCollected(), 1)
	birch := all.GetCollected()[0]
	held := keys.now()
	require.Len(t, held, 1)
	listed, err := answerJSON(birch)
	require.NoError(t, err)
	assert.JSONEq(t, string(listed), string(held[birch.GetDid()]), "the key holds what collected list says of the agent")

	_, refused = askCollected(t, srv, asAPerson(), "cancel", sigil.Sent{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	assert.Empty(t, keys.now(), "a key stands for an agent nothing waits for")
}

// 「[Opus 5.5] etcd not taking it is said where failing handlers are, and what an
// 「[Opus 5.5] agent said is collected all the same: the file holds it.」」
func TestKeysEtcdDoesNotTakeAreSaid(t *testing.T) {
	srv, _ := namedOverA2A(t)
	srv.handlerFailures = statusline.NewHandlerFailureLog()
	srv.collectedKeys = &keysHeld{refuses: errors.New("etcd is down")}
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")

	all, refused := askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	require.Len(t, all.GetCollected(), 1)

	said := false
	for _, f := range srv.handlerFailures.Since(statusline.HandlerFailureWindow) {
		if f.Handler == "etcd-collected" && f.Error != "" {
			said = true
		}
	}
	assert.True(t, said, "etcd refused the keys and nothing said so")
}
