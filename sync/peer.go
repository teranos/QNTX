package sync

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"

	"go.uber.org/zap"
)

const timestampFormat = time.RFC3339Nano

// Conn abstracts the WebSocket connection for testability.
// The real implementation wraps gorilla/websocket; tests use a channel pair.
type Conn interface {
	ReadJSON(v interface{}) error
	WriteJSON(v interface{}) error
	Close() error
}

// Peer manages one sync session with a remote QNTX instance.
// Both sides of the connection run the same code — the protocol is symmetric.
type Peer struct {
	conn   Conn
	tree   SyncTree
	store  ats.AttestationStore
	logger *zap.SugaredLogger

	// Stats tracked during reconciliation
	sent     int
	received int
}

// NewPeer creates a sync peer for a single reconciliation session.
func NewPeer(conn Conn, tree SyncTree, store ats.AttestationStore, logger *zap.SugaredLogger) *Peer {
	return &Peer{
		conn:   conn,
		tree:   tree,
		store:  store,
		logger: logger,
	}
}

// Reconcile runs the full sync protocol. Both peers call this concurrently
// on their respective ends of the connection. Returns the number of
// attestations sent and received.
//
// The protocol is symmetric: each side sends its state, computes what the
// other needs, and sends it. No leader election, no request/response — both
// sides act simultaneously.
func (p *Peer) Reconcile(ctx context.Context) (sent, received int, err error) {
	// Phase 1: Exchange root hashes
	root, err := p.tree.Root()
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to compute sync root")
	}

	if err := p.send(Msg{
		Type:     MsgHello,
		RootHash: root,
	}); err != nil {
		return 0, 0, errors.Wrap(err, "failed to send sync hello")
	}

	var hello Msg
	if err := p.recv(&hello); err != nil {
		return 0, 0, errors.Wrap(err, "failed to receive sync hello")
	}

	if hello.Type != MsgHello {
		return 0, 0, errors.Newf("expected sync_hello, got %s", hello.Type)
	}

	// Roots match — fully synced
	if hello.RootHash == root {
		p.logger.Debugw("Sync roots match, already in sync")
		if err := p.sendDone(); err != nil {
			return 0, 0, err
		}
		return 0, 0, nil
	}

	p.logger.Debugw("Sync roots differ, starting reconciliation",
		"local_root", root,
		"remote_root", hello.RootHash,
	)

	// Phase 2: Exchange group hashes
	// GroupHashes() already returns hex strings — no conversion needed
	localGroups, err := p.tree.GroupHashes()
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to get group hashes")
	}

	if err := p.send(Msg{
		Type:   MsgGroupHashes,
		Groups: localGroups,
	}); err != nil {
		return 0, 0, errors.Wrap(err, "failed to send group hashes")
	}

	var remoteGroupsMsg Msg
	if err := p.recv(&remoteGroupsMsg); err != nil {
		return 0, 0, errors.Wrap(err, "failed to receive group hashes")
	}

	if remoteGroupsMsg.Type != MsgGroupHashes {
		return 0, 0, errors.Newf("expected sync_group_hashes, got %s", remoteGroupsMsg.Type)
	}

	// Phase 3: Compute diff and exchange needs
	// Diff takes hex strings directly — the WASM module handles all byte ↔ hex
	_, remoteOnly, divergent, err := p.tree.Diff(remoteGroupsMsg.Groups)
	if err != nil {
		return 0, 0, errors.Wrap(err, "failed to compute sync diff")
	}

	// We need attestations from groups we don't have, and divergent groups
	needed := make([]string, 0, len(remoteOnly)+len(divergent))
	needed = append(needed, remoteOnly...)
	needed = append(needed, divergent...)

	if err := p.send(Msg{
		Type: MsgNeed,
		Need: needed,
	}); err != nil {
		return 0, 0, errors.Wrap(err, "failed to send sync need")
	}

	// Phase 4: Receive their needs and send attestations
	var needMsg Msg
	if err := p.recv(&needMsg); err != nil {
		return 0, 0, errors.Wrap(err, "failed to receive sync need")
	}

	if needMsg.Type != MsgNeed {
		return 0, 0, errors.Newf("expected sync_need, got %s", needMsg.Type)
	}

	// Fulfill their request — send attestations they need
	if err := p.sendRequestedAttestations(ctx, needMsg.Need); err != nil {
		return 0, 0, errors.Wrap(err, "failed to send requested attestations")
	}

	// Receive attestations we requested
	if len(needed) > 0 {
		if err := p.receiveAttestations(ctx); err != nil {
			return 0, 0, errors.Wrap(err, "failed to receive attestations")
		}
	} else {
		// We didn't need anything, but still need to read their
		// attestations message (might be empty) to keep the protocol in sync
		var attMsg Msg
		if err := p.recv(&attMsg); err != nil {
			return 0, 0, errors.Wrap(err, "failed to receive attestations")
		}
	}

	if err := p.sendDone(); err != nil {
		return 0, 0, err
	}

	p.logger.Infow("Sync reconciliation complete",
		"sent", p.sent,
		"received", p.received,
	)

	return p.sent, p.received, nil
}

// sendRequestedAttestations looks up attestations for the groups the peer
// requested and sends them.
func (p *Peer) sendRequestedAttestations(ctx context.Context, requestedHexKeys []string) error {
	atts := make(map[string][]AttestationWire)

	for _, hexKey := range requestedHexKeys {
		// Reverse-lookup: group key hash → (actor, context)
		actor, actCtx, err := p.tree.FindGroupKey(hexKey)
		if err != nil {
			continue // We don't have this group
		}

		// Query attestations for this actor+context pair
		results, err := p.store.GetAttestations(ats.AttestationFilter{
			Actors:   []string{actor},
			Contexts: []string{actCtx},
		})
		if err != nil {
			p.logger.Warnw("Failed to query attestations for sync",
				"actor", actor,
				"context", actCtx,
				"error", err,
			)
			continue
		}

		wires := make([]AttestationWire, 0, len(results))
		for _, as := range results {
			wires = append(wires, toWire(as))
		}
		atts[hexKey] = wires
		p.sent += len(wires)
	}

	return p.send(Msg{
		Type:         MsgAttestations,
		Attestations: atts,
	})
}

// receiveAttestations reads the attestations message from the peer and
// persists them to the local store.
func (p *Peer) receiveAttestations(ctx context.Context) error {
	var msg Msg
	if err := p.recv(&msg); err != nil {
		return err
	}

	if msg.Type != MsgAttestations {
		return errors.Newf("expected sync_attestations, got %s", msg.Type)
	}

	for _, wires := range msg.Attestations {
		for _, w := range wires {
			as, err := fromWire(w)
			if err != nil {
				p.logger.Warnw("Failed to parse synced attestation",
					"id", w.ID,
					"error", err,
				)
				continue
			}

			// Skip if we already have this attestation by ASID
			if p.store.AttestationExists(as.ID) {
				continue
			}

			// Check by content hash too — same claim might have different ASID
			aj, err := attestationJSON(as)
			if err != nil {
				p.logger.Warnw("Failed to serialize attestation for content hash",
					"id", as.ID,
					"error", err,
				)
				continue
			}
			chHex, err := p.tree.ContentHash(aj)
			if err != nil {
				p.logger.Warnw("Failed to compute content hash for synced attestation",
					"id", as.ID,
					"error", err,
				)
				continue
			}

			exists, err := p.tree.Contains(chHex)
			if err != nil {
				p.logger.Warnw("Failed to check content hash existence",
					"id", as.ID,
					"error", err,
				)
				continue
			}
			if exists {
				continue
			}

			if err := p.store.CreateAttestation(as); err != nil {
				p.logger.Warnw("Failed to persist synced attestation",
					"id", as.ID,
					"subjects", as.Subjects,
					"error", err,
				)
				continue
			}

			p.received++
		}
	}

	return nil
}

func (p *Peer) send(msg Msg) error {
	return p.conn.WriteJSON(msg)
}

func (p *Peer) recv(msg *Msg) error {
	return p.conn.ReadJSON(msg)
}

func (p *Peer) sendDone() error {
	return p.send(Msg{
		Type:     MsgDone,
		Sent:     p.sent,
		Received: p.received,
	})
}

// toWire converts an attestation to its wire format.
func toWire(as *types.As) AttestationWire {
	return AttestationWire{
		ID:         as.ID,
		Subjects:   as.Subjects,
		Predicates: as.Predicates,
		Contexts:   as.Contexts,
		Actors:     as.Actors,
		Timestamp:  as.Timestamp.Format(timestampFormat),
		Source:     as.Source,
		Attributes: as.Attributes,
	}
}

// fromWire converts a wire attestation back to a types.As.
func fromWire(w AttestationWire) (*types.As, error) {
	ts, err := time.Parse(timestampFormat, w.Timestamp)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse timestamp %q for attestation %s", w.Timestamp, w.ID)
	}

	return &types.As{
		ID:         w.ID,
		Subjects:   w.Subjects,
		Predicates: w.Predicates,
		Contexts:   w.Contexts,
		Actors:     w.Actors,
		Timestamp:  ts,
		Source:     w.Source,
		Attributes: w.Attributes,
		CreatedAt:  time.Now(),
	}, nil
}

// attestationJSON serializes a types.As to JSON matching Rust's Attestation struct.
// Critical: timestamp is i64 milliseconds (UnixMilli), not nanoseconds or RFC3339.
// This JSON is passed to SyncTree.ContentHash() which delegates to Rust.
func attestationJSON(as *types.As) (string, error) {
	v := struct {
		ID         string                 `json:"id"`
		Subjects   []string               `json:"subjects"`
		Predicates []string               `json:"predicates"`
		Contexts   []string               `json:"contexts"`
		Actors     []string               `json:"actors"`
		Timestamp  int64                  `json:"timestamp"`
		Source     string                 `json:"source"`
		Attributes map[string]interface{} `json:"attributes,omitempty"`
	}{
		ID:         as.ID,
		Subjects:   as.Subjects,
		Predicates: as.Predicates,
		Contexts:   as.Contexts,
		Actors:     as.Actors,
		Timestamp:  as.Timestamp.UnixMilli(),
		Source:     as.Source,
		Attributes: as.Attributes,
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", errors.Wrapf(err, "failed to marshal attestation %s to JSON", as.ID)
	}
	return string(b), nil
}
