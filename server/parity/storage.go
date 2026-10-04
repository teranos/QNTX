package parity

import (
	_ "embed"
	"encoding/json"

	"github.com/teranos/errors"
)

// "Make parity would just be for the storage backend specifically." make parity
// reads the source, before there is a node, and writes what it read here; the
// node embeds it, as it does the OpenAPI document, so the storage sigil says what
// the source this build was made from persists, and never this node's own
// stores.

// Stored is one thing QNTX persists, as make parity read it.
type Stored struct {
	Name     string `json:"name"`
	SQLite   bool   `json:"sqlite"`
	DuckDB   bool   `json:"duckdb"`
	Postgres bool   `json:"postgres"`
	// Rebuilt rows cascade from attestations, so a take-in rebuilds them.
	Rebuilt bool `json:"rebuilt"`
	// Sites are the Go files that reach this thing with hand-written SQL. Files
	// and not lines: a line moves with every edit above it, and the written
	// file would be stale for an edit that changed nothing it says.
	Sites []string `json:"sites"`
}

// StorageFile is where make parity writes, relative to the checkout.
const StorageFile = "server/parity/storage.json"

//go:embed storage.json
var storage []byte

// Storage is what make parity wrote when this build's source was read.
func Storage() ([]Stored, error) {
	var things []Stored
	if err := json.Unmarshal(storage, &things); err != nil {
		return nil, errors.Wrapf(err, "%s did not parse", StorageFile)
	}
	return things, nil
}
