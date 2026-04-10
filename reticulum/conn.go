package reticulum

import (
	"encoding/json"

	"github.com/teranos/QNTX/errors"
)

// LinkHandle identifies an active Reticulum link in the Leviculum runtime.
// Opaque to Go — the Rust/WASM side manages link lifecycle, encryption,
// and transport. Go sees only the handle.
type LinkHandle uint64

// Conn implements sync.Conn over a Reticulum link.
//
// The sync protocol sends JSON messages (sync_hello, sync_group_hashes,
// sync_need, sync_attestations, sync_done). Conn marshals them to bytes,
// passes them through the Reticulum link's encrypted channel, and
// unmarshals on the other side.
//
// The Reticulum link provides end-to-end encryption with forward secrecy.
// The sync protocol doesn't need to know — it sees ReadJSON/WriteJSON/Close.
type Conn struct {
	link   LinkHandle
	bridge LinkBridge
}

// LinkBridge is the interface to the Leviculum runtime.
// Implemented by the WASM/FFI layer that wraps Leviculum.
//
// TODO(LNK): Implement via wazero calls to qntx-core + leviculum.
type LinkBridge interface {
	// Send writes raw bytes to the Reticulum link's encrypted channel.
	Send(link LinkHandle, data []byte) error

	// Recv reads raw bytes from the Reticulum link's encrypted channel.
	// Blocks until data is available or the link closes.
	Recv(link LinkHandle) ([]byte, error)

	// Close tears down the Reticulum link.
	Close(link LinkHandle) error
}

// NewConn wraps a Reticulum link as a sync.Conn.
func NewConn(link LinkHandle, bridge LinkBridge) *Conn {
	return &Conn{link: link, bridge: bridge}
}

// ReadJSON reads a JSON message from the Reticulum link.
// Satisfies the sync.Conn interface.
func (c *Conn) ReadJSON(v interface{}) error {
	data, err := c.bridge.Recv(c.link)
	if err != nil {
		return errors.Wrap(err, "reticulum link recv failed")
	}
	if err := json.Unmarshal(data, v); err != nil {
		return errors.Wrapf(err, "reticulum link unmarshal failed (%d bytes)", len(data))
	}
	return nil
}

// WriteJSON writes a JSON message to the Reticulum link.
// Satisfies the sync.Conn interface.
func (c *Conn) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return errors.Wrap(err, "reticulum link marshal failed")
	}
	if err := c.bridge.Send(c.link, data); err != nil {
		return errors.Wrapf(err, "reticulum link send failed (%d bytes)", len(data))
	}
	return nil
}

// Close closes the Reticulum link.
// Satisfies the sync.Conn interface.
func (c *Conn) Close() error {
	return c.bridge.Close(c.link)
}
