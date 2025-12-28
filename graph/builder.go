package graph

import (
	"database/sql"

	"github.com/teranos/QNTX/ats/ax"
	"github.com/teranos/QNTX/ats/storage"
	"go.uber.org/zap"
)

// AxGraphBuilder builds graph structures from Ax query results
type AxGraphBuilder struct {
	db        *sql.DB
	executor  *ax.AxExecutor
	verbosity int
	logger    *zap.SugaredLogger
}

// NewAxGraphBuilder creates a new Ax graph builder.
// Node types are determined purely from attested node_type predicates.
func NewAxGraphBuilder(db *sql.DB, verbosity int, logger *zap.SugaredLogger) (*AxGraphBuilder, error) {
	executor := storage.NewExecutor(db)

	return &AxGraphBuilder{
		db:        db,
		executor:  executor,
		verbosity: verbosity,
		logger:    logger.Named("graph.builder"),
	}, nil
}
