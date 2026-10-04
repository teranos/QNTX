//go:build rustsqlite

package sqlitecgo_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/db/rustdriver"
	"github.com/teranos/QNTX/server/parity"
)

// TestSQLiteIsHeldToItsPin asks the SQLite ats-sqlite links which version it
// is, and holds the answer to server/parity/sqlite_<version>_<rev>.
func TestSQLiteIsHeldToItsPin(t *testing.T) {
	pinned, err := parity.Pinned("../../../server/parity", "sqlite")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "pin.db")
	store, err := sqlitecgo.NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore(%s): %v", path, err)
	}
	defer store.Close()

	const driver = "rustsqlite-engine-pin"
	rustdriver.RegisterNamed(driver, "engine-pin", store.StorePtr(), store.ReadConnPtr(), store.Mu(), store.MuRead())
	db, err := sql.Open(driver, path)
	if err != nil {
		t.Fatalf("sql.Open(%s, %s): %v", driver, path, err)
	}
	defer db.Close()

	var linked string
	if err := db.QueryRow("SELECT sqlite_version()").Scan(&linked); err != nil {
		t.Fatalf("SELECT sqlite_version() through %s: %v", driver, err)
	}
	if linked != pinned {
		t.Errorf("ats-sqlite links SQLite %s, the pin is %s", linked, pinned)
	}
}
