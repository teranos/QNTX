//go:build cgo && rustpostgres && !quickdev

package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/postgrescgo"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/ats/types"
)

// TestAPostgresNodeSendsAndTakesIn: a write lands in one node's file and is
// sent to Postgres; a second node, opening on an empty file, takes it in.
func TestAPostgresNodeSendsAndTakesIn(t *testing.T) {
	url := os.Getenv("QNTX_POSTGRES_URL")
	if url == "" {
		t.Fatal("QNTX_POSTGRES_URL names no Postgres to test against")
	}
	record, err := postgrescgo.NewPostgresStore(url, "", fmt.Sprintf("node%d_%d", os.Getpid(), time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer record.Close()

	node := func(name string) (*sqlitecgo.RustStore, string, storage.FileSentMark) {
		path := filepath.Join(t.TempDir(), name+".db")
		landing, err := sqlitecgo.NewFileStore(path)
		if err != nil {
			t.Fatalf("NewFileStore(%s): %v", path, err)
		}
		t.Cleanup(func() { landing.Close() })
		return landing, path, storage.FileSentMark{Path: path + ".sent"}
	}

	first, path, sent := node("first")
	if err := landOnPostgres(first, record, path, sent); err != nil {
		t.Fatalf("landOnPostgres on an empty file: %v", err)
	}
	at := time.UnixMilli(1_700_000_000_000)
	as := &types.As{ID: "AS-sent", Subjects: []string{"ALICE"}, Predicates: []string{"knows"},
		Contexts: []string{"work"}, Actors: []string{"human:bob"}, Timestamp: at, Source: "test", CreatedAt: at}
	if err := first.CreateAttestation(as); err != nil {
		t.Fatalf("CreateAttestation: %v", err)
	}
	if rows, err := storage.SendOut(first, record, sent); err != nil || rows != 1 {
		t.Fatalf("SendOut = %d, %v; want 1", rows, err)
	}

	second, path, sent := node("second")
	if err := landOnPostgres(second, record, path, sent); err != nil {
		t.Fatalf("landOnPostgres on a second node: %v", err)
	}
	if !second.AttestationExists("AS-sent") {
		t.Error("the second node did not take in what the first sent")
	}
}
