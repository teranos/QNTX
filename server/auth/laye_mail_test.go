package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/services"
)

// "1. but laye could use QNTX to send mail."

// "right, and QNTX mail allows you to set a custom template, try to make it fit laye theme"

// "A mail actually goes out to both me and my friend"

// "the MAIL ROOT received has a button for cancelling the invitation"

const layeOrigin = "https://laye.example"

// layeMailing is a node that sends mail, with laye in front of it and laye as
// its primary.
func layeMailing(t *testing.T) (*Handler, *mailbox, ed25519.PrivateKey) {
	t.Helper()
	h, _, _, box, _ := invitingHandler(t)
	_, nodeKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	h.SetNodeKey(nodeKey)
	_, laye, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	require.NoError(t, h.SetLaye(EncodeDIDKey(laye.Public().(ed25519.PublicKey))))
	require.NoError(t, h.SetPrimary(layeOrigin))
	return h, box, laye
}

// layeSigns is what laye signs to ask this node something as itself.
func layeSigns(t *testing.T, h *Handler, key ed25519.PrivateKey) string {
	t.Helper()
	nodeDID, err := h.ownDID()
	require.NoError(t, err)
	did := EncodeDIDKey(key.Public().(ed25519.PublicKey))
	jti := make([]byte, assertionIDBytes)
	_, err = rand.Read(jti)
	require.NoError(t, err)
	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.RegisteredClaims{
		Issuer: did, Subject: did, Audience: jwt.ClaimStrings{nodeDID},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)), ID: hex.EncodeToString(jti),
	}).SignedString(key)
	require.NoError(t, err)
	return signedScheme + signed
}

func layeInvited(h *Handler, authorization string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, layeInvitedPath, strings.NewReader(string(raw)))
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	h.Routes()[layeInvitedPath](rec, req)
	return rec
}

// laye's own template: its look, filled with what laye says.
var layesTemplate = layeTemplate{
	Subject: "{{.subject}}",
	Text:    "{{.body}}\n{{.link}}",
	HTML:    `<div style="background:#050608;color:#e6e2e2">l a y e<p>{{.body}}</p><a href="{{.link}}">{{.link_label}}</a></div>`,
}

func invitedAtLaye() layeInvitation {
	return layeInvitation{
		Invitation: "abc123",
		Email:      "tim@example.com",
		InvitedBy:  mastodonAccount,
		Friend: layeMail{Template: layesTemplate, Values: map[string]string{
			"subject": "You are invited to laye", "body": "Sign in with github github:42.",
			"link": "https://laye.example/laye/me", "link_label": "Accept the invitation",
		}},
		Root: layeMail{Template: layesTemplate, Values: map[string]string{
			"subject": "You invited tim@example.com", "body": "Cancel it here.",
			"link": "https://laye.example/laye/me?cancel=abc123", "link_label": "Cancel the invitation",
		}},
	}
}

func TestLayeHasTheNodeMailAnInvitationToTheFriendAndTheRoot(t *testing.T) {
	h, box, laye := layeMailing(t)
	rec := layeInvited(h, layeSigns(t, h, laye), invitedAtLaye())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, box.sent, 2)

	friend := box.sent[0]
	assert.Equal(t, "tim@example.com", friend.to)
	assert.Equal(t, "You are invited to laye", friend.mail.Subject)
	html, err := services.BodyOf(friend.mail.Bodies, services.MailHTML)
	require.NoError(t, err)
	assert.Contains(t, html, "#050608", "the mail wears laye's template")
	assert.Contains(t, html, `href="https://laye.example/laye/me"`)

	root := box.sent[1]
	assert.NotEmpty(t, root.userID, "the root's copy goes to the User its route reaches")
	text, err := services.BodyOf(root.mail.Bodies, services.MailText)
	require.NoError(t, err)
	assert.Contains(t, text, "https://laye.example/laye/me?cancel=abc123")
}

func TestOnlyLayeHasTheNodeMail(t *testing.T) {
	h, box, _ := layeMailing(t)
	_, other, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, layeInvited(h, layeSigns(t, h, other), invitedAtLaye()).Code)
	assert.Equal(t, http.StatusUnauthorized, layeInvited(h, "", invitedAtLaye()).Code)
	assert.Empty(t, box.sent)
}

func TestLayesSignatureHasTheNodeMailOnce(t *testing.T) {
	h, box, laye := layeMailing(t)
	signed := layeSigns(t, h, laye)
	require.Equal(t, http.StatusOK, layeInvited(h, signed, invitedAtLaye()).Code)
	assert.Equal(t, http.StatusUnauthorized, layeInvited(h, signed, invitedAtLaye()).Code)
	assert.Len(t, box.sent, 2)
}

// The node's mail leads only to laye: a link anywhere else is refused.
func TestAMailLeadsOnlyToLaye(t *testing.T) {
	h, box, laye := layeMailing(t)
	elsewhere := invitedAtLaye()
	elsewhere.Friend.Values["link"] = "https://evil.example/"
	assert.Equal(t, http.StatusBadRequest, layeInvited(h, layeSigns(t, h, laye), elsewhere).Code)
	assert.Empty(t, box.sent)
}

func TestARootTheNodeDoesNotKnowGetsNoCopy(t *testing.T) {
	h, box, laye := layeMailing(t)
	nobody := invitedAtLaye()
	nobody.InvitedBy = "github:999"
	rec := layeInvited(h, layeSigns(t, h, laye), nobody)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Empty(t, box.sent, "nothing goes to the friend when the root's copy cannot")
}

func TestATemplateThatDoesNotFillSendsNothing(t *testing.T) {
	h, box, laye := layeMailing(t)
	broken := invitedAtLaye()
	delete(broken.Root.Values, "link_label")
	assert.Equal(t, http.StatusBadRequest, layeInvited(h, layeSigns(t, h, laye), broken).Code)
	assert.Empty(t, box.sent)
}
