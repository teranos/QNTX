package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	elementstorage "github.com/teranos/QNTX/element/storage"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// "given a namespace there can be multiple canvasses"
// "User ROOT and SUPER sees list of canvasses and who that canvas belongs to"
// "A User may want to add another user to their canvas as Owner of it by
// e-mail invitation. A beneficiary needs to accept it through the mail. The
// inviter received a message when the User has accepted it."

// PersonView is what a canvas's owner looks like on the list.
type PersonView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

// People is who the node knows, for naming owners and finding an invitee.
type People interface {
	Person(id string) (PersonView, bool)
	PersonByEmail(email string) (PersonView, bool)
}

// Mailer sends what the node mails in its own name (ADR-041).
type Mailer interface {
	SendAsNode(ctx context.Context, userID string, m services.NodeMail) (messageID, attestationID string, err error)
}

// WithPeople names owners and finds invitees.
func WithPeople(people People) CanvasHandlerOption {
	return func(h *CanvasHandler) { h.people = people }
}

// WithMailer sends invitations, and tells an inviter their invitee said yes.
func WithMailer(mailer Mailer) CanvasHandlerOption {
	return func(h *CanvasHandler) { h.mailer = mailer }
}

// WithInviteLink is where an invitee is sent to accept: the page, with the
// token on it.
func WithInviteLink(link func(token string) string) CanvasHandlerOption {
	return func(h *CanvasHandler) { h.inviteLink = link }
}

// canvasView is one canvas on the list, its owners named.
type canvasView struct {
	elementstorage.Canvas
	OwnerViews []PersonView `json:"owner_views"`
	Mine       bool         `json:"mine"`
}

func (h *CanvasHandler) view(c elementstorage.Canvas, admitted auth.Admission) canvasView {
	v := canvasView{Canvas: c, OwnerViews: []PersonView{}}
	for _, id := range c.Owners {
		if h.people != nil {
			if p, ok := h.people.Person(id); ok {
				v.OwnerViews = append(v.OwnerViews, p)
				continue
			}
		}
		v.OwnerViews = append(v.OwnerViews, PersonView{ID: id, Name: id})
	}
	v.Mine = slices.Contains(c.Owners, admitted.UserID)
	return v
}

// HandleCanvases lists the canvases of the namespace stood in, and creates one.
// Routes:
//
//	GET  /api/canvases                       - the canvases this caller may see
//	POST /api/canvases {"name","kind"}       - creates one; "namespace" is ROOT's and SUPER's to create
//	POST /api/canvases/{id}/disable|enable   - delete is disable
//	POST /api/canvases/{id}/owners {"user"}  - ROOT and SUPER make a User an owner
//	DELETE /api/canvases/{id}/owners/{user}  - takes an owner off; none left is "made unowned"
//	POST /api/canvases/{id}/access {"user"}  - grants a look at the namespace's canvas
//	DELETE /api/canvases/{id}/access/{user}  - takes it back
//	POST /api/canvases/{id}/invite {"email"} - an owner invites a User by mail
//	POST /api/canvases/accept {"token"}      - the invitee says yes
func (h *CanvasHandler) HandleCanvases(w http.ResponseWriter, r *http.Request) {
	store, ok := h.anyStoreOf(w, r)
	if !ok {
		return
	}
	admitted, _ := auth.AdmissionFrom(r.Context())
	rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/api/canvases"), "/")
	parts := strings.Split(rest, "/")

	switch {
	case rest == "" && r.Method == http.MethodGet:
		h.listCanvases(w, r, store, admitted)
	case rest == "" && r.Method == http.MethodPost:
		h.createCanvas(w, r, store, admitted)
	case rest == "accept" && r.Method == http.MethodPost:
		h.acceptInvitation(w, r, store, admitted)
	case len(parts) >= 2:
		h.oneCanvas(w, r, store, admitted, parts[0], parts[1:])
	default:
		h.writeError(w, errors.New("no such route"), http.StatusNotFound)
	}
}

func (h *CanvasHandler) listCanvases(w http.ResponseWriter, r *http.Request, store *elementstorage.CanvasStore, admitted auth.Admission) {
	all, err := store.Canvases(r.Context())
	if err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}
	views := []canvasView{}
	for _, c := range all {
		if h.mayAct(r, c) {
			views = append(views, h.view(c, admitted))
		}
	}
	h.writeJSON(w, views)
}

func (h *CanvasHandler) createCanvas(w http.ResponseWriter, r *http.Request, store *elementstorage.CanvasStore, admitted auth.Admission) {
	var body struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}
	if body.Kind == "" {
		body.Kind = elementstorage.CanvasOfAUser
	}
	if body.Kind == elementstorage.CanvasOfTheNamespace && !admitted.OwnsEveryCanvas() {
		h.writeError(w, errors.New("only ROOT, or SUPER here, creates the namespace's canvas"), http.StatusForbidden)
		return
	}
	if body.Kind == elementstorage.CanvasOfTheNamespace {
		if err := store.Create(r.Context(), body.Name, admitted.UserID); err != nil {
			h.writeCreateError(w, err)
			return
		}
		canvases, err := store.Canvases(r.Context())
		if err != nil {
			h.writeError(w, err, http.StatusInternalServerError)
			return
		}
		for _, c := range canvases {
			if c.Kind == elementstorage.CanvasOfTheNamespace {
				w.WriteHeader(http.StatusCreated)
				h.writeJSON(w, h.view(c, admitted))
				return
			}
		}
		return
	}
	c, err := store.CreateCanvas(r.Context(), body.Name, body.Kind, admitted.UserID)
	if err != nil {
		h.writeCreateError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, h.view(c, admitted))
}

func (h *CanvasHandler) writeCreateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, elementstorage.ErrCanvasExists):
		h.writeError(w, err, http.StatusConflict)
	case strings.Contains(err.Error(), "no name"), strings.Contains(err.Error(), "not \""):
		h.writeError(w, err, http.StatusBadRequest)
	default:
		h.writeError(w, err, http.StatusInternalServerError)
	}
}

// oneCanvas is everything done to a canvas by id.
func (h *CanvasHandler) oneCanvas(w http.ResponseWriter, r *http.Request, store *elementstorage.CanvasStore, admitted auth.Admission, id string, parts []string) {
	canvas, err := store.Canvas(r.Context(), id)
	if err != nil {
		if errors.Is(err, elementstorage.ErrNoSuchCanvas) {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}
	if !h.mayAct(r, canvas) {
		h.writeError(w, errors.Newf("the canvas %s is not yours", id), http.StatusForbidden)
		return
	}
	action := parts[0]
	var who string
	if len(parts) > 1 {
		who = parts[1]
	}
	privileged := admitted.OwnsEveryCanvas()

	switch {
	case action == "disable" && r.Method == http.MethodPost:
		err = store.Disable(r.Context(), id, admitted.UserID)
	case action == "enable" && r.Method == http.MethodPost:
		err = store.Enable(r.Context(), id)
	case action == "owners" && r.Method == http.MethodPost:
		if !privileged {
			h.writeError(w, errors.New("an owner is added by invitation; ROOT and SUPER add one outright"), http.StatusForbidden)
			return
		}
		who, err = h.userInBody(r)
		if err == nil {
			err = store.AddOwner(r.Context(), id, who)
		}
	case action == "owners" && r.Method == http.MethodDelete && who != "":
		if !privileged && who != admitted.UserID {
			h.writeError(w, errors.New("only ROOT and SUPER take another owner off"), http.StatusForbidden)
			return
		}
		err = store.RemoveOwner(r.Context(), id, who)
	case action == "owners" && r.Method == http.MethodDelete:
		if !privileged {
			h.writeError(w, errors.New("only ROOT and SUPER make a canvas unowned"), http.StatusForbidden)
			return
		}
		err = store.Disown(r.Context(), id)
	case action == "access" && r.Method == http.MethodPost:
		if !privileged {
			h.writeError(w, errors.New("only ROOT and SUPER grant the namespace's canvas"), http.StatusForbidden)
			return
		}
		who, err = h.userInBody(r)
		if err == nil {
			err = store.GrantAccess(r.Context(), id, who)
		}
	case action == "access" && r.Method == http.MethodDelete && who != "":
		if !privileged {
			h.writeError(w, errors.New("only ROOT and SUPER revoke the namespace's canvas"), http.StatusForbidden)
			return
		}
		err = store.RevokeAccess(r.Context(), id, who)
	case action == "invite" && r.Method == http.MethodPost:
		h.invite(w, r, store, admitted, canvas)
		return
	default:
		h.writeError(w, errors.New("no such route"), http.StatusNotFound)
		return
	}
	if err != nil {
		switch {
		case errors.Is(err, elementstorage.ErrNoSuchCanvas):
			h.writeError(w, err, http.StatusNotFound)
		case strings.Contains(err.Error(), "is named"):
			h.writeError(w, err, http.StatusBadRequest)
		default:
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}
	canvas, err = store.Canvas(r.Context(), id)
	if err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, h.view(canvas, admitted))
}

func (h *CanvasHandler) userInBody(r *http.Request) (string, error) {
	var body struct {
		User string `json:"user"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return "", errors.Wrap(err, "invalid request body")
	}
	if body.User == "" {
		return "", errors.New("a User is named, and this one is not")
	}
	return body.User, nil
}

// invite mails a User an invitation to own this canvas.
func (h *CanvasHandler) invite(w http.ResponseWriter, r *http.Request, store *elementstorage.CanvasStore, admitted auth.Admission, canvas elementstorage.Canvas) {
	if h.people == nil || h.mailer == nil || h.inviteLink == nil {
		h.writeError(w, errors.New("this node keeps no Users to invite, or sends no mail"), http.StatusNotImplemented)
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(strings.ToLower(body.Email))
	if email == "" {
		h.writeError(w, errors.New("email is required"), http.StatusBadRequest)
		return
	}
	invitee, ok := h.people.PersonByEmail(email)
	if !ok {
		h.writeError(w, errors.Newf("no User has the address %s", email), http.StatusNotFound)
		return
	}
	if slices.Contains(canvas.Owners, invitee.ID) {
		h.writeError(w, errors.Newf("%s already owns %s", invitee.Name, canvas.Name), http.StatusConflict)
		return
	}
	inv, err := store.Invite(r.Context(), canvas.ID, admitted.UserID, invitee.ID, email)
	if err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}
	inviter := admitted.DisplayName
	if inviter == "" {
		inviter = admitted.UserID
	}
	link := h.inviteLink(inv.Token)
	_, _, err = h.mailer.SendAsNode(r.Context(), invitee.ID, services.NodeMail{
		Name:    "canvas-invitation",
		Subject: inviter + " invites you to own the canvas " + canvas.Name,
		Text:    inviter + " invites you to own the canvas " + canvas.Name + ".\n\nAccept it here: " + link + "\n",
		HTML: "<p>" + htmlEscape(inviter) + " invites you to own the canvas <b>" + htmlEscape(canvas.Name) + "</b>.</p>" +
			"<p><a href=\"" + htmlEscape(link) + "\">Accept the invitation</a></p>",
	})
	if err != nil {
		h.writeError(w, errors.Wrap(err, "the invitation was written, and the mail was not sent"), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	h.writeJSON(w, map[string]string{"invited": invitee.ID, "email": email})
}

// acceptInvitation is the invitee saying yes, and the inviter hearing it.
func (h *CanvasHandler) acceptInvitation(w http.ResponseWriter, r *http.Request, store *elementstorage.CanvasStore, admitted auth.Admission) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}
	inv, err := store.Accept(r.Context(), body.Token, admitted.UserID)
	if err != nil {
		switch {
		case errors.Is(err, elementstorage.ErrNoSuchInvitation):
			h.writeError(w, err, http.StatusNotFound)
		case strings.Contains(err.Error(), "not "):
			h.writeError(w, err, http.StatusForbidden)
		default:
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}
	canvas, err := store.Canvas(r.Context(), inv.CanvasID)
	if err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}
	if h.mailer != nil {
		invitee := admitted.DisplayName
		if invitee == "" {
			invitee = admitted.UserID
		}
		if _, _, err := h.mailer.SendAsNode(r.Context(), inv.Inviter, services.NodeMail{
			Name:    "canvas-invitation-accepted",
			Subject: invitee + " now owns the canvas " + canvas.Name + " with you",
			Text:    invitee + " accepted your invitation and now owns the canvas " + canvas.Name + " with you.\n",
			HTML:    "<p>" + htmlEscape(invitee) + " accepted your invitation and now owns the canvas <b>" + htmlEscape(canvas.Name) + "</b> with you.</p>",
		}); err != nil {
			h.logWarn("The inviter %s was not told %s accepted: %v", inv.Inviter, admitted.UserID, err)
		}
	}
	h.writeJSON(w, h.view(canvas, admitted))
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(s)
}
