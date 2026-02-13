package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"

	"go.uber.org/zap"
)

// chanConn implements Conn over a pair of channels for in-process testing.
// Messages are JSON-serialized through the channels to match real WebSocket behavior.
type chanConn struct {
	in  chan json.RawMessage
	out chan json.RawMessage
}

func (c *chanConn) ReadJSON(v interface{}) error {
	raw, ok := <-c.in
	if !ok {
		return fmt.Errorf("connection closed")
	}
	return json.Unmarshal(raw, v)
}

func (c *chanConn) WriteJSON(v interface{}) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.out <- raw
	return nil
}

func (c *chanConn) Close() error {
	return nil
}

// connPair creates two connected Conn implementations for testing.
func connPair() (Conn, Conn) {
	ab := make(chan json.RawMessage, 32)
	ba := make(chan json.RawMessage, 32)
	return &chanConn{in: ba, out: ab}, &chanConn{in: ab, out: ba}
}

// memStore is a minimal in-memory AttestationStore for testing.
type memStore struct {
	attestations map[string]*types.As
}

func newMemStore() *memStore {
	return &memStore{attestations: make(map[string]*types.As)}
}

func (s *memStore) CreateAttestation(as *types.As) error {
	s.attestations[as.ID] = as
	return nil
}

func (s *memStore) AttestationExists(asid string) bool {
	_, ok := s.attestations[asid]
	return ok
}

func (s *memStore) GenerateAndCreateAttestation(cmd *types.AsCommand) (*types.As, error) {
	as := cmd.ToAs(fmt.Sprintf("as-test-%d", time.Now().UnixNano()))
	if err := s.CreateAttestation(as); err != nil {
		return nil, err
	}
	return as, nil
}

func (s *memStore) GetAttestations(filters ats.AttestationFilter) ([]*types.As, error) {
	var results []*types.As
	for _, as := range s.attestations {
		if matchesFilter(as, filters) {
			results = append(results, as)
		}
	}
	return results, nil
}

func matchesFilter(as *types.As, f ats.AttestationFilter) bool {
	if len(f.Actors) > 0 && !anyIn(f.Actors, as.Actors) {
		return false
	}
	if len(f.Contexts) > 0 && !anyIn(f.Contexts, as.Contexts) {
		return false
	}
	return true
}

func anyIn(want, have []string) bool {
	for _, w := range want {
		for _, h := range have {
			if w == h {
				return true
			}
		}
	}
	return false
}

func testLogger() *zap.SugaredLogger {
	logger, _ := zap.NewDevelopment()
	return logger.Sugar()
}

func makeAs(id, subject, predicate, ctx, actor string) *types.As {
	return &types.As{
		ID:         id,
		Subjects:   []string{subject},
		Predicates: []string{predicate},
		Contexts:   []string{ctx},
		Actors:     []string{actor},
		Timestamp:  time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
		Source:     "cli",
		CreatedAt:  time.Now(),
	}
}

func insertWithTree(store *memStore, tree *Tree, as *types.As) {
	store.CreateAttestation(as)
	ch := ContentHash(as)
	for _, actor := range as.Actors {
		for _, ctx := range as.Contexts {
			tree.Insert(GroupKey{Actor: actor, Context: ctx}, ch)
		}
	}
}

func TestPeer_AlreadyInSync(t *testing.T) {
	connA, connB := connPair()
	treeA, treeB := NewTree(), NewTree()
	storeA, storeB := newMemStore(), newMemStore()
	logger := testLogger()

	as := makeAs("as-1", "user-1", "member", "team", "hr")
	insertWithTree(storeA, treeA, as)
	insertWithTree(storeB, treeB, as)

	peerA := NewPeer(connA, treeA, storeA, logger)
	peerB := NewPeer(connB, treeB, storeB, logger)

	ctx := context.Background()

	type result struct {
		sent, received int
		err            error
	}
	ch := make(chan result, 2)

	go func() {
		s, r, err := peerA.Reconcile(ctx)
		ch <- result{s, r, err}
	}()
	go func() {
		s, r, err := peerB.Reconcile(ctx)
		ch <- result{s, r, err}
	}()

	for i := 0; i < 2; i++ {
		res := <-ch
		if res.err != nil {
			t.Fatalf("reconciliation failed: %v", res.err)
		}
		if res.sent != 0 || res.received != 0 {
			t.Fatalf("expected 0 sent/received for synced trees, got sent=%d received=%d",
				res.sent, res.received)
		}
	}
}

func TestPeer_OneSideHasMore(t *testing.T) {
	connA, connB := connPair()
	treeA, treeB := NewTree(), NewTree()
	storeA, storeB := newMemStore(), newMemStore()
	logger := testLogger()

	// Both have as1
	as1 := makeAs("as-1", "user-1", "member", "team", "hr")
	insertWithTree(storeA, treeA, as1)
	insertWithTree(storeB, treeB, as1)

	// Only A has as2
	as2 := makeAs("as-2", "user-2", "admin", "team", "hr")
	insertWithTree(storeA, treeA, as2)

	peerA := NewPeer(connA, treeA, storeA, logger)
	peerB := NewPeer(connB, treeB, storeB, logger)

	ctx := context.Background()

	type result struct {
		sent, received int
		err            error
	}
	ch := make(chan result, 2)

	go func() {
		s, r, err := peerA.Reconcile(ctx)
		ch <- result{s, r, err}
	}()
	go func() {
		s, r, err := peerB.Reconcile(ctx)
		ch <- result{s, r, err}
	}()

	var totalSent, totalReceived int
	for i := 0; i < 2; i++ {
		res := <-ch
		if res.err != nil {
			t.Fatalf("reconciliation failed: %v", res.err)
		}
		totalSent += res.sent
		totalReceived += res.received
	}

	// B should have received as2
	if !storeB.AttestationExists("as-2") {
		t.Fatal("store B should have received as-2 from A")
	}

	if totalReceived == 0 {
		t.Fatal("expected at least one attestation received")
	}
}

func TestPeer_BothHaveUnique(t *testing.T) {
	connA, connB := connPair()
	treeA, treeB := NewTree(), NewTree()
	storeA, storeB := newMemStore(), newMemStore()
	logger := testLogger()

	// A has as1, B has as2 — different groups
	as1 := makeAs("as-1", "user-1", "member", "team-a", "actor-a")
	insertWithTree(storeA, treeA, as1)

	as2 := makeAs("as-2", "user-2", "admin", "team-b", "actor-b")
	insertWithTree(storeB, treeB, as2)

	peerA := NewPeer(connA, treeA, storeA, logger)
	peerB := NewPeer(connB, treeB, storeB, logger)

	ctx := context.Background()

	type result struct {
		sent, received int
		err            error
	}
	ch := make(chan result, 2)

	go func() {
		s, r, err := peerA.Reconcile(ctx)
		ch <- result{s, r, err}
	}()
	go func() {
		s, r, err := peerB.Reconcile(ctx)
		ch <- result{s, r, err}
	}()

	for i := 0; i < 2; i++ {
		res := <-ch
		if res.err != nil {
			t.Fatalf("reconciliation failed: %v", res.err)
		}
	}

	// Both should now have both attestations
	if !storeA.AttestationExists("as-2") {
		t.Fatal("store A should have received as-2 from B")
	}
	if !storeB.AttestationExists("as-1") {
		t.Fatal("store B should have received as-1 from A")
	}
}

func TestPeer_EmptyTrees(t *testing.T) {
	connA, connB := connPair()
	treeA, treeB := NewTree(), NewTree()
	storeA, storeB := newMemStore(), newMemStore()
	logger := testLogger()

	peerA := NewPeer(connA, treeA, storeA, logger)
	peerB := NewPeer(connB, treeB, storeB, logger)

	ctx := context.Background()

	type result struct {
		sent, received int
		err            error
	}
	ch := make(chan result, 2)

	go func() {
		s, r, err := peerA.Reconcile(ctx)
		ch <- result{s, r, err}
	}()
	go func() {
		s, r, err := peerB.Reconcile(ctx)
		ch <- result{s, r, err}
	}()

	for i := 0; i < 2; i++ {
		res := <-ch
		if res.err != nil {
			t.Fatalf("reconciliation failed: %v", res.err)
		}
		if res.sent != 0 || res.received != 0 {
			t.Fatal("empty trees should exchange nothing")
		}
	}
}

func TestWireRoundtrip(t *testing.T) {
	as := makeAs("as-roundtrip", "user-1", "member", "team", "hr")
	as.Attributes = map[string]interface{}{"color": "blue"}

	wire := toWire(as)
	back, err := fromWire(wire)
	if err != nil {
		t.Fatalf("fromWire failed: %v", err)
	}

	if back.ID != as.ID {
		t.Fatalf("ID mismatch: %s vs %s", back.ID, as.ID)
	}
	if back.Subjects[0] != as.Subjects[0] {
		t.Fatal("subject mismatch")
	}
	if !back.Timestamp.Equal(as.Timestamp) {
		t.Fatalf("timestamp mismatch: %v vs %v", back.Timestamp, as.Timestamp)
	}
	if back.Attributes["color"] != "blue" {
		t.Fatal("attributes not preserved")
	}
}

func TestHexToHash_Roundtrip(t *testing.T) {
	original := ContentHash(makeAs("as-1", "s", "p", "c", "a"))
	hexStr := HexHash(original)
	recovered := hexToHash(hexStr)

	if original != recovered {
		t.Fatalf("hex roundtrip failed: %x vs %x", original, recovered)
	}
}
