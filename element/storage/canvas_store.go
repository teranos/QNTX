package storage

import (
	"context"
	"database/sql"
	"github.com/teranos/QNTX/internal/sqlclose"
	"time"

	"github.com/teranos/QNTX/db"
	pb "github.com/teranos/QNTX/element/proto"
	"github.com/teranos/errors"
)

// ErrNotFound is returned when a canvas entity (element, composition, minimized window) does not exist.
var ErrNotFound = errors.New("not found")

// CanvasElement represents an element on the canvas workspace
// Field types align with proto CanvasElement (int32 coordinates, string content)
type CanvasElement struct {
	ID        string    `json:"id"`
	CanvasID  string    `json:"canvas_id"` // Which canvas this element belongs to (empty = root canvas)
	Symbol    string    `json:"symbol"`
	X         int32     `json:"x"`
	Y         int32     `json:"y"`
	Width     *int32    `json:"width,omitempty"`
	Height    *int32    `json:"height,omitempty"`
	Content   *string   `json:"content,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CanvasComposition represents a melded composition of elements
// Uses edge-based DAG structure to support multi-directional melding
// See ADR-009 for rationale
type CanvasComposition struct {
	ID        string                `json:"id"`
	Edges     []*pb.CompositionEdge `json:"edges"`
	X         int32                 `json:"x"`
	Y         int32                 `json:"y"`
	CreatedAt time.Time             `json:"created_at"`
	UpdatedAt time.Time             `json:"updated_at"`
}

// compositionEdge is an internal struct for database operations
// Maps proto CompositionEdge to database schema
type compositionEdge struct {
	From      string `db:"from_element_id"`
	To        string `db:"to_element_id"`
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

// === Element operations ===

// UpsertElement creates or updates an element
func (s *CanvasStore) UpsertElement(ctx context.Context, item *CanvasElement) error {
	now := time.Now()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now

	query := `
		INSERT INTO canvas_elements (id, canvas_id, symbol, x, y, width, height, content, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			canvas_id = excluded.canvas_id,
			symbol = excluded.symbol,
			x = excluded.x,
			y = excluded.y,
			width = excluded.width,
			height = excluded.height,
			content = excluded.content,
			updated_at = excluded.updated_at
	`

	_, err := s.db.ExecContext(ctx, query,
		item.ID, item.CanvasID, item.Symbol, item.X, item.Y,
		item.Width, item.Height, item.Content,
		item.CreatedAt.Format(time.RFC3339Nano),
		item.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return errors.Wrapf(err, "failed to upsert canvas element %s", item.ID)
	}

	return nil
}

// GetElement retrieves an element by ID
func (s *CanvasStore) GetElement(ctx context.Context, id string) (*CanvasElement, error) {
	query := `SELECT id, canvas_id, symbol, x, y, width, height, content, created_at, updated_at
	          FROM canvas_elements WHERE id = ?`

	var item CanvasElement
	var createdAt, updatedAt string

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&item.ID, &item.CanvasID, &item.Symbol, &item.X, &item.Y,
		&item.Width, &item.Height, &item.Content,
		&createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.Wrapf(ErrNotFound, "canvas element %s", id)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get canvas element %s", id)
	}

	var parseErr error
	item.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAt)
	if parseErr != nil {
		return nil, errors.Wrapf(parseErr, "invalid created_at timestamp for element %s: %s", item.ID, createdAt)
	}
	item.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updatedAt)
	if parseErr != nil {
		return nil, errors.Wrapf(parseErr, "invalid updated_at timestamp for element %s: %s", item.ID, updatedAt)
	}

	return &item, nil
}

// ListElements returns all elements
func (s *CanvasStore) ListElements(ctx context.Context) (_ []*CanvasElement, err error) {
	query := `SELECT id, canvas_id, symbol, x, y, width, height, content, created_at, updated_at
	          FROM canvas_elements ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list canvas elements")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for ListElements") }()

	var items []*CanvasElement
	for rows.Next() {
		var item CanvasElement
		var createdAt, updatedAt string

		if err := rows.Scan(
			&item.ID, &item.CanvasID, &item.Symbol, &item.X, &item.Y,
			&item.Width, &item.Height, &item.Content,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan canvas element")
		}

		var parseErr error
		item.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAt)
		if parseErr != nil {
			return nil, errors.Wrapf(parseErr, "invalid created_at timestamp for element %s: %s", item.ID, createdAt)
		}
		item.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updatedAt)
		if parseErr != nil {
			return nil, errors.Wrapf(parseErr, "invalid updated_at timestamp for element %s: %s", item.ID, updatedAt)
		}

		items = append(items, &item)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating canvas elements")
	}

	return items, nil
}

// DeleteElement removes an element
func (s *CanvasStore) DeleteElement(ctx context.Context, id string) error {
	query := `DELETE FROM canvas_elements WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return errors.Wrapf(err, "failed to delete canvas element %s", id)
	}

	// A driver that cannot count the rows has not said the element was absent,
	// and ErrNotFound here would be this function inventing that.
	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrapf(err, "deleted canvas element %s but could not count the rows", id)
	}
	if rows == 0 {
		return errors.Wrapf(ErrNotFound, "canvas element %s", id)
	}

	return nil
}

// === Composition operations ===

// UpsertComposition creates or updates a composition
func (s *CanvasStore) UpsertComposition(ctx context.Context, comp *CanvasComposition) (err error) {
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
	defer func() { err = db.Undone(err, tx) }()

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
	insertQuery := `INSERT INTO composition_edges (composition_id, from_element_id, to_element_id, direction, position)
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
func (s *CanvasStore) GetComposition(ctx context.Context, id string) (_ *CanvasComposition, err error) {
	query := `SELECT id, x, y, created_at, updated_at
	          FROM canvas_compositions WHERE id = ?`

	var comp CanvasComposition
	var createdAt, updatedAt string

	err = s.db.QueryRowContext(ctx, query, id).Scan(
		&comp.ID, &comp.X, &comp.Y,
		&createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.Wrapf(ErrNotFound, "canvas composition %s", id)
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
	edgeQuery := `SELECT from_element_id, to_element_id, direction, position
	              FROM composition_edges
	              WHERE composition_id = ?
	              ORDER BY position ASC`
	rows, err := s.db.QueryContext(ctx, edgeQuery, id)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query composition edges for %s", id)
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for GetComposition") }()

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
func (s *CanvasStore) ListCompositions(ctx context.Context) (_ []*CanvasComposition, err error) {
	query := `SELECT id, x, y, created_at, updated_at
	          FROM canvas_compositions ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list canvas compositions")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for ListCompositions") }()

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
	for _, comp := range comps {
		comp.Edges, err = s.edgesForComposition(ctx, comp.ID)
		if err != nil {
			return nil, err
		}
	}

	return comps, nil
}

// edgesForComposition reads one composition's edges in its own scope, so
// each composition's rows close before the next open — one open handle per
// composition would otherwise stack until the caller returns.
func (s *CanvasStore) edgesForComposition(ctx context.Context, compID string) (_ []*pb.CompositionEdge, err error) {
	edgeRows, err := s.db.QueryContext(ctx, `SELECT from_element_id, to_element_id, direction, position
	              FROM composition_edges
	              WHERE composition_id = ?
	              ORDER BY position ASC`, compID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query composition edges for %s", compID)
	}
	defer func() { err = sqlclose.With(err, edgeRows.Close(), "edge rows for ListCompositions") }()

	edges := []*pb.CompositionEdge{}
	for edgeRows.Next() {
		var edge compositionEdge
		if err := edgeRows.Scan(&edge.From, &edge.To, &edge.Direction, &edge.Position); err != nil {
			return nil, errors.Wrapf(err, "failed to scan edge for composition %s", compID)
		}
		edges = append(edges, edge.toProtoEdge())
	}
	if err := edgeRows.Err(); err != nil {
		return nil, errors.Wrapf(err, "error iterating composition edges for %s", compID)
	}
	return edges, nil
}

// DeleteComposition removes a composition
func (s *CanvasStore) DeleteComposition(ctx context.Context, id string) error {
	query := `DELETE FROM canvas_compositions WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return errors.Wrapf(err, "failed to delete canvas composition %s", id)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrapf(err, "deleted canvas composition %s but could not count the rows", id)
	}
	if rows == 0 {
		return errors.Wrapf(ErrNotFound, "canvas composition %s", id)
	}

	return nil
}

// === Minimized window operations ===

// MinimizedWindow represents an element minimized to the tray
type MinimizedWindow struct {
	ElementID string    `json:"element_id"`
	CreatedAt time.Time `json:"created_at"`
}

// AddMinimizedWindow records an element as minimized
func (s *CanvasStore) AddMinimizedWindow(ctx context.Context, elementID string) error {
	query := `INSERT OR IGNORE INTO minimized_windows (element_id) VALUES (?)`
	_, err := s.db.ExecContext(ctx, query, elementID)
	if err != nil {
		return errors.Wrapf(err, "failed to add minimized window %s", elementID)
	}
	return nil
}

// ListMinimizedWindows returns all minimized window element IDs
func (s *CanvasStore) ListMinimizedWindows(ctx context.Context) (_ []*MinimizedWindow, err error) {
	query := `SELECT element_id, created_at FROM minimized_windows ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list minimized windows")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for ListMinimizedWindows") }()

	var windows []*MinimizedWindow
	for rows.Next() {
		var w MinimizedWindow
		var createdAt string
		if err := rows.Scan(&w.ElementID, &createdAt); err != nil {
			return nil, errors.Wrap(err, "failed to scan minimized window")
		}
		var parseErr error
		w.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAt)
		if parseErr != nil {
			// Fallback: SQLite default format uses fractional seconds without timezone
			w.CreatedAt, parseErr = time.Parse("2006-01-02T15:04:05.000", createdAt)
			if parseErr != nil {
				return nil, errors.Wrapf(parseErr, "invalid created_at timestamp for minimized window %s: %s", w.ElementID, createdAt)
			}
		}
		windows = append(windows, &w)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating minimized windows")
	}

	return windows, nil
}

// RemoveMinimizedWindow removes a minimized window record
func (s *CanvasStore) RemoveMinimizedWindow(ctx context.Context, elementID string) error {
	query := `DELETE FROM minimized_windows WHERE element_id = ?`
	result, err := s.db.ExecContext(ctx, query, elementID)
	if err != nil {
		return errors.Wrapf(err, "failed to remove minimized window %s", elementID)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrapf(err, "removed minimized window %s but could not count the rows", elementID)
	}
	if rows == 0 {
		return errors.Wrapf(ErrNotFound, "minimized window %s", elementID)
	}

	return nil
}
