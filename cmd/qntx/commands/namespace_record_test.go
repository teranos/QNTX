//go:build cgo && rustduckdb && !quickdev

package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/errors"
)

// "what I want is for a Namespace to begin life dbless and enable either
// SQLite or parquet or Supabase later on"
func TestANamespaceWithNoStorageIsNotOpened(t *testing.T) {
	h := openedParquetNode(t)

	_, err := h.OpenNamespace("pond", storage.NamespaceRecord{Kind: storage.RecordNone})

	var storeless namespaces.NoStorage
	require.True(t, errors.As(err, &storeless), "opening a namespace with no storage answered %v", err)
	_, landed := h.landings["pond"]
	assert.False(t, landed, "a namespace with no storage was given a landing file")
}

// A sqlite namespace is its landing file and sends to nothing: a write is
// kept there, on this node.
func TestASqliteNamespaceKeepsItsWritesInItsFile(t *testing.T) {
	h := openedParquetNode(t)

	_, err := h.OpenNamespace("pond", storage.NamespaceRecord{Kind: storage.RecordSQLite})
	require.NoError(t, err)
	at := time.UnixMilli(1_700_000_000_000)
	require.NoError(t, h.landings["pond"].CreateAttestation(&types.As{ID: "AS-kept", Subjects: []string{"ALICE"},
		Predicates: []string{"knows"}, Contexts: []string{"work"}, Actors: []string{"human:bob"},
		Timestamp: at, Source: "test", CreatedAt: at}))
	h.CloseNamespace("pond")

	_, err = h.OpenNamespace("pond", storage.NamespaceRecord{Kind: storage.RecordSQLite})
	require.NoError(t, err)
	t.Cleanup(func() { h.CloseNamespace("pond") })
	assert.True(t, h.landings["pond"].AttestationExists("AS-kept"))
	_, sent := h.records["pond"]
	assert.False(t, sent, "a sqlite namespace was given a record to send to")
}

func TestARecordOfAKindNobodyNamedIsRefused(t *testing.T) {
	h := openedParquetNode(t)

	_, err := h.OpenNamespace("pond", storage.NamespaceRecord{Kind: "mysql"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mysql")
}
