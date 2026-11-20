package auth

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	errors "github.com/teranos/sacred-error"
)

// "1. but laye could use QNTX to send mail."

// "right, and QNTX mail allows you to set a custom template, try to make it fit laye theme"

// layeInvitedPath is where laye has the node mail an invitation laye holds:
// to the friend, and the root's copy with its cancel.
const layeInvitedPath = "/auth/laye/invited"

// layeTemplate is a mail's look, laye's own: a subject, a text body and an
// html body, filled as a plugin's template is (services.FillTemplate).
type layeTemplate struct {
	Subject string `json:"subject"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
}

// layeMail is one mail: laye's template, and what it is filled with. Its
// link is where the mail leads, and it leads only to laye.
type layeMail struct {
	Template layeTemplate      `json:"template"`
	Values   map[string]string `json:"values"`
}

// layeInvitation is an invitation laye holds, as laye has it mailed.
type layeInvitation struct {
	Invitation string   `json:"invitation"`
	Email      string   `json:"email"`
	InvitedBy  string   `json:"invited_by"`
	Friend     layeMail `json:"friend"`
	Root       layeMail `json:"root"`
}

// layeAsking is whether a request is laye's own: signed by laye's key, as
// itself, for this node, short-lived and presented once.
func (h *Handler) layeAsking(r *http.Request, now time.Time) error {
	if h.laye == "" {
		return errors.New("no laye is in front of this node (laye.did)")
	}
	signed, ok := signedAssertion(r)
	if !ok {
		return errors.New("laye signs what it asks of this node")
	}
	key, err := DecodeUserDID(h.laye)
	if err != nil {
		return errors.Wrapf(err, "laye.did %q names no key", h.laye)
	}
	audience, err := h.ownDID()
	if err != nil {
		return err
	}
	var claims jwt.RegisteredClaims
	if _, err := jwt.ParseWithClaims(signed, &claims, func(*jwt.Token) (any, error) { return key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(h.laye),
		jwt.WithSubject(h.laye),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	); err != nil {
		return errors.Wrap(err, "the signature is not laye's, for this node")
	}
	if claims.ExpiresAt.Time.After(now.Add(assertionMaxLife)) {
		return errors.Newf("the signature lasts past the %s one may", assertionMaxLife)
	}
	if id, err := hex.DecodeString(claims.ID); err != nil || len(id) != assertionIDBytes {
		return errors.Newf("the signature's jti %q is not %d random bytes in hex", claims.ID, assertionIDBytes)
	}
	if _, fresh := h.assertions.spend(claims.ID, claims.ExpiresAt.Time, now); !fresh {
		return errors.Newf("the signature %s was already presented", claims.ID)
	}
	return nil
}

// filled is a mail laye's template makes, leading only to laye.
func (h *Handler) filled(m layeMail, name string) (services.NodeMail, error) {
	if !h.logsInElsewhere || !strings.HasPrefix(m.Values["link"], h.loginAt+"/") {
		return services.NodeMail{}, errors.Newf("the %s mail leads to %q, and this node's mail leads only to laye (%s)", name, m.Values["link"], h.loginAt)
	}
	filled, err := services.FillTemplate(&protocol.MailTemplate{
		Subject: m.Template.Subject,
		Bodies:  []*protocol.MailTemplateBody{services.TemplateText(m.Template.Text), services.TemplateHTML(m.Template.HTML)},
	}, m.Values)
	if err != nil {
		return services.NodeMail{}, errors.Wrapf(err, "the %s mail does not fill", name)
	}
	return services.NodeMail{Name: name, Subject: filled.Subject, Bodies: filled.Bodies.All()}, nil
}

// handleLayeInvited mails an invitation laye holds: the friend's to their
// address, and the root's copy to the User the root's route reaches. Nothing
// goes when either cannot.
func (h *Handler) handleLayeInvited(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := h.layeAsking(r, time.Now()); err != nil {
		h.writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if h.inviteMailer == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node sends no mail, so an invitation cannot reach anybody")
		return
	}
	var asked layeInvitation
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&asked); err != nil {
		h.writeError(w, http.StatusBadRequest, "the body is not an invitation laye holds")
		return
	}
	email := strings.TrimSpace(asked.Email)
	if email == "" || !strings.Contains(email, "@") || strings.ContainsFunc(email, unicode.IsSpace) {
		h.writeError(w, http.StatusBadRequest, "email is the friend's e-mail address")
		return
	}
	if asked.Invitation == "" || strings.ContainsFunc(asked.Invitation, unicode.IsSpace) {
		h.writeError(w, http.StatusBadRequest, "invitation is the id laye holds it under")
		return
	}
	friend, err := h.filled(asked.Friend, "laye-invitation")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	root, err := h.filled(asked.Root, "laye-invitation-sent")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, found, err := h.users.ByRoute(asked.InvitedBy)
	if err != nil {
		h.writeError(w, http.StatusServiceUnavailable, "the User store did not answer: "+err.Error())
		return
	}
	if !found {
		h.writeError(w, http.StatusNotFound, "no User here is reached by "+asked.InvitedBy+", so the root's copy has nobody to go to")
		return
	}

	if _, _, err := h.inviteMailer.SendAsNodeTo(r.Context(), services.AddressRecipient("laye-invitation:"+asked.Invitation, email), friend); err != nil {
		h.logger.Errorw("laye's invitation was not mailed", "invitation", asked.Invitation, "email", email, "error", err)
		h.writeError(w, http.StatusBadGateway, "the mail to "+email+" was not sent: "+err.Error())
		return
	}
	if _, _, err := h.inviteMailer.SendAsNode(r.Context(), user.ID, root); err != nil {
		h.logger.Errorw("laye's invitation went out, the root's copy did not", "invitation", asked.Invitation, "user", user.ID, "error", err)
		h.writeError(w, http.StatusBadGateway, "the invitation went to "+email+" and the root's copy with the cancel was not sent: "+err.Error())
		return
	}
	h.attest(PredicateInvited, asked.InvitedBy, map[string]any{
		"invitation": asked.Invitation, "email": email, "by": asked.InvitedBy, "at": "laye",
	})
	h.writeJSON(w, http.StatusOK, map[string]string{"invitation": asked.Invitation, "mailed": email})
}
