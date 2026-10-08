package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
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

// Attested when ROOT invites, cancels, and when the invitee proves the account.
const (
	PredicateInvited             = "identity:invited"
	PredicateInvitationCancelled = "identity:invitation-cancelled"
	PredicateUserCreated         = "identity:created"
)

// "if both google and apple, then we set both, and  if set then we set the mail address of that provider"

// "or the username"

// InvitationAccount is one provider the invitee may sign in with, and the
// account there that is theirs: an address, or a username.
type InvitationAccount struct {
	Provider string `json:"provider"`
	Account  string `json:"account"`
}

// Invitation is one person ROOT invited: their name, where the mail went, and
// every provider they may sign in with.
type Invitation struct {
	ID          string              `json:"id"`
	Email       string              `json:"email"`
	DisplayName string              `json:"display_name,omitempty"`
	Accounts    []InvitationAccount `json:"accounts"`
	InvitedBy   string              `json:"invited_by"`
	CreatedAt   int64               `json:"created_at"`
	CancelledAt int64               `json:"cancelled_at,omitempty"`
	AcceptedBy  string              `json:"accepted_by,omitempty"`
	AcceptedAt  int64               `json:"accepted_at,omitempty"`
}

// open is whether the link still admits anybody.
func (inv Invitation) open() bool {
	return inv.CancelledAt == 0 && inv.AcceptedBy == ""
}

// names is whether a binding is one of the accounts ROOT entered: the same
// provider, and the account by its canonical id or by the handle the provider
// vouched.
func (inv Invitation) names(b SignedBinding) bool {
	for _, a := range inv.Accounts {
		if b.Claim.Provider != a.Provider {
			continue
		}
		if strings.EqualFold(b.Claim.CanonicalID, a.Account) {
			return true
		}
		if b.Claim.Handle != nil && strings.EqualFold(*b.Claim.Handle, a.Account) {
			return true
		}
	}
	return false
}

// signsInWith is the accounts as a person reads them, each provider with its
// account, joined by or.
func (inv Invitation) signsInWith() string {
	said := make([]string, 0, len(inv.Accounts))
	for _, a := range inv.Accounts {
		said = append(said, a.Provider+" as "+a.Account)
	}
	return strings.Join(said, " or ")
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

const invitationColumns = `id, email, display_name, invited_by, created_at,
	COALESCE(cancelled_at, 0), COALESCE(accepted_by, ''), COALESCE(accepted_at, 0)`

func scanInvitation(row interface{ Scan(...any) error }) (Invitation, error) {
	var inv Invitation
	err := row.Scan(&inv.ID, &inv.Email, &inv.DisplayName, &inv.InvitedBy, &inv.CreatedAt,
		&inv.CancelledAt, &inv.AcceptedBy, &inv.AcceptedAt)
	return inv, err
}

// put writes an invitation under the hash of its token, never the token, and
// its accounts with it, in one transaction.
func (t *invitationTable) put(inv Invitation, token string) (err error) {
	tx, err := t.db.Begin()
	if err != nil {
		return errors.Wrapf(err, "failed to begin writing invitation %s", inv.ID)
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				err = errors.WithSecondaryError(err, rollbackErr)
			}
		}
	}()
	if _, err = tx.Exec(`INSERT INTO invitations (id, token_hash, email, display_name, invited_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		inv.ID, hashOf(token), inv.Email, inv.DisplayName, inv.InvitedBy, inv.CreatedAt); err != nil {
		return errors.Wrapf(err, "failed to write invitation %s for %s", inv.ID, inv.Email)
	}
	for _, a := range inv.Accounts {
		if _, err = tx.Exec(`INSERT INTO invitation_accounts (invitation_id, provider, account) VALUES (?, ?, ?)`,
			inv.ID, a.Provider, a.Account); err != nil {
			return errors.Wrapf(err, "failed to write %s as %s on invitation %s", a.Provider, a.Account, inv.ID)
		}
	}
	return errors.Wrapf(tx.Commit(), "failed to commit invitation %s", inv.ID)
}

// withAccounts reads the accounts an invitation names, by provider.
func (t *invitationTable) withAccounts(inv Invitation) (_ Invitation, err error) {
	rows, err := t.db.Query(`SELECT provider, account FROM invitation_accounts WHERE invitation_id = ? ORDER BY provider`, inv.ID)
	if err != nil {
		return inv, errors.Wrapf(err, "failed to read the accounts of invitation %s", inv.ID)
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = errors.Wrapf(closeErr, "failed to close the accounts read of invitation %s", inv.ID)
		}
	}()
	inv.Accounts = []InvitationAccount{}
	for rows.Next() {
		var a InvitationAccount
		if err := rows.Scan(&a.Provider, &a.Account); err != nil {
			return inv, errors.Wrapf(err, "failed to scan an account of invitation %s", inv.ID)
		}
		inv.Accounts = append(inv.Accounts, a)
	}
	return inv, errors.Wrapf(rows.Err(), "the accounts of invitation %s stopped answering", inv.ID)
}

// one is the invitation a query names, with its accounts. False is none.
func (t *invitationTable) one(where string, arg any) (Invitation, bool, error) {
	inv, err := scanInvitation(t.db.QueryRow(`SELECT `+invitationColumns+` FROM invitations WHERE `+where+` = ?`, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, false, nil
	}
	if err != nil {
		return Invitation{}, false, errors.Wrapf(err, "failed to read an invitation by %s", where)
	}
	inv, err = t.withAccounts(inv)
	return inv, err == nil, err
}

// byToken is the invitation a link's token names. False is none.
func (t *invitationTable) byToken(token string) (Invitation, bool, error) {
	return t.one("token_hash", hashOf(token))
}

func (t *invitationTable) byID(id string) (Invitation, bool, error) {
	return t.one("id", id)
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
	for i := range held {
		if held[i], err = t.withAccounts(held[i]); err != nil {
			return nil, err
		}
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
// POST, ROOT inviting someone.
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
		h.writeError(w, http.StatusForbidden, "an invitation is sent by ROOT")
		return
	}
	if h.inviteMailer == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node sends no mail, so an invitation cannot reach anybody")
		return
	}
	var body struct {
		DisplayName string              `json:"display_name"`
		Email       string              `json:"email"`
		Accounts    []InvitationAccount `json:"accounts"`
		// The page ROOT invited from, which the links open on.
		Page string `json:"page"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "the body is not a JSON object of display_name, email and accounts")
		return
	}
	name := strings.TrimSpace(body.DisplayName)
	email := strings.TrimSpace(body.Email)
	if email == "" || !strings.Contains(email, "@") || strings.ContainsFunc(email, unicode.IsSpace) {
		h.writeError(w, http.StatusBadRequest, "email is the invitee's e-mail address")
		return
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		h.writeError(w, http.StatusBadRequest, "display_name is one line of text")
		return
	}
	if refusal, bad := refuseName(User{}, name); bad {
		h.writeError(w, http.StatusBadRequest, refusal)
		return
	}
	if len(body.Accounts) == 0 {
		h.writeError(w, http.StatusBadRequest, "accounts names at least one provider the invitee signs in with")
		return
	}
	accounts := make([]InvitationAccount, 0, len(body.Accounts))
	seen := map[string]bool{}
	for _, a := range body.Accounts {
		a.Provider, a.Account = strings.TrimSpace(a.Provider), strings.TrimSpace(a.Account)
		switch {
		case a.Provider == "":
			h.writeError(w, http.StatusBadRequest, "every account names its provider")
			return
		case a.Account == "" || strings.ContainsFunc(a.Account, unicode.IsControl):
			h.writeError(w, http.StatusBadRequest, "the account at "+a.Provider+" is the invitee's address or username there")
			return
		case seen[a.Provider]:
			h.writeError(w, http.StatusBadRequest, a.Provider+" is named twice")
			return
		}
		seen[a.Provider] = true
		accounts = append(accounts, a)
	}
	page, ok := h.ownPage(body.Page)
	if !ok {
		h.writeError(w, http.StatusBadRequest, "page "+body.Page+" is not on an origin in auth.rp_origins")
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
		ID: id, Email: email, DisplayName: name, Accounts: accounts,
		InvitedBy: p.UserID, CreatedAt: time.Now().UTC().UnixMilli(),
	}
	if err := h.invitations.put(inv, token); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.attest(PredicateInvited, route, map[string]any{
		"invitation": id, "email": email, "display_name": name, "accounts": accounts, "by": p.UserID,
	})

	inviter := p.DisplayName
	if inviter == "" {
		inviter = RootName
	}
	invitee := email
	if name != "" {
		invitee = name + " (" + email + ")"
	}
	signsIn := inv.signsInWith()
	link := page + "/?invitation=" + url.QueryEscape(token)
	cancel := page + "/?invitation-cancel=" + url.QueryEscape(id)

	// The invitee holds no User yet: the mail goes to the address, recorded
	// under the invitation.
	if _, _, err := h.inviteMailer.SendAsNodeTo(r.Context(), services.MailRecipient{ID: "invitation:" + id, Email: email}, services.NodeMail{
		Name:    "invitation",
		Subject: inviter + " invites you to QNTX",
		Text: inviter + " invites you to QNTX.\n\nSign in with " + signsIn + ":\n" + link + "\n\n" +
			"No password is set at QNTX.\n",
		HTML: "<p>" + htmlEscape(inviter) + " invites you to QNTX.</p>" +
			"<p>Sign in with <b>" + htmlEscape(signsIn) + "</b>.</p>" +
			"<p><a href=\"" + htmlEscape(link) + "\">Accept the invitation</a></p>" +
			"<p>No password is set at QNTX.</p>",
	}); err != nil {
		h.logger.Errorw("invitation written, invitee's mail not sent", "invitation", id, "email", email, "error", err)
		h.writeError(w, http.StatusBadGateway, "the invitation "+id+" was written and the mail to "+email+" was not sent: "+err.Error())
		return
	}

	if _, _, err := h.inviteMailer.SendAsNode(r.Context(), p.UserID, services.NodeMail{
		Name:    "invitation-sent",
		Subject: "You invited " + invitee,
		Text: "You invited " + invitee + " to sign in with " + signsIn + ".\n\n" +
			"Cancel the invitation:\n" + cancel + "\n",
		HTML: "<p>You invited <b>" + htmlEscape(invitee) + "</b> to sign in with " + htmlEscape(signsIn) + ".</p>" +
			"<p><a href=\"" + htmlEscape(cancel) + "\">Cancel the invitation</a></p>",
	}); err != nil {
		h.logger.Errorw("invitation sent to the invitee, ROOT's copy not sent", "invitation", id, "user", p.UserID, "error", err)
		h.writeError(w, http.StatusBadGateway, "the invitation went to "+email+" and your copy with the cancel was not sent: "+err.Error())
		return
	}

	h.logger.Infow("invited", "invitation", id, "email", email, "signs_in_with", signsIn, "by", p.UserID)
	h.writeJSON(w, http.StatusCreated, inv)
}

// ownPage is the page the links open on: the one ROOT invited from when its
// origin is in auth.rp_origins, so a link only ever leads to this node, and
// the first rp_origin when ROOT named none.
func (h *Handler) ownPage(asked string) (string, bool) {
	if asked == "" {
		return h.invitePage, true
	}
	u, err := url.Parse(asked)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	origin := u.Scheme + "://" + u.Host
	for _, own := range h.ownOrigins {
		if strings.TrimSuffix(own, "/") == origin {
			return origin + strings.TrimSuffix(u.EscapedPath(), "/"), true
		}
	}
	return "", false
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

// invitationSeen is what the link shows the invitee: the providers and the
// account ROOT named at each.
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
	h.writeJSON(w, http.StatusOK, map[string]any{"accounts": inv.Accounts})
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

// acceptInvitation is the link arriving with the account ROOT named, vouched by
// that provider: the User is made, USER and made by ROOT, and the invitation
// is spent. False with no error admits nobody.
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
			"invitation", inv.ID, "signs_in_with", inv.signsInWith(), "bindings", len(vouched))
		return "", nil, false, nil
	}
	route := matched.Claim.CanonicalID

	h.creating.Lock()
	defer h.creating.Unlock()

	existing, held, err := h.users.ByRoute(route)
	if err != nil {
		return "", nil, false, errors.Wrapf(err, "failed to read whether %s already reaches a User", route)
	}
	if held && existing.Level != LevelPublicRegistration {
		return "", nil, false, errors.Newf("%s already reaches User %s at %s, so invitation %s makes nobody", route, existing.ID, existing.Level, inv.ID)
	}

	now := time.Now().UTC().UnixMilli()
	var u User
	if held {
		u = raised(existing, inv, route, matched)
	} else {
		u = withRoute(User{
			Level:          LevelUser,
			CreatedBy:      inv.InvitedBy,
			DisplayName:    inv.DisplayName,
			CreatedAt:      now,
			EmailAddresses: []string{inv.Email},
		}, route, matched)
		u.ID, err = identity.GenerateUserID("user")
		if err != nil {
			return "", nil, false, errors.Wrapf(err, "failed to generate a User id for %s", route)
		}
	}
	spent, err := h.invitations.accept(inv.ID, u.ID, now)
	if err != nil {
		return "", nil, false, err
	}
	if !spent {
		return "", nil, false, nil
	}
	if err := h.users.Put(u); err != nil {
		return "", nil, false, errors.Wrapf(err, "invitation %s was spent and the User for %s was not written", inv.ID, route)
	}
	h.logger.Infow("User made USER by an invitation", "user", u.ID, "route", route, "invitation", inv.ID, "by", inv.InvitedBy, "was", existing.Level)
	h.attest(PredicateUserCreated, route, map[string]any{
		"user": u.ID, "invitation": inv.ID, "level": string(u.Level), "by": inv.InvitedBy, "email": inv.Email,
	})
	return route, matched, true, nil
}

// "When the invited account already has a public-registration User, should accepting the invitation raise that existing User to SUPER"

// "yes"

// "instead of SUPER, lets just mke a new level called USER"

// raised is a public registration made USER by ROOT's invitation: no longer
// at one door, made by ROOT, and holding what the provider just vouched.
func raised(u User, inv Invitation, route string, matched *SignedBinding) User {
	u.Level = LevelUser
	u.Namespace = ""
	u.CreatedBy = inv.InvitedBy
	if u.DisplayName == "" {
		u.DisplayName = inv.DisplayName
	}
	if !slices.Contains(u.EmailAddresses, inv.Email) {
		u.EmailAddresses = append(u.EmailAddresses, inv.Email)
	}
	for i, a := range u.Accounts {
		if a.CanonicalID != route {
			continue
		}
		if a.Provider == "" {
			a.Provider = matched.Claim.Provider
		}
		a.Binding = matched
		u.Accounts[i] = a
	}
	return u
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(s)
}
