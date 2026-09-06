//go:build cgo && rustduckdb

package duckdbcgo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// A User written through the FFI is read back whole: what Tim de Facile said
// about himself is what the object holds, across the Go and Rust records.
func TestAUserRoundTripsWhole(t *testing.T) {
	store, err := NewUserStore("file://" + t.TempDir())
	require.NoError(t, err)
	t.Cleanup(store.Close)

	tim := auth.User{
		ID:             "US-TIM-1",
		DisplayName:    "Tim de Facile",
		EmailAddresses: []string{"tim@example.com"},
		PhoneNumbers:   []string{"+31612345678", "0201234567"},
		Level:          auth.LevelRoot,
		Keys:           []auth.UserKey{{DID: "did:key:zTim", Origin: auth.OriginBrowser}},
		CreatedAt:      1,
	}
	require.NoError(t, store.Put(tim))

	found, ok, err := store.ByRoute("did:key:zTim")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, tim.DisplayName, found.DisplayName)
	assert.Equal(t, tim.EmailAddresses, found.EmailAddresses)
	assert.Equal(t, tim.PhoneNumbers, found.PhoneNumbers)
}

// A User who has said nothing has nil lists in Go, and the object must still
// read back: null is an empty list on the Rust side, never a type error.
func TestAUserWhoSaidNothingRoundTrips(t *testing.T) {
	store, err := NewUserStore("file://" + t.TempDir())
	require.NoError(t, err)
	t.Cleanup(store.Close)

	require.NoError(t, store.Put(auth.User{
		ID:        "US-QUIET-1",
		Level:     auth.LevelRoot,
		Keys:      []auth.UserKey{{DID: "did:key:zQuiet", Origin: auth.OriginBrowser}},
		CreatedAt: 1,
	}))

	found, ok, err := store.ByRoute("did:key:zQuiet")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Empty(t, found.EmailAddresses)
	assert.Empty(t, found.PhoneNumbers)
}
