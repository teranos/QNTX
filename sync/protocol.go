package sync

import "github.com/teranos/QNTX/plugin/grpc/protocol"

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
	Name     string `json:"name,omitempty"` // self-identified node name (from [sync] name)

	// GroupHashes: hex-encoded group key hash → hex-encoded group hash
	Groups map[string]string `json:"groups,omitempty"`

	// Need: list of hex-encoded group key hashes the sender wants
	Need []string `json:"need,omitempty"`

	// Attestations: proto attestations grouped by hex group key hash
	Attestations map[string][]*protocol.Attestation `json:"attestations,omitempty"`

	// Stats (on Done): how many attestations were exchanged
	Sent     int `json:"sent,omitempty"`
	Received int `json:"received,omitempty"`

	// Budget (on Done): peer's local spend and cluster limits.
	// Pointer fields so old peers that omit these just get nil.
	BudgetDailyUSD   *float64 `json:"budget_daily_usd,omitempty"`
	BudgetWeeklyUSD  *float64 `json:"budget_weekly_usd,omitempty"`
	BudgetMonthlyUSD *float64 `json:"budget_monthly_usd,omitempty"`

	// Cluster budget limits: each node's configured ceiling.
	// The effective cluster limit is the average across all nodes.
	ClusterDailyLimitUSD   *float64 `json:"cluster_daily_limit_usd,omitempty"`
	ClusterWeeklyLimitUSD  *float64 `json:"cluster_weekly_limit_usd,omitempty"`
	ClusterMonthlyLimitUSD *float64 `json:"cluster_monthly_limit_usd,omitempty"`
}
