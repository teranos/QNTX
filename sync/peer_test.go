package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"

	"go.uber.org/zap"
)

// chanConn implements Conn over a pair of channels for in-process testing.
// Messages are JSON-serialized through the channels to match real WebSocket behavior.
type chanConn struct {
	in   chan json.RawMessage
	out  chan json.RawMessage
	done chan struct{}
	once sync.Once
}

func (c *chanConn) ReadJSON(v interface{}) error {
	select {
	case raw, ok := <-c.in:
		if !ok {
			return fmt.Errorf("connection closed")
		}
		return json.Unmarshal(raw, v)
	case <-c.done:
		return fmt.Errorf("connection closed")
	}
}

func (c *chanConn) WriteJSON(v interface{}) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	select {
	case c.out <- raw:
		return nil
	case <-c.done:
		return fmt.Errorf("connection closed")
	}
}

func (c *chanConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

// connPair creates two connected Conn implementations for testing.
func connPair() (Conn, Conn) {
	ab := make(chan json.RawMessage, 32)
	ba := make(chan json.RawMessage, 32)
	return &chanConn{in: ba, out: ab, done: make(chan struct{})},
		&chanConn{in: ab, out: ba, done: make(chan struct{})}
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

// --------------------------------------------------------------------------
// memTree: in-memory SyncTree for protocol testing.
//
// Uses Go's SHA-256 for hashing. The hashes won't match Rust's output, but
// the protocol tests verify message exchange and attestation transfer — not
// hash correctness. Hash correctness is tested by `cargo test` on the Rust side.
// --------------------------------------------------------------------------

type memTreeGroup struct {
	actor, context string
	leaves         map[string]bool // content hash hex → present
}

type memTree struct {
	mu     sync.Mutex
	groups map[string]*memTreeGroup // group key hash hex → group
}

func newMemTree() *memTree {
	return &memTree{groups: make(map[string]*memTreeGroup)}
}

func (t *memTree) groupKeyHash(actor, ctx string) string {
	h := sha256.New()
	h.Write([]byte("gk:"))
	h.Write([]byte(actor))
	h.Write([]byte("\x00"))
	h.Write([]byte(ctx))
	return hex.EncodeToString(h.Sum(nil))
}

func (t *memTree) groupHash(g *memTreeGroup) string {
	hashes := make([]string, 0, len(g.leaves))
	for h := range g.leaves {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)

	hasher := sha256.New()
	hasher.Write([]byte("grp:"))
	hasher.Write([]byte(g.actor))
	hasher.Write([]byte("\x00"))
	hasher.Write([]byte(g.context))
	hasher.Write([]byte("\x00"))
	for _, h := range hashes {
		hasher.Write([]byte(h))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func (t *memTree) Root() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.groups) == 0 {
		return "0000000000000000000000000000000000000000000000000000000000000000", nil
	}

	ghashes := make([]string, 0, len(t.groups))
	for _, g := range t.groups {
		ghashes = append(ghashes, t.groupHash(g))
	}
	sort.Strings(ghashes)

	h := sha256.New()
	h.Write([]byte("root:"))
	for _, gh := range ghashes {
		h.Write([]byte(gh))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (t *memTree) GroupHashes() (map[string]string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := make(map[string]string, len(t.groups))
	for gkh, g := range t.groups {
		result[gkh] = t.groupHash(g)
	}
	return result, nil
}

func (t *memTree) Diff(remoteGroups map[string]string) (localOnly, remoteOnly, divergent []string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for gkh, g := range t.groups {
		rgh, exists := remoteGroups[gkh]
		if !exists {
			localOnly = append(localOnly, gkh)
		} else if rgh != t.groupHash(g) {
			divergent = append(divergent, gkh)
		}
	}

	for gkh := range remoteGroups {
		if _, exists := t.groups[gkh]; !exists {
			remoteOnly = append(remoteOnly, gkh)
		}
	}
	return
}

func (t *memTree) Contains(contentHashHex string) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, g := range t.groups {
		if g.leaves[contentHashHex] {
			return true, nil
		}
	}
	return false, nil
}

func (t *memTree) FindGroupKey(gkhHex string) (actor, context string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	g, ok := t.groups[gkhHex]
	if !ok {
		return "", "", fmt.Errorf("group not found: %s", gkhHex)
	}
	return g.actor, g.context, nil
}

func (t *memTree) Insert(actor, context, contentHashHex string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	gkh := t.groupKeyHash(actor, context)
	g, ok := t.groups[gkh]
	if !ok {
		g = &memTreeGroup{
			actor:   actor,
			context: context,
			leaves:  make(map[string]bool),
		}
		t.groups[gkh] = g
	}
	g.leaves[contentHashHex] = true
	return nil
}

func (t *memTree) ContentHash(attestationJSON string) (string, error) {
	// Simple deterministic hash of the JSON — doesn't match Rust, but
	// that's fine for protocol tests.
	h := sha256.Sum256([]byte(attestationJSON))
	return hex.EncodeToString(h[:]), nil
}

// --------------------------------------------------------------------------
// Test helpers
// --------------------------------------------------------------------------

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

func insertWithTree(store *memStore, tree SyncTree, as *types.As) {
	store.CreateAttestation(as)
	aj, _ := attestationJSON(as)
	chHex, _ := tree.ContentHash(aj)
	for _, actor := range as.Actors {
		for _, ctx := range as.Contexts {
			tree.Insert(actor, ctx, chHex)
		}
	}
}

// --------------------------------------------------------------------------
// Tests
// --------------------------------------------------------------------------

func TestPeer_AlreadyInSync(t *testing.T) {
	connA, connB := connPair()
	treeA, treeB := newMemTree(), newMemTree()
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
	treeA, treeB := newMemTree(), newMemTree()
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
	treeA, treeB := newMemTree(), newMemTree()
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
	treeA, treeB := newMemTree(), newMemTree()
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

func TestPeer_ContextCancellation(t *testing.T) {
	connA, _ := connPair()
	tree := newMemTree()
	store := newMemStore()
	logger := testLogger()

	// Insert data so roots won't match (forces the protocol past hello)
	insertWithTree(store, tree, makeAs("as-1", "user-1", "member", "team", "hr"))

	peer := NewPeer(connA, tree, store, logger)

	// Short deadline — the other side never responds
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, _, err := peer.Reconcile(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestAttestationJSON_TimestampMillis(t *testing.T) {
	as := makeAs("as-ts", "s", "p", "c", "a")
	as.Timestamp = time.UnixMilli(1718452800000)

	j, err := attestationJSON(as)
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(j), &parsed); err != nil {
		t.Fatal(err)
	}

	// Timestamp should be a number (milliseconds), not a string
	ts, ok := parsed["timestamp"].(float64)
	if !ok {
		t.Fatalf("timestamp should be a number, got %T", parsed["timestamp"])
	}
	if int64(ts) != 1718452800000 {
		t.Fatalf("timestamp should be 1718452800000, got %v", ts)
	}
}

// --------------------------------------------------------------------------
// TODO: Spike tests — protocol failure modes
//
// Spike (edge cases, adversarial inputs, failure recovery):
//
// - Connection drop mid-attestation transfer: close the Conn after sending
//   MsgNeed but before MsgAttestations. Verify Reconcile returns error,
//   no partial state corruption in the store.
//
// - Malformed messages: peer sends invalid JSON, wrong message type at
//   each phase (e.g. MsgAttestations where MsgHello expected), missing
//   required fields. Verify clean error, no panic.
//
// - Peer sends Need for groups that don't exist: fabricated group key
//   hashes. Verify FindGroupKey misses are handled, empty attestation
//   response sent.
//
// - Sync limits: peer requests >maxGroupsPerSync groups. Verify truncation
//   and warning log. Peer triggers >maxAttestationsPerSync. Verify cap.
//
// --------------------------------------------------------------------------
// TODO: Jenny tests — complex real-world scenarios
//
// Jenny (multi-step, stateful, realistic):
//
// - Large-scale sync: 1000+ divergent groups, verify convergence and that
//   both sides end up with the union of attestations.
//
// - Multi-hop convergence: three nodes (A, B, C). A syncs with B, B syncs
//   with C. Verify C gets A's attestations through B without direct contact.
//
// - Repeated sync idempotency: sync twice in a row. Second sync should
//   match roots immediately (zero transfer).
//
// - Intermittent connectivity (tube scenario): sync, add attestations
//   on both sides, sync again. Verify incremental reconciliation works.
// --------------------------------------------------------------------------
