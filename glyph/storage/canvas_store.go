package storage

import (
	"context"
	"database/sql"
	"time"

	"github.com/teranos/QNTX/errors"
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
type CanvasComposition struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"` // ax-prompt, ax-py, py-prompt
	InitiatorID string    `json:"initiator_id"`
	TargetID    string    `json:"target_id"`
	X           int       `json:"x"`
	Y           int       `json:"y"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
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

	glyph.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	glyph.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

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

		glyph.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		glyph.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

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

	query := `
		INSERT INTO canvas_compositions (id, type, initiator_id, target_id, x, y, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			x = excluded.x,
			y = excluded.y,
			updated_at = excluded.updated_at
	`

	_, err := s.db.ExecContext(ctx, query,
		comp.ID, comp.Type, comp.InitiatorID, comp.TargetID, comp.X, comp.Y,
		comp.CreatedAt.Format(time.RFC3339Nano),
		comp.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return errors.Wrapf(err, "failed to upsert canvas composition %s", comp.ID)
	}

	return nil
}

// GetComposition retrieves a composition by ID
func (s *CanvasStore) GetComposition(ctx context.Context, id string) (*CanvasComposition, error) {
	query := `SELECT id, type, initiator_id, target_id, x, y, created_at, updated_at
	          FROM canvas_compositions WHERE id = ?`

	var comp CanvasComposition
	var createdAt, updatedAt string

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&comp.ID, &comp.Type, &comp.InitiatorID, &comp.TargetID, &comp.X, &comp.Y,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, errors.Newf("canvas composition %s not found", id)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get canvas composition %s", id)
	}

	comp.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	comp.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

	return &comp, nil
}

// ListCompositions returns all compositions
func (s *CanvasStore) ListCompositions(ctx context.Context) ([]*CanvasComposition, error) {
	query := `SELECT id, type, initiator_id, target_id, x, y, created_at, updated_at
	          FROM canvas_compositions ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list canvas compositions")
	}
	defer rows.Close()

	var comps []*CanvasComposition
	for rows.Next() {
		var comp CanvasComposition
		var createdAt, updatedAt string

		if err := rows.Scan(
			&comp.ID, &comp.Type, &comp.InitiatorID, &comp.TargetID, &comp.X, &comp.Y,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan canvas composition")
		}

		comp.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		comp.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)

		comps = append(comps, &comp)
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
