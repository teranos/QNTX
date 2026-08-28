package server

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
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

	// An encode that failed leaves nil, and string(nil) is "", so the row goes
	// in naming no subjects and reads as an attestation about nothing.
	var encodeErr error
	encode := func(field string, v any) string {
		if encodeErr != nil {
			return ""
		}
		b, err := json.Marshal(v)
		if err != nil {
			encodeErr = errors.Wrapf(err, "failed to encode the %s of %s", field, as.ID)
			return ""
		}
		return string(b)
	}

	subjects := encode("subjects", as.Subjects)
	predicates := encode("predicates", as.Predicates)
	contexts := encode("contexts", as.Contexts)
	actors := encode("actors", as.Actors)
	attributes := encode("attributes", as.Attributes)
	if encodeErr != nil {
		logger.Warnw("Attestation not written to Ground", "id", as.ID, "error", encodeErr)
		return
	}

	_, err = db.Exec(`INSERT OR IGNORE INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		as.ID,
		subjects,
		predicates,
		contexts,
		actors,
		as.Timestamp.UTC().Format("2006-01-02 15:04:05"),
		as.Source,
		attributes,
		as.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		logger.Warnw("Failed to write to Ground db",
			"path", dbPath, "asid", as.ID, "error", errors.Wrap(err, "ground db insert failed"))
		return
	}

	logger.Debugw("Wrote news to Ground db", "path", dbPath, "asid", as.ID, "predicate", as.Predicates[0])
}

// cachedProjectCtx is computed once at init from the startup working directory.
// os.Getwd() can shift during shutdown, so we freeze it early.
var cachedProjectCtx string

func init() {
	// Without a working directory this becomes "project:./.", which every
	// clone would then share — one namespace for attestations from anywhere.
	cwd, err := os.Getwd()
	if err != nil {
		cachedProjectCtx = "project:unknown"
		return
	}
	cachedProjectCtx = "project:" + filepath.Join(filepath.Base(filepath.Dir(cwd)), filepath.Base(cwd))
}

// writeGroundNews writes an attestation to Ground's database with the given
// predicate prefix ("deferred:" or "immediate:").
func writeGroundNews(dbPath string, prefix, subject, predicate, actor, detail string, extraAttrs map[string]interface{}, logger *zap.SugaredLogger) {
	if dbPath == "" {
		return
	}

	projectCtx := cachedProjectCtx
	fullPred := prefix + predicate

	asid, err := identity.GenerateASUID("AS", subject, fullPred, projectCtx)
	if err != nil {
		logger.Warnw("Failed to generate ASID for ground news", "predicate", fullPred, "error", err)
		return
	}

	attrs := map[string]interface{}{
		"detail": detail,
		"after":  time.Now().Unix(),
	}
	for k, v := range extraAttrs {
		attrs[k] = v
	}

	now := time.Now()
	writeToGround(dbPath, &types.As{
		ID:         asid,
		Subjects:   []string{subject},
		Predicates: []string{fullPred},
		Contexts:   []string{projectCtx},
		Actors:     []string{actor},
		Timestamp:  now,
		Source:     actor,
		Attributes: attrs,
		CreatedAt:  now,
	}, logger)
}

// WriteDeferredNews writes a deferred news attestation to Ground's database.
// Delivered at the next Stop hook after the "after" timestamp passes.
func WriteDeferredNews(dbPath string, subject, predicate, actor, detail string, extraAttrs map[string]interface{}, logger *zap.SugaredLogger) {
	writeGroundNews(dbPath, "deferred:", subject, predicate, actor, detail, extraAttrs, logger)
}

// WriteImmediateNews writes an immediate news attestation to Ground's database.
// Delivered in real time by Ground's asyncRewake watcher polling every 2s.
func WriteImmediateNews(dbPath string, subject, predicate, actor, detail string, extraAttrs map[string]interface{}, logger *zap.SugaredLogger) {
	writeGroundNews(dbPath, "immediate:", subject, predicate, actor, detail, extraAttrs, logger)
}
