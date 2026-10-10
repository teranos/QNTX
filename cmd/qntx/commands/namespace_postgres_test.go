//go:build cgo && rustduckdb && rustpostgres && !quickdev

package commands

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
)

// A write lands in one node's file for the namespace and is sent to Postgres
// as the namespace closes; a second node, opening the namespace on an empty
// file, takes it in.
func TestAPostgresNamespaceSendsAndTakesIn(t *testing.T) {
	_, named := os.LookupEnv("QNTX_POSTGRES_URL")
	require.True(t, named, "QNTX_POSTGRES_URL names no Postgres; run under scripts/with-postgres.sh")
	kept := storage.NamespaceRecord{Kind: storage.RecordPostgres, URL: "env:QNTX_POSTGRES_URL"}
	name := fmt.Sprintf("pond_%d_%d", os.Getpid(), time.Now().UnixNano())

	first := openedParquetNode(t)
	_, err := first.OpenNamespace(name, kept)
	require.NoError(t, err)
	at := time.UnixMilli(1_700_000_000_000)
	require.NoError(t, first.landings[name].CreateAttestation(&types.As{ID: "AS-sent", Subjects: []string{"ALICE"},
		Predicates: []string{"knows"}, Contexts: []string{"work"}, Actors: []string{"human:bob"},
		Timestamp: at, Source: "test", CreatedAt: at}))
	first.CloseNamespace(name)

	second := openedParquetNode(t)
	_, err = second.OpenNamespace(name, kept)
	require.NoError(t, err)
	t.Cleanup(func() { second.CloseNamespace(name) })
	assert.True(t, second.landings[name].AttestationExists("AS-sent"), "the second node did not take in what the first sent")
}
