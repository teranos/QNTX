//go:build cgo && rustsqlite

package syscap

import (
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
)

// storageAvailable returns true when Rust SQLite backend is available
func storageAvailable() bool {
	return true
}

// storageBackendVersion returns the qntx-sqlite library version
func storageBackendVersion() string {
	return sqlitecgo.Version()
}
