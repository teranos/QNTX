package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/errors"
)

// "As ROOT i send an invite link to a friend, i enter their e-mail address"

// "A mail actually goes out to both me and my friend"

// "the MAIL ROOT received has a button for cancelling the invitation"

// "so, if ROOT selected Mastodon, the invited user only sees the mastodon link, and only the mastodon acc specified by ROOT would be applicable"

// ADR-031:
// "There is one ROOT User. A SUPER User is created by it and by nobody else."

// Attested when ROOT invites, cancels, and when the friend proves the account.
const (
	PredicateInvited             = "identity:invited"
	PredicateInvitationCancelled = "identity:invitation-cancelled"
	PredicateUserCreated         = "identity:created"
)

// Invitation is one friend ROOT invited: where the mail went, which provider
// they sign in with, and the account there that is theirs.
type Invitation struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Provider    string `json:"provider"`
	Account     string `json:"account"`
	InvitedBy   string `json:"invited_by"`
	CreatedAt   int64  `json:"created_at"`
	CancelledAt int64  `json:"cancelled_at,omitempty"`
	AcceptedBy  string `json:"accepted_by,omitempty"`
	AcceptedAt  int64  `json:"accepted_at,omitempty"`
}

// open is whether the link still admits anybody.
func (inv Invitation) open() bool {
	return inv.CancelledAt == 0 && inv.AcceptedBy == ""
}

// names is whether a binding is the account ROOT entered: the same provider,
// and the account by its canonical id or by the handle the provider vouched.
func (inv Invitation) names(b SignedBinding) bool {
	if b.Claim.Provider != inv.Provider {
		return false
	}
	if strings.EqualFold(b.Claim.CanonicalID, inv.Account) {
		return true
	}
	return b.Claim.Handle != nil && strings.EqualFold(*b.Claim.Handle, inv.Account)
}

// InvitationMailer sends what the node mails in its own name (ADR-042): to a
// User by id, and to an address nobody holds yet.
type InvitationMailer interface {
	SendAsNode(ctx context.Context, userID string, m services.NodeMail) (messageID, attestationID string, err error)
	SendAsNodeTo(ctx context.Context, u services.MailRecipient, m services.NodeMail) (messageID, attestationID string, err error)
}

// SetInvitationMail hands the handler what invitations are mailed with, and
// the page their links open on.
func (h *Handler) SetInvitationMail(mailer InvitationMailer, page string) {
	h.inviteMailer = mailer
	h.invitePage = strings.TrimSuffix(page, "/")
}

// invitationTable keeps invitations in the operational db.
type invitationTable struct{ db *sql.DB }

func newInvitationTable(db *sql.DB) *invitationTable {
	if db == nil {
		return nil
	}
	return &invitationTable{db: db}
}

const invitationColumns = `id, email, provider, account, invited_by, created_at,
	COALESCE(cancelled_at, 0), COALESCE(accepted_by, ''), COALESCE(accepted_at, 0)`

func scanInvitation(row interface{ Scan(...any) error }) (Invitation, error) {
	var inv Invitation
	err := row.Scan(&inv.ID, &inv.Email, &inv.Provider, &inv.Account, &inv.InvitedBy, &inv.CreatedAt,
		&inv.CancelledAt, &inv.AcceptedBy, &inv.AcceptedAt)
	return inv, err
}

// put writes an invitation under the hash of its token, never the token.
func (t *invitationTable) put(inv Invitation, token string) error {
	_, err := t.db.Exec(`INSERT INTO invitations (id, token_hash, email, provider, account, invited_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		inv.ID, hashOf(token), inv.Email, inv.Provider, inv.Account, inv.InvitedBy, inv.CreatedAt)
	return errors.Wrapf(err, "failed to write invitation %s for %s", inv.ID, inv.Email)
}

// byToken is the invitation a link's token names. False is none.
func (t *invitationTable) byToken(token string) (Invitation, bool, error) {
	inv, err := scanInvitation(t.db.QueryRow(`SELECT `+invitationColumns+` FROM invitations WHERE token_hash = ?`, hashOf(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, false, nil
	}
	if err != nil {
		return Invitation{}, false, errors.Wrap(err, "failed to read an invitation by its token")
	}
	return inv, true, nil
}

func (t *invitationTable) byID(id string) (Invitation, bool, error) {
	inv, err := scanInvitation(t.db.QueryRow(`SELECT `+invitationColumns+` FROM invitations WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, false, nil
	}
	if err != nil {
		return Invitation{}, false, errors.Wrapf(err, "failed to read invitation %s", id)
	}
	return inv, true, nil
}

func (t *invitationTable) list() (held []Invitation, err error) {
	rows, err := t.db.Query(`SELECT ` + invitationColumns + ` FROM invitations ORDER BY created_at DESC`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list invitations")
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = errors.Wrap(closeErr, "failed to close the invitations read")
		}
	}()
	held = []Invitation{}
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan an invitation")
		}
		held = append(held, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "the invitations table stopped answering")
	}
	return held, nil
}

// cancel closes an open invitation. False is one that was not open.
func (t *invitationTable) cancel(id string, at int64) (bool, error) {
	res, err := t.db.Exec(`UPDATE invitations SET cancelled_at = ? WHERE id = ? AND cancelled_at IS NULL AND accepted_by IS NULL`, at, id)
	if err != nil {
		return false, errors.Wrapf(err, "failed to cancel invitation %s", id)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, errors.Wrapf(err, "failed to read what cancelling invitation %s changed", id)
	}
	return n == 1, nil
}

// accept spends an open invitation on the User it made. False is one that
// was not open, which a second arrival on the same link finds.
func (t *invitationTable) accept(id, userID string, at int64) (bool, error) {
	res, err := t.db.Exec(`UPDATE invitations SET accepted_by = ?, accepted_at = ? WHERE id = ? AND cancelled_at IS NULL AND accepted_by IS NULL`, userID, at, id)
	if err != nil {
		return false, errors.Wrapf(err, "failed to spend invitation %s on %s", id, userID)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, errors.Wrapf(err, "failed to read what spending invitation %s changed", id)
	}
	return n == 1, nil
}

func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// invitationsCollection answers GET /auth/invitations, every invitation, and
// POST, ROOT inviting a friend.
func (h *Handler) invitationsCollection(w http.ResponseWriter, r *http.Request, p Presented) {
	if h.invitations == nil || h.users == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node keeps no Users, so it invites nobody")
		return
	}
	switch r.Method {
	case http.MethodGet:
		held, err := h.invitations.list()
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.writeJSON(w, http.StatusOK, held)
	case http.MethodPost:
		h.invite(w, r, p)
	default:
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) invite(w http.ResponseWriter, r *http.Request, p Presented) {
	route, _ := p.Admitted()
	if h.levelOf(route) != LevelRoot {
		h.writeError(w, http.StatusForbidden, "a friend is invited by ROOT")
		return
	}
	if h.inviteMailer == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node sends no mail, so an invitation cannot reach anybody")
		return
	}
	var body struct {
		Email    string `json:"email"`
		Provider string `json:"provider"`
		Account  string `json:"account"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "the body is not a JSON object of email, provider and account")
		return
	}
	email := strings.TrimSpace(body.Email)
	provider := strings.TrimSpace(body.Provider)
	account := strings.TrimSpace(body.Account)
	switch {
	case email == "" || !strings.Contains(email, "@") || strings.ContainsFunc(email, unicode.IsSpace):
		h.writeError(w, http.StatusBadRequest, "email is the friend's e-mail address")
		return
	case provider == "":
		h.writeError(w, http.StatusBadRequest, "provider is the one the friend signs in with")
		return
	case account == "" || strings.ContainsFunc(account, unicode.IsControl):
		h.writeError(w, http.StatusBadRequest, "account is the friend's account at "+provider)
		return
	}

	token, err := randomHex(32)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "no invitation token was made: "+err.Error())
		return
	}
	id, err := randomHex(16)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "no invitation id was made: "+err.Error())
		return
	}
	inv := Invitation{
		ID: id, Email: email, Provider: provider, Account: account,
		InvitedBy: p.UserID, CreatedAt: time.Now().UTC().UnixMilli(),
	}
	if err := h.invitations.put(inv, token); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.attest(PredicateInvited, route, map[string]any{
		"invitation": id, "email": email, "provider": provider, "account": account, "by": p.UserID,
	})

	inviter := p.DisplayName
	if inviter == "" {
		inviter = RootName
	}
	link := h.invitePage + "/?invitation=" + url.QueryEscape(token)
	cancel := h.invitePage + "/?invitation-cancel=" + url.QueryEscape(id)

	// The friend holds no User yet: the mail goes to the address, recorded
	// under the invitation.
	if _, _, err := h.inviteMailer.SendAsNodeTo(r.Context(), services.MailRecipient{ID: "invitation:" + id, Email: email}, services.NodeMail{
		Name:    "invitation",
		Subject: inviter + " invites you to QNTX",
		Text: inviter + " invites you to QNTX.\n\nSign in with " + provider + " as " + account + ":\n" + link + "\n\n" +
			"No password is set at QNTX.\n",
		HTML: "<p>" + htmlEscape(inviter) + " invites you to QNTX.</p>" +
			"<p>Sign in with " + htmlEscape(provider) + " as <b>" + htmlEscape(account) + "</b>.</p>" +
			"<p><a href=\"" + htmlEscape(link) + "\">Accept the invitation</a></p>" +
			"<p>No password is set at QNTX.</p>",
	}); err != nil {
		h.logger.Errorw("invitation written, friend's mail not sent", "invitation", id, "email", email, "error", err)
		h.writeError(w, http.StatusBadGateway, "the invitation "+id+" was written and the mail to "+email+" was not sent: "+err.Error())
		return
	}

	if _, _, err := h.inviteMailer.SendAsNode(r.Context(), p.UserID, services.NodeMail{
		Name:    "invitation-sent",
		Subject: "You invited " + email,
		Text: "You invited " + email + " to sign in with " + provider + " as " + account + ".\n\n" +
			"Cancel the invitation:\n" + cancel + "\n",
		HTML: "<p>You invited <b>" + htmlEscape(email) + "</b> to sign in with " + htmlEscape(provider) + " as <b>" + htmlEscape(account) + "</b>.</p>" +
			"<p><a href=\"" + htmlEscape(cancel) + "\">Cancel the invitation</a></p>",
	}); err != nil {
		h.logger.Errorw("invitation sent to the friend, ROOT's copy not sent", "invitation", id, "user", p.UserID, "error", err)
		h.writeError(w, http.StatusBadGateway, "the invitation went to "+email+" and your copy with the cancel was not sent: "+err.Error())
		return
	}

	h.logger.Infow("friend invited", "invitation", id, "email", email, "provider", provider, "by", p.UserID)
	h.writeJSON(w, http.StatusCreated, inv)
}

// handleInvitation routes one invitation: GET /auth/invitations/{token} to
// anyone holding the link, POST /auth/invitations/{id}/cancel to ROOT.
func (h *Handler) handleInvitation(w http.ResponseWriter, r *http.Request) {
	if h.invitations == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node keeps no invitations")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/auth/invitations/")
	key, verb, _ := strings.Cut(rest, "/")
	switch {
	case key == "":
		h.writeError(w, http.StatusBadRequest, "no invitation in "+r.URL.Path)
	case verb == "" && r.Method == http.MethodGet:
		h.invitationSeen(w, key)
	case verb == "cancel" && r.Method == http.MethodPost:
		h.sessionOnly(func(w http.ResponseWriter, r *http.Request, p Presented) { h.cancelInvitation(w, p, key) })(w, r)
	default:
		h.writeError(w, http.StatusNotFound, "no such route on an invitation: "+r.Method+" "+r.URL.Path)
	}
}

// invitationSeen is what the link shows the friend: the provider and the
// account ROOT named there.
func (h *Handler) invitationSeen(w http.ResponseWriter, token string) {
	inv, found, err := h.invitations.byToken(token)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		h.writeError(w, http.StatusNotFound, "no such invitation")
		return
	}
	if !inv.open() {
		h.writeError(w, http.StatusGone, "this invitation was cancelled or already used")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"provider": inv.Provider, "account": inv.Account})
}

func (h *Handler) cancelInvitation(w http.ResponseWriter, p Presented, id string) {
	route, _ := p.Admitted()
	if h.levelOf(route) != LevelRoot {
		h.writeError(w, http.StatusForbidden, "an invitation is cancelled by ROOT")
		return
	}
	inv, found, err := h.invitations.byID(id)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		h.writeError(w, http.StatusNotFound, "no invitation "+id)
		return
	}
	closed, err := h.invitations.cancel(id, time.Now().UTC().UnixMilli())
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !closed {
		h.writeError(w, http.StatusConflict, "the invitation to "+inv.Email+" was already cancelled or used")
		return
	}
	h.logger.Infow("invitation cancelled", "invitation", id, "email", inv.Email, "by", p.UserID)
	h.attest(PredicateInvitationCancelled, route, map[string]any{"invitation": id, "email": inv.Email, "by": p.UserID})
	h.writeJSON(w, http.StatusOK, map[string]string{"invitation": id, "email": inv.Email, "status": "cancelled"})
}

// acceptInvitation is the friend arriving with the link and the account ROOT
// named, vouched by that provider: their User is made, SUPER and made by
// ROOT, and the invitation is spent. False with no error admits nobody.
func (h *Handler) acceptInvitation(token string, vouched []SignedBinding) (string, *SignedBinding, bool, error) {
	if h.invitations == nil || h.users == nil || token == "" {
		return "", nil, false, nil
	}
	inv, found, err := h.invitations.byToken(token)
	if err != nil {
		return "", nil, false, err
	}
	if !found || !inv.open() {
		return "", nil, false, nil
	}
	var matched *SignedBinding
	for i := range vouched {
		if inv.names(vouched[i]) {
			matched = &vouched[i]
			break
		}
	}
	if matched == nil {
		h.logger.Infow("invitation refused: no binding is the account ROOT named",
			"invitation", inv.ID, "provider", inv.Provider, "account", inv.Account, "bindings", len(vouched))
		return "", nil, false, nil
	}
	route := matched.Claim.CanonicalID

	h.creating.Lock()
	defer h.creating.Unlock()

	_, held, err := h.users.ByRoute(route)
	if err != nil {
		return "", nil, false, errors.Wrapf(err, "failed to read whether %s already reaches a User", route)
	}
	if held {
		return "", nil, false, errors.Newf("%s already reaches a User, so invitation %s makes nobody", route, inv.ID)
	}

	u := withRoute(User{
		Level:          LevelSuper,
		CreatedBy:      inv.InvitedBy,
		CreatedAt:      time.Now().UTC().UnixMilli(),
		EmailAddresses: []string{inv.Email},
	}, route, matched)
	u.ID, err = identity.GenerateUserID("user")
	if err != nil {
		return "", nil, false, errors.Wrapf(err, "failed to generate a User id for %s", route)
	}
	spent, err := h.invitations.accept(inv.ID, u.ID, u.CreatedAt)
	if err != nil {
		return "", nil, false, err
	}
	if !spent {
		return "", nil, false, nil
	}
	if err := h.users.Put(u); err != nil {
		return "", nil, false, errors.Wrapf(err, "invitation %s was spent and the User for %s was not written", inv.ID, route)
	}
	h.logger.Infow("User created from an invitation", "user", u.ID, "route", route, "invitation", inv.ID, "by", inv.InvitedBy)
	h.attest(PredicateUserCreated, route, map[string]any{
		"user": u.ID, "invitation": inv.ID, "level": string(u.Level), "by": inv.InvitedBy, "email": inv.Email,
	})
	return route, matched, true, nil
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(s)
}
