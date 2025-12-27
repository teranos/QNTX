package schedule

import (
	"database/sql"
	"testing"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// createTestDB creates an in-memory test database.
func createTestDB(t *testing.T) *sql.DB {
	return qntxtest.CreateTestDB(t)
}
