package storage

import (
	"context"
	"database/sql"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/db"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/errors"
)

// "given a namespace there can be multiple canvasses"
// "A canvas can have multiple owners"
// "when anyone deletes a canvas, its not actually deleted, its disabled."

// What kind of canvas a row is: the namespace's own, or a User's.
const (
	CanvasOfTheNamespace = "namespace"
	CanvasOfAUser        = "user"
)

// Canvas is one canvas a namespace holds.
type Canvas struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	DisabledBy string    `json:"disabled_by"`
	// Owners are the Users a row names. ROOT and the SUPERs of the namespace
	// own every canvas without one.
	Owners []string `json:"owners"`
	// Access is the Users granted a look at the namespace's canvas.
	Access []string `json:"access"`
}

// Disabled reports whether somebody disabled this canvas.
func (c Canvas) Disabled() bool { return c.DisabledBy != "" }

// Invitation is a User asked to own a canvas, waiting for them to say yes.
type Invitation struct {
	Token      string
	CanvasID   string
	Inviter    string
	Invitee    string
	Email      string
	CreatedAt  time.Time
	AcceptedAt *time.Time
}

// ErrNoSuchCanvas is a canvas id this namespace does not hold.
var ErrNoSuchCanvas = errors.New("no such canvas in this namespace")

// ErrNoSuchInvitation is a token nobody was invited under, or one already spent.
var ErrNoSuchInvitation = errors.New("no such invitation")

// In is this store scoped to one canvas: the elements, compositions and
// minimized windows read and written are that canvas's.
func (s *CanvasStore) In(canvasID string) *CanvasStore {
	return &CanvasStore{db: s.db, canvas: canvasID}
}

// InCanvas is the canvas this store is scoped to, empty for the namespace's.
func (s *CanvasStore) InCanvas() string { return s.canvas }

// scope is the canvas rows are read and written under: the one this store is
// in, or the namespace's canvas, or nothing where none was created.
func (s *CanvasStore) scope(ctx context.Context) (string, error) {
	if s.canvas != "" {
		return s.canvas, nil
	}
	c, err := s.namespaceCanvas(ctx)
	if errors.Is(err, ErrNoCanvas) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

// namespaceCanvas is the namespace's own canvas.
func (s *CanvasStore) namespaceCanvas(ctx context.Context) (Canvas, error) {
	var c Canvas
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, kind, created_by, created_at, disabled_by FROM canvases WHERE kind = ? LIMIT 1`,
		CanvasOfTheNamespace).Scan(&c.ID, &c.Name, &c.Kind, &c.CreatedBy, &createdAt, &c.DisabledBy)
	if errors.Is(err, sql.ErrNoRows) {
		return Canvas{}, ErrNoCanvas
	}
	if err != nil {
		return Canvas{}, errors.Wrap(err, "failed to read the namespace's canvas")
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	return c, nil
}

// Name is the name of the namespace's canvas.
func (s *CanvasStore) Name(ctx context.Context) (string, error) {
	c, err := s.namespaceCanvas(ctx)
	if err != nil {
		return "", err
	}
	return c.Name, nil
}

// Create makes the namespace's canvas under a name, by whoever asked.
func (s *CanvasStore) Create(ctx context.Context, name, by string) error {
	if _, err := s.namespaceCanvas(ctx); err == nil {
		return errors.Wrapf(ErrCanvasExists, "creating %s", name)
	} else if !errors.Is(err, ErrNoCanvas) {
		return err
	}
	_, err := s.CreateCanvas(ctx, name, CanvasOfTheNamespace, by)
	return err
}

// CreateCanvas makes a canvas of a kind under a name. Its creator owns it,
// when it is a User's; the namespace's canvas has no owner row.
func (s *CanvasStore) CreateCanvas(ctx context.Context, name, kind, by string) (Canvas, error) {
	if name == "" {
		return Canvas{}, errors.New("a canvas is named, and this one has no name")
	}
	if kind != CanvasOfTheNamespace && kind != CanvasOfAUser {
		return Canvas{}, errors.Newf("a canvas is the namespace's or a User's, not %q", kind)
	}
	id, err := identity.GenerateUserID("canvas")
	if err != nil {
		return Canvas{}, errors.Wrap(err, "failed to mint a canvas id")
	}
	id = "CV" + id[2:]
	now := time.Now().UTC()
	c := Canvas{ID: id, Name: name, Kind: kind, CreatedBy: by, CreatedAt: now, Owners: []string{}, Access: []string{}}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Canvas{}, errors.Wrap(err, "failed to begin creating a canvas")
	}
	defer func() { err = db.Undone(err, tx) }()
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO canvases (id, name, kind, created_by, created_at, disabled_by) VALUES (?, ?, ?, ?, ?, '')`,
		id, name, kind, by, now.Format(time.RFC3339Nano)); err != nil {
		return Canvas{}, errors.Wrapf(err, "failed to create the canvas %s", name)
	}
	if kind == CanvasOfAUser && by != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO canvas_owners (canvas_id, user_id) VALUES (?, ?)`, id, by); err != nil {
			return Canvas{}, errors.Wrapf(err, "failed to make %s the owner of %s", by, name)
		}
		c.Owners = []string{by}
	}
	if err = tx.Commit(); err != nil {
		return Canvas{}, errors.Wrap(err, "failed to commit creating a canvas")
	}
	return c, nil
}

// Canvases is every canvas this namespace holds, oldest first, each with
// its owners and who was granted access.
func (s *CanvasStore) Canvases(ctx context.Context) (_ []Canvas, err error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, kind, created_by, created_at, disabled_by FROM canvases ORDER BY created_at ASC`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list canvases")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for Canvases") }()

	var canvases []Canvas
	for rows.Next() {
		var c Canvas
		var createdAt string
		if err := rows.Scan(&c.ID, &c.Name, &c.Kind, &c.CreatedBy, &createdAt, &c.DisabledBy); err != nil {
			return nil, errors.Wrap(err, "failed to scan a canvas")
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		canvases = append(canvases, c)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating canvases")
	}
	for i := range canvases {
		if canvases[i].Owners, err = s.people(ctx, "canvas_owners", canvases[i].ID); err != nil {
			return nil, err
		}
		if canvases[i].Access, err = s.people(ctx, "canvas_access", canvases[i].ID); err != nil {
			return nil, err
		}
	}
	return canvases, nil
}

// Canvas is one canvas by id, with its owners and who was granted access.
func (s *CanvasStore) Canvas(ctx context.Context, id string) (Canvas, error) {
	var c Canvas
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, kind, created_by, created_at, disabled_by FROM canvases WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.Kind, &c.CreatedBy, &createdAt, &c.DisabledBy)
	if errors.Is(err, sql.ErrNoRows) {
		return Canvas{}, errors.Wrapf(ErrNoSuchCanvas, "%s", id)
	}
	if err != nil {
		return Canvas{}, errors.Wrapf(err, "failed to read the canvas %s", id)
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if c.Owners, err = s.people(ctx, "canvas_owners", id); err != nil {
		return Canvas{}, err
	}
	if c.Access, err = s.people(ctx, "canvas_access", id); err != nil {
		return Canvas{}, err
	}
	return c, nil
}

// people is the Users a relation table names for a canvas.
func (s *CanvasStore) people(ctx context.Context, table, canvasID string) (_ []string, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM `+table+` WHERE canvas_id = ? ORDER BY user_id`, canvasID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read %s of %s", table, canvasID)
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for "+table) }()
	people := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, errors.Wrapf(err, "failed to scan %s", table)
		}
		people = append(people, id)
	}
	return people, rows.Err()
}

// relate writes one (canvas, user) row, or removes it.
func (s *CanvasStore) relate(ctx context.Context, table, canvasID, userID string, held bool) error {
	if userID == "" {
		return errors.New("a User is named, and this one is not")
	}
	if _, err := s.Canvas(ctx, canvasID); err != nil {
		return err
	}
	var err error
	if held {
		_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO `+table+` (canvas_id, user_id) VALUES (?, ?)`, canvasID, userID)
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE canvas_id = ? AND user_id = ?`, canvasID, userID)
	}
	if err != nil {
		return errors.Wrapf(err, "failed to write %s for %s on %s", table, userID, canvasID)
	}
	return nil
}

// AddOwner makes a User an owner of a canvas.
func (s *CanvasStore) AddOwner(ctx context.Context, canvasID, userID string) error {
	return s.relate(ctx, "canvas_owners", canvasID, userID, true)
}

// RemoveOwner takes a User off a canvas's owners. Removing every owner is
// "made unowned".
func (s *CanvasStore) RemoveOwner(ctx context.Context, canvasID, userID string) error {
	return s.relate(ctx, "canvas_owners", canvasID, userID, false)
}

// Disown takes every owner off a canvas.
func (s *CanvasStore) Disown(ctx context.Context, canvasID string) error {
	if _, err := s.Canvas(ctx, canvasID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM canvas_owners WHERE canvas_id = ?`, canvasID); err != nil {
		return errors.Wrapf(err, "failed to disown %s", canvasID)
	}
	return nil
}

// GrantAccess lets a User see the namespace's canvas.
func (s *CanvasStore) GrantAccess(ctx context.Context, canvasID, userID string) error {
	return s.relate(ctx, "canvas_access", canvasID, userID, true)
}

// RevokeAccess takes that back.
func (s *CanvasStore) RevokeAccess(ctx context.Context, canvasID, userID string) error {
	return s.relate(ctx, "canvas_access", canvasID, userID, false)
}

// Disable is what deleting a canvas is: it stays, marked by who did it.
func (s *CanvasStore) Disable(ctx context.Context, canvasID, by string) error {
	if by == "" {
		return errors.New("a canvas is disabled by somebody, and nobody is named")
	}
	return s.setDisabled(ctx, canvasID, by)
}

// Enable brings a disabled canvas back.
func (s *CanvasStore) Enable(ctx context.Context, canvasID string) error {
	return s.setDisabled(ctx, canvasID, "")
}

func (s *CanvasStore) setDisabled(ctx context.Context, canvasID, by string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE canvases SET disabled_by = ? WHERE id = ?`, by, canvasID)
	if err != nil {
		return errors.Wrapf(err, "failed to write disabled_by on %s", canvasID)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errors.Wrapf(err, "wrote disabled_by on %s but could not count the rows", canvasID)
	}
	if n == 0 {
		return errors.Wrapf(ErrNoSuchCanvas, "%s", canvasID)
	}
	return nil
}

// Invite asks a User to own a canvas. The token is what the mail carries,
// and accepting spends it.
func (s *CanvasStore) Invite(ctx context.Context, canvasID, inviter, invitee, email string) (Invitation, error) {
	if _, err := s.Canvas(ctx, canvasID); err != nil {
		return Invitation{}, err
	}
	token, err := identity.GenerateRandomID(32)
	if err != nil {
		return Invitation{}, errors.Wrap(err, "failed to mint an invitation token")
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO canvas_invitations (token, canvas_id, inviter, invitee, email, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		token, canvasID, inviter, invitee, email, now.Format(time.RFC3339Nano)); err != nil {
		return Invitation{}, errors.Wrapf(err, "failed to write the invitation of %s to %s", invitee, canvasID)
	}
	return Invitation{Token: token, CanvasID: canvasID, Inviter: inviter, Invitee: invitee, Email: email, CreatedAt: now}, nil
}

// Accept spends an invitation: the invitee becomes an owner. The one who
// accepts has to be the one invited.
func (s *CanvasStore) Accept(ctx context.Context, token, userID string) (_ Invitation, err error) {
	var inv Invitation
	var createdAt string
	var acceptedAt sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT token, canvas_id, inviter, invitee, email, created_at, accepted_at FROM canvas_invitations WHERE token = ?`, token).
		Scan(&inv.Token, &inv.CanvasID, &inv.Inviter, &inv.Invitee, &inv.Email, &createdAt, &acceptedAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && acceptedAt.Valid) {
		return Invitation{}, ErrNoSuchInvitation
	}
	if err != nil {
		return Invitation{}, errors.Wrap(err, "failed to read the invitation")
	}
	if inv.Invitee != userID {
		return Invitation{}, errors.Newf("this invitation is %s's, not %s's", inv.Invitee, userID)
	}
	inv.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Invitation{}, errors.Wrap(err, "failed to begin accepting an invitation")
	}
	defer func() { err = db.Undone(err, tx) }()
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `UPDATE canvas_invitations SET accepted_at = ? WHERE token = ?`,
		now.Format(time.RFC3339Nano), token); err != nil {
		return Invitation{}, errors.Wrap(err, "failed to spend the invitation")
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO canvas_owners (canvas_id, user_id) VALUES (?, ?)`,
		inv.CanvasID, userID); err != nil {
		return Invitation{}, errors.Wrapf(err, "failed to make %s an owner of %s", userID, inv.CanvasID)
	}
	if err = tx.Commit(); err != nil {
		return Invitation{}, errors.Wrap(err, "failed to commit accepting an invitation")
	}
	inv.AcceptedAt = &now
	return inv, nil
}
