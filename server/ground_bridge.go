package server

import (
	"database/sql"
	"encoding/json"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// writeToGround inserts an attestation directly into Ground's database.
// This bridges the gap between QNTX's per-clone database and Ground's
// standalone database at ~/.local/share/ground/ground.db.
func writeToGround(dbPath string, as *types.As, logger *zap.SugaredLogger) {
	if dbPath == "" {
		return
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		logger.Warnw("Failed to open Ground db", "path", dbPath, "error", err)
		return
	}
	defer db.Close()

	subjects, _ := json.Marshal(as.Subjects)
	predicates, _ := json.Marshal(as.Predicates)
	contexts, _ := json.Marshal(as.Contexts)
	actors, _ := json.Marshal(as.Actors)
	attributes, _ := json.Marshal(as.Attributes)

	_, err = db.Exec(`INSERT OR IGNORE INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		as.ID,
		string(subjects),
		string(predicates),
		string(contexts),
		string(actors),
		as.Timestamp.UTC().Format("2006-01-02 15:04:05"),
		as.Source,
		string(attributes),
		as.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		logger.Warnw("Failed to write to Ground db",
			"path", dbPath, "asid", as.ID, "error", errors.Wrap(err, "ground db insert failed"))
		return
	}

	logger.Infow("Wrote deferred news to Ground db", "path", dbPath, "asid", as.ID)
}
