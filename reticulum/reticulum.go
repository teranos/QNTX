// Package reticulum provides Reticulum mesh network transport for QNTX sync.
//
// A QNTX node becomes a Reticulum destination. The node's ed25519 keypair
// (did:key) serves as both the QNTX actor identity and the Reticulum
// destination identity — one key, no mapping layer.
//
// The sync.Conn interface bridges Reticulum links to the reconciliation
// protocol. The same Peer.Reconcile() that runs over WebSocket runs over
// a Reticulum link unchanged.
//
// Transport path:
//
//	sync.Conn → reticulum.Conn → Leviculum (Rust/WASM) → Reticulum network
//
// See docs/vision/reticulum.md for the design vision.
package reticulum

// App is the Reticulum application name for QNTX destinations.
const App = "qntx"

// SyncAspect is the destination aspect for attestation sync.
// A QNTX node's sync destination name is "qntx.sync".
const SyncAspect = "sync"
