package grpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3" // sqlite3 driver
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// GroundServer implements the GroundService gRPC server
type GroundServer struct {
	protocol.UnimplementedGroundServiceServer
	dbPath    string
	authToken string
	logger    *zap.SugaredLogger
}

// NewGroundServer creates a new Ground gRPC server
func NewGroundServer(dbPath string, authToken string, logger *zap.SugaredLogger) *GroundServer {
	return &GroundServer{
		dbPath:    dbPath,
		authToken: authToken,
		logger:    logger,
	}
}

// WriteToGround inserts an attestation into Ground's SQLite database
func (s *GroundServer) WriteToGround(ctx context.Context, req *protocol.WriteToGroundRequest) (*protocol.WriteToGroundResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.WriteToGroundResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	if s.dbPath == "" {
		return &protocol.WriteToGroundResponse{
			Success: false,
			Error:   "ground_db_path not configured",
		}, nil
	}

	// Generate ASID from request fields
	subject := "_"
	if len(req.Subjects) > 0 {
		subject = req.Subjects[0]
	}
	predicate := "_"
	if len(req.Predicates) > 0 {
		predicate = req.Predicates[0]
	}
	ctxStr := "_"
	if len(req.Contexts) > 0 {
		ctxStr = req.Contexts[0]
	}
	asid, err := identity.GenerateASUID("AS", subject, predicate, ctxStr)
	if err != nil {
		return &protocol.WriteToGroundResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to generate ASID: %v", err),
		}, nil
	}

	attributes := make(map[string]interface{})
	if req.Attributes != nil {
		attributes = req.Attributes.AsMap()
	}

	now := time.Now()
	as := &types.As{
		ID:         asid,
		Subjects:   req.Subjects,
		Predicates: req.Predicates,
		Contexts:   req.Contexts,
		Actors:     req.Actors,
		Timestamp:  now,
		Source:     req.Source,
		Attributes: attributes,
		CreatedAt:  now,
	}

	if err := writeToGroundDB(s.dbPath, as); err != nil {
		s.logger.Warnw("Failed to write to Ground db via gRPC", "path", s.dbPath, "asid", as.ID, "error", err)
		return &protocol.WriteToGroundResponse{
			Success: false,
			Error:   fmt.Sprintf("ground db write failed: %v", err),
		}, nil
	}

	s.logger.Infow("Wrote deferred news to Ground db via gRPC", "path", s.dbPath, "asid", as.ID, "source", as.Source)

	return &protocol.WriteToGroundResponse{
		Success: true,
	}, nil
}

// writeToGroundDB inserts an attestation into Ground's SQLite database.
func writeToGroundDB(dbPath string, as *types.As) error {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return errors.Wrapf(err, "failed to open Ground db at %s", dbPath)
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
		return errors.Wrapf(err, "ground db insert failed for %s", as.ID)
	}

	return nil
}
