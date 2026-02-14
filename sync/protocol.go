package sync

// Sync protocol message types exchanged between QNTX peers.
//
// The reconciliation protocol is symmetric — both sides run the same
// state machine. Neither is "server" or "client"; they're peers.
//
// Protocol flow:
//
//	1. Both send SyncHello (root hash)
//	2. If roots match → SyncDone, zero attestations transferred
//	3. If roots differ → both send SyncGroupHashes (all group hashes)
//	4. Each side computes Diff: local-only, remote-only, divergent
//	5. Each side sends SyncNeed (group keys it wants attestations for)
//	6. Each side sends SyncAttestations for requested groups
//	7. Both send SyncDone

// MsgType identifies the sync protocol message kind.
type MsgType string

const (
	// MsgHello is the initial handshake: "here's my root hash."
	MsgHello MsgType = "sync_hello"

	// MsgGroupHashes carries all (group key hash → group hash) pairs.
	// Sent when roots differ so the peer can compute a Diff.
	MsgGroupHashes MsgType = "sync_group_hashes"

	// MsgNeed requests attestations for specific group key hashes.
	// Sent after computing the diff from received group hashes.
	MsgNeed MsgType = "sync_need"

	// MsgAttestations carries attestations for requested groups.
	MsgAttestations MsgType = "sync_attestations"

	// MsgDone signals reconciliation is complete.
	MsgDone MsgType = "sync_done"
)

// Msg is the envelope for all sync protocol messages.
type Msg struct {
	Type MsgType `json:"type"`

	// Hello
	RootHash string `json:"root_hash,omitempty"`

	// GroupHashes: hex-encoded group key hash → hex-encoded group hash
	Groups map[string]string `json:"groups,omitempty"`

	// Need: list of hex-encoded group key hashes the sender wants
	Need []string `json:"need,omitempty"`

	// Attestations: serialized attestations grouped by hex group key hash
	Attestations map[string][]AttestationWire `json:"attestations,omitempty"`

	// Stats (on Done): how many attestations were exchanged
	Sent     int `json:"sent,omitempty"`
	Received int `json:"received,omitempty"`
}

// AttestationWire is the over-the-wire representation of an attestation.
// Mirrors types.As but with string timestamps for JSON transport.
type AttestationWire struct {
	ID         string                 `json:"id"`
	Subjects   []string               `json:"subjects"`
	Predicates []string               `json:"predicates"`
	Contexts   []string               `json:"contexts"`
	Actors     []string               `json:"actors"`
	Timestamp  string                 `json:"timestamp"`
	Source     string                 `json:"source"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}
