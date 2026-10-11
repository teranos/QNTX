package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/sqlclose"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// "and while ROOT is them, they cannot be themselves"

// Becoming is ROOT being a User that is not ROOT (ADR-031), held by the session
// ROOT became them in. It is removed whole before 1.0.0.
type becoming struct {
	user string
	by   string
	// session is the hash of ROOT's session, which is them until it unbecomes them.
	session string
	at      time.Time
	// own is the ids of the tokens they had minted when ROOT became them: those
	// are refused until ROOT unbecomes them, and anything minted for them after
	// lives only as long as this.
	own []string
}

// becomings is every becoming the node holds, in the process and, given a db,
// in the operational db, so a restart neither ends one nor lets them be
// themselves while ROOT is them.
type becomings struct {
	mu        sync.RWMutex
	bySession map[string]becoming
	db        *sql.DB
	logger    *zap.SugaredLogger
}

// kept loads the becomings db holds and keeps every later one there too.
func (b *becomings) kept(db *sql.DB, logger *zap.SugaredLogger) (err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.db, b.logger = db, logger
	b.bySession = map[string]becoming{}
	if db == nil {
		return nil
	}
	rows, err := db.Query(`SELECT session, user_id, by_user, became_at, own_tokens FROM auth_becomings`)
	if err != nil {
		return errors.Wrap(err, "the becomings were not read from the operational db")
	}
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for becomings") }()
	for rows.Next() {
		var x becoming
		var at int64
		var own string
		if err := rows.Scan(&x.session, &x.user, &x.by, &at, &own); err != nil {
			return errors.Wrap(err, "a becoming did not read")
		}
		if err := json.Unmarshal([]byte(own), &x.own); err != nil {
			return errors.Wrapf(err, "the tokens of the User %s, as ROOT became them, did not read", x.user)
		}
		x.at = time.UnixMilli(at)
		b.bySession[x.session] = x
	}
	return errors.Wrap(rows.Err(), "the becomings were not read whole")
}

func (b *becomings) ofSession(hash string) (becoming, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	x, ok := b.bySession[hash]
	return x, ok
}

func (b *becomings) ofUser(id string) (becoming, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, x := range b.bySession {
		if x.user == id {
			return x, true
		}
	}
	return becoming{}, false
}

func (b *becomings) all() []becoming {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]becoming, 0, len(b.bySession))
	for _, x := range b.bySession {
		out = append(out, x)
	}
	return out
}

// alreadyBecome is a User some session is being already.
type alreadyBecome struct{ user string }

func (e alreadyBecome) Error() string {
	return "ROOT is being " + e.user + " already, and is them until it unbecomes them"
}

// begin writes a becoming down, the db first: a becoming the db did not take
// would end at the next restart and hand them back their tokens unasked.
func (b *becomings) begin(x becoming) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bySession == nil {
		b.bySession = map[string]becoming{}
	}
	for _, held := range b.bySession {
		if held.user == x.user || held.session == x.session {
			return alreadyBecome{user: held.user}
		}
	}
	if b.db != nil {
		own, err := json.Marshal(x.own)
		if err != nil {
			return errors.Wrap(err, "the tokens they hold did not write")
		}
		if _, err := b.db.Exec(`INSERT INTO auth_becomings (session, user_id, by_user, became_at, own_tokens) VALUES (?, ?, ?, ?, ?)`,
			x.session, x.user, x.by, x.at.UnixMilli(), string(own)); err != nil {
			return errors.Wrapf(err, "becoming %s was not written to the operational db", x.user)
		}
	}
	b.bySession[x.session] = x
	return nil
}

func (b *becomings) end(hash string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.db != nil {
		if _, err := b.db.Exec(`DELETE FROM auth_becomings WHERE session = ?`, hash); err != nil {
			return errors.Wrap(err, "the becoming was not removed from the operational db")
		}
	}
	delete(b.bySession, hash)
	return nil
}

// routeOf is a route that reaches u, which is what a session names: an account
// they hold, or else a key.
func routeOf(u User) string {
	for _, a := range u.Accounts {
		if a.CanonicalID != "" {
			return a.CanonicalID
		}
	}
	for _, k := range u.Keys {
		if k.DID != "" {
			return k.DID
		}
	}
	return ""
}

// asBecome presents ROOT's session as the User it is being.
func (h *Handler) asBecome(p *Presented) {
	if !p.SessionLive || p.sessionToken == "" {
		return
	}
	x, being := h.becomings.ofSession(hashOf(p.sessionToken))
	if !being {
		return
	}
	u, found, err := h.userByID(x.user)
	if err != nil || !found {
		h.logger.Errorw("the User ROOT is being was not read, so the session is presented as ROOT",
			"user", x.user, "found", found, "error", err)
		return
	}
	p.becameFrom = p.Session
	p.Session, p.UserID, p.DisplayName, p.Namespace = routeOf(u), u.ID, u.Name(), u.Namespace
	p.BecomingBy = x.by
}

// notThemselves is who is being the User a request speaks for, when it is not
// that becoming asking: their own sessions, and the tokens they held when ROOT
// became them. A token minted for them since is the becoming's, and passes.
func (h *Handler) notThemselves(p Presented, userID string) (string, bool) {
	if userID == "" || p.BecomingBy != "" {
		return "", false
	}
	x, being := h.becomings.ofUser(userID)
	if !being {
		return "", false
	}
	if p.Bearer != nil && !slices.Contains(x.own, p.Bearer.ID) {
		return "", false
	}
	return x.by, true
}

// rejectBecome turns away a User while ROOT is them. 403: presenting it again
// changes nothing until ROOT unbecomes them.
func (h *Handler) rejectBecome(w http.ResponseWriter, r *http.Request, userID, by string) {
	h.logger.Infow("Admission refused",
		"path", r.URL.Path,
		"user", userID,
		"reason", "ROOT is being this User",
		"becoming_by", by)
	h.writeJSON(w, http.StatusForbidden, map[string]string{
		"error":       "ROOT is being this User, and they are themselves again when ROOT unbecomes them",
		"becoming_by": by,
	})
}

// become is ROOT becoming the User named by id.
// POST /auth/users/{id}/become
func (h *Handler) become(w http.ResponseWriter, r *http.Request, p Presented, id string) {
	route, _ := p.Admitted()
	if p.BecomingBy != "" || h.levelOf(route) != LevelRoot {
		h.writeError(w, http.StatusForbidden, "a User is become by ROOT")
		return
	}
	u, found, err := h.userByID(id)
	if err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "read", "user": id, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		h.writeError(w, http.StatusNotFound, "no User "+id)
		return
	}
	if u.Level == LevelRoot {
		h.writeError(w, http.StatusBadRequest, "ROOT is itself, and becomes nobody who is ROOT")
		return
	}
	if u.SwitchedOff() {
		h.writeError(w, http.StatusConflict, "User "+id+" is switched off by "+u.DisabledBy+", and is admitted at no gate")
		return
	}
	if h.levelOf(routeOf(u)) == "" {
		h.writeError(w, http.StatusConflict, "nothing admits User "+id+", so being them is being nobody")
		return
	}
	var own []string
	if h.tokens != nil {
		held, err := h.tokens.List()
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "the token store did not answer: "+err.Error())
			return
		}
		for _, t := range held {
			if t.MintedByUser == u.ID {
				own = append(own, t.ID)
			}
		}
	}
	x := becoming{user: u.ID, by: p.UserID, session: hashOf(p.sessionToken), at: time.Now(), own: own}
	if err := h.becomings.begin(x); err != nil {
		var already alreadyBecome
		if errors.As(err, &already) {
			h.writeError(w, http.StatusConflict, err.Error())
			return
		}
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// "So, yes, this can be destructive in the sense that a session may get revoked."
	h.sessions.endUser(u.ID)
	h.logger.Infow("ROOT became a User", "user", u.ID, "by", x.by, "tokens_refused", len(own))
	h.attest(PredicateBecame, route, map[string]any{"user": u.ID, "by": x.by})
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "become", "user": u.ID, "by": x.by})
}

// HandleUnbecome is ROOT being itself again.
// POST /i/unbecome
func (h *Handler) HandleUnbecome(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	p := h.presented(r)
	if !p.SessionLive || p.BecomingBy == "" {
		h.writeError(w, http.StatusConflict, "this session is not being anybody")
		return
	}
	x, revoked, err := h.unbecome(hashOf(p.sessionToken))
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.logger.Infow("ROOT unbecame a User", "user", x.user, "by", x.by, "tokens_revoked", len(revoked))
	h.attest(PredicateUnbecame, p.becameFrom, map[string]any{"user": x.user, "by": x.by, "revoked": revoked})
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "unbecome", "user": x.user, "by": x.by})
}

// unbecome ends the becoming a session holds, and the tokens minted for its
// User while it lasted with it.
func (h *Handler) unbecome(hash string) (becoming, []string, error) {
	x, being := h.becomings.ofSession(hash)
	if !being {
		return becoming{}, nil, errors.New("this session is not being anybody")
	}
	revoked := []string{}
	if h.tokens != nil {
		held, err := h.tokens.List()
		if err != nil {
			return x, nil, errors.Wrap(err, "the token store did not answer, so nothing was unbecome")
		}
		for _, t := range held {
			if t.MintedByUser != x.user || slices.Contains(x.own, t.ID) || t.RevokedAt != nil {
				continue
			}
			if err := h.tokens.Revoke(t.ID); err != nil {
				return x, revoked, errors.Wrapf(err, "token %s, minted while ROOT was %s, was not revoked, so nothing was unbecome", t.ID, x.user)
			}
			revoked = append(revoked, t.ID)
		}
	}
	return x, revoked, h.becomings.end(hash)
}

// unbecomeTheEnded ends every becoming whose session is over: a becoming
// outliving ROOT's session would keep the User from being themselves with
// nobody left to unbecome them.
func (h *Handler) unbecomeTheEnded() {
	for _, x := range h.becomings.all() {
		if h.sessions.liveHash(x.session) {
			continue
		}
		_, revoked, err := h.unbecome(x.session)
		if err != nil {
			h.logger.Errorw("a becoming whose session ended was not unbecome", "user", x.user, "error", err)
			continue
		}
		h.logger.Infow("ROOT's session ended, so it unbecame a User", "user", x.user, "by", x.by, "tokens_revoked", len(revoked))
		h.attest(PredicateUnbecame, x.by, map[string]any{"user": x.user, "by": x.by, "revoked": revoked, "because": "the session ended"})
	}
}
