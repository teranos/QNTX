package storage

import (
	"context"
	"database/sql"
	"time"

	"github.com/teranos/QNTX/errors"
	pb "github.com/teranos/QNTX/glyph/proto"
)

// CanvasGlyph represents a glyph on the canvas workspace
type CanvasGlyph struct {
	ID         string    `json:"id"`
	Symbol     string    `json:"symbol"`
	X          int       `json:"x"`
	Y          int       `json:"y"`
	Width      *int      `json:"width,omitempty"`
	Height     *int      `json:"height,omitempty"`
	ResultData *string   `json:"result_data,omitempty"` // JSON for result glyphs
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// CanvasComposition represents a melded composition of glyphs
// Uses edge-based DAG structure to support multi-directional melding
// See ADR-009 for rationale
type CanvasComposition struct {
	ID        string                  `json:"id"`
	Edges     []*pb.CompositionEdge   `json:"edges"`
	X         int                     `json:"x"`
	Y         int                     `json:"y"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

// compositionEdge is an internal struct for database operations
// Maps proto CompositionEdge to database schema
type compositionEdge struct {
	From      string `db:"from_glyph_id"`
	To        string `db:"to_glyph_id"`
	Direction string `db:"direction"`
	Position  int32  `db:"position"`
}

// toProtoEdge converts internal DB struct to proto
func (e *compositionEdge) toProtoEdge() *pb.CompositionEdge {
	return &pb.CompositionEdge{
		From:      e.From,
		To:        e.To,
		Direction: e.Direction,
		Position:  e.Position,
	}
}

// fromProtoEdge converts proto to internal DB struct
func fromProtoEdge(e *pb.CompositionEdge) *compositionEdge {
	return &compositionEdge{
		From:      e.From,
		To:        e.To,
		Direction: e.Direction,
		Position:  e.Position,
	}
}

// CanvasStore provides storage operations for canvas state
type CanvasStore struct {
	db *sql.DB
}

// NewCanvasStore creates a new canvas store
func NewCanvasStore(db *sql.DB) *CanvasStore {
	return &CanvasStore{db: db}
}

// === Glyph operations ===

// UpsertGlyph creates or updates a glyph
func (s *CanvasStore) UpsertGlyph(ctx context.Context, glyph *CanvasGlyph) error {
	now := time.Now()
	if glyph.CreatedAt.IsZero() {
		glyph.CreatedAt = now
	}
	glyph.UpdatedAt = now

	query := `
		INSERT INTO canvas_glyphs (id, symbol, x, y, width, height, result_data, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			symbol = excluded.symbol,
			x = excluded.x,
			y = excluded.y,
			width = excluded.width,
			height = excluded.height,
			result_data = excluded.result_data,
			updated_at = excluded.updated_at
	`

	_, err := s.db.ExecContext(ctx, query,
		glyph.ID, glyph.Symbol, glyph.X, glyph.Y,
		glyph.Width, glyph.Height, glyph.ResultData,
		glyph.CreatedAt.Format(time.RFC3339Nano),
		glyph.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return errors.Wrapf(err, "failed to upsert canvas glyph %s", glyph.ID)
	}

	return nil
}

// GetGlyph retrieves a glyph by ID
func (s *CanvasStore) GetGlyph(ctx context.Context, id string) (*CanvasGlyph, error) {
	query := `SELECT id, symbol, x, y, width, height, result_data, created_at, updated_at
	          FROM canvas_glyphs WHERE id = ?`

	var glyph CanvasGlyph
	var createdAt, updatedAt string

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&glyph.ID, &glyph.Symbol, &glyph.X, &glyph.Y,
		&glyph.Width, &glyph.Height, &glyph.ResultData,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, errors.Newf("canvas glyph %s not found", id)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get canvas glyph %s", id)
	}

	var parseErr error
	glyph.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAt)
	if parseErr != nil {
		return nil, errors.Wrapf(parseErr, "invalid created_at timestamp for glyph %s: %s", glyph.ID, createdAt)
	}
	glyph.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updatedAt)
	if parseErr != nil {
		return nil, errors.Wrapf(parseErr, "invalid updated_at timestamp for glyph %s: %s", glyph.ID, updatedAt)
	}

	return &glyph, nil
}

// ListGlyphs returns all glyphs
func (s *CanvasStore) ListGlyphs(ctx context.Context) ([]*CanvasGlyph, error) {
	query := `SELECT id, symbol, x, y, width, height, result_data, created_at, updated_at
	          FROM canvas_glyphs ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list canvas glyphs")
	}
	defer rows.Close()

	var glyphs []*CanvasGlyph
	for rows.Next() {
		var glyph CanvasGlyph
		var createdAt, updatedAt string

		if err := rows.Scan(
			&glyph.ID, &glyph.Symbol, &glyph.X, &glyph.Y,
			&glyph.Width, &glyph.Height, &glyph.ResultData,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan canvas glyph")
		}

		var parseErr error
		glyph.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAt)
		if parseErr != nil {
			return nil, errors.Wrapf(parseErr, "invalid created_at timestamp for glyph %s: %s", glyph.ID, createdAt)
		}
		glyph.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updatedAt)
		if parseErr != nil {
			return nil, errors.Wrapf(parseErr, "invalid updated_at timestamp for glyph %s: %s", glyph.ID, updatedAt)
		}

		glyphs = append(glyphs, &glyph)
	}

	return glyphs, nil
}

// DeleteGlyph removes a glyph
func (s *CanvasStore) DeleteGlyph(ctx context.Context, id string) error {
	query := `DELETE FROM canvas_glyphs WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return errors.Wrapf(err, "failed to delete canvas glyph %s", id)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.Newf("canvas glyph %s not found", id)
	}

	return nil
}

// === Composition operations ===

// UpsertComposition creates or updates a composition
func (s *CanvasStore) UpsertComposition(ctx context.Context, comp *CanvasComposition) error {
	now := time.Now()
	if comp.CreatedAt.IsZero() {
		comp.CreatedAt = now
	}
	comp.UpdatedAt = now

	// Start transaction for atomic composition + edges table updates
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	// Upsert composition record
	query := `
		INSERT INTO canvas_compositions (id, x, y, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			x = excluded.x,
			y = excluded.y,
			updated_at = excluded.updated_at
	`

	_, err = tx.ExecContext(ctx, query,
		comp.ID, comp.X, comp.Y,
		comp.CreatedAt.Format(time.RFC3339Nano),
		comp.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return errors.Wrapf(err, "failed to upsert canvas composition %s", comp.ID)
	}

	// Delete existing edges for this composition
	deleteQuery := `DELETE FROM composition_edges WHERE composition_id = ?`
	_, err = tx.ExecContext(ctx, deleteQuery, comp.ID)
	if err != nil {
		return errors.Wrapf(err, "failed to delete old composition edges for %s", comp.ID)
	}

	// Insert new edges
	insertQuery := `INSERT INTO composition_edges (composition_id, from_glyph_id, to_glyph_id, direction, position)
	                VALUES (?, ?, ?, ?, ?)`
	for _, edge := range comp.Edges {
		_, err = tx.ExecContext(ctx, insertQuery,
			comp.ID, edge.From, edge.To, edge.Direction, edge.Position)
		if err != nil {
			return errors.Wrapf(err, "failed to insert composition edge %s→%s", edge.From, edge.To)
		}
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit composition upsert transaction")
	}

	return nil
}

// GetComposition retrieves a composition by ID
func (s *CanvasStore) GetComposition(ctx context.Context, id string) (*CanvasComposition, error) {
	query := `SELECT id, x, y, created_at, updated_at
	          FROM canvas_compositions WHERE id = ?`

	var comp CanvasComposition
	var createdAt, updatedAt string

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&comp.ID, &comp.X, &comp.Y,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, errors.Newf("canvas composition %s not found", id)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get canvas composition %s", id)
	}

	var parseErr error
	comp.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAt)
	if parseErr != nil {
		return nil, errors.Wrapf(parseErr, "invalid created_at timestamp for composition %s: %s", comp.ID, createdAt)
	}
	comp.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updatedAt)
	if parseErr != nil {
		return nil, errors.Wrapf(parseErr, "invalid updated_at timestamp for composition %s: %s", comp.ID, updatedAt)
	}

	// Query edges table
	edgeQuery := `SELECT from_glyph_id, to_glyph_id, direction, position
	              FROM composition_edges
	              WHERE composition_id = ?
	              ORDER BY position ASC`
	rows, err := s.db.QueryContext(ctx, edgeQuery, id)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query composition edges for %s", id)
	}
	defer rows.Close()

	comp.Edges = []*pb.CompositionEdge{}
	for rows.Next() {
		var edge compositionEdge
		if err := rows.Scan(&edge.From, &edge.To, &edge.Direction, &edge.Position); err != nil {
			return nil, errors.Wrapf(err, "failed to scan edge for composition %s", id)
		}
		comp.Edges = append(comp.Edges, edge.toProtoEdge())
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "error iterating composition edges for %s", id)
	}

	// Validate: composition must have at least one edge
	if len(comp.Edges) == 0 {
		return nil, errors.Newf("composition %s has no edges (orphaned)", id)
	}

	return &comp, nil
}

// ListCompositions returns all compositions
func (s *CanvasStore) ListCompositions(ctx context.Context) ([]*CanvasComposition, error) {
	query := `SELECT id, x, y, created_at, updated_at
	          FROM canvas_compositions ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list canvas compositions")
	}
	defer rows.Close()

	// First pass: collect all composition data (avoid nested queries)
	var comps []*CanvasComposition
	for rows.Next() {
		var comp CanvasComposition
		var createdAt, updatedAt string

		if err := rows.Scan(
			&comp.ID, &comp.X, &comp.Y,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan canvas composition")
		}

		var parseErr error
		comp.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAt)
		if parseErr != nil {
			return nil, errors.Wrapf(parseErr, "invalid created_at timestamp for composition %s: %s", comp.ID, createdAt)
		}
		comp.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updatedAt)
		if parseErr != nil {
			return nil, errors.Wrapf(parseErr, "invalid updated_at timestamp for composition %s: %s", comp.ID, updatedAt)
		}

		comps = append(comps, &comp)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating compositions")
	}

	// Second pass: query edges table for each composition (after closing first result set)
	edgeQuery := `SELECT from_glyph_id, to_glyph_id, direction, position
	              FROM composition_edges
	              WHERE composition_id = ?
	              ORDER BY position ASC`
	for _, comp := range comps {
		edgeRows, err := s.db.QueryContext(ctx, edgeQuery, comp.ID)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to query composition edges for %s", comp.ID)
		}

		comp.Edges = []*pb.CompositionEdge{}
		for edgeRows.Next() {
			var edge compositionEdge
			if err := edgeRows.Scan(&edge.From, &edge.To, &edge.Direction, &edge.Position); err != nil {
				edgeRows.Close()
				return nil, errors.Wrapf(err, "failed to scan edge for composition %s", comp.ID)
			}
			comp.Edges = append(comp.Edges, edge.toProtoEdge())
		}
		edgeRows.Close()

		if err := edgeRows.Err(); err != nil {
			return nil, errors.Wrapf(err, "error iterating composition edges for %s", comp.ID)
		}
	}

	return comps, nil
}

// DeleteComposition removes a composition
func (s *CanvasStore) DeleteComposition(ctx context.Context, id string) error {
	query := `DELETE FROM canvas_compositions WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return errors.Wrapf(err, "failed to delete canvas composition %s", id)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return errors.Newf("canvas composition %s not found", id)
	}

	return nil
}
