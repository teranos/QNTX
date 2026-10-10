package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin/grpc/services"
)

// "As ROOT i send an invite link to a friend, i enter their e-mail address"

// "A mail actually goes out to both me and my friend"

// "the MAIL ROOT received has a button for cancelling the invitation"

// "so, if ROOT selected Mastodon, the invited user only sees the mastodon link, and only the mastodon acc specified by ROOT would be applicable"

// sentMail is one mail the node would have sent: to a User by id, or to an
// address nobody holds yet.
type sentMail struct {
	userID string
	to     string
	mail   services.NodeMail
}

type mailbox struct{ sent []sentMail }

func (m *mailbox) SendAsNode(_ context.Context, userID string, mail services.NodeMail) (string, string, error) {
	m.sent = append(m.sent, sentMail{userID: userID, mail: mail})
	return "msg", "AS", nil
}

func (m *mailbox) SendAsNodeTo(_ context.Context, u services.MailRecipient, mail services.NodeMail) (string, string, error) {
	m.sent = append(m.sent, sentMail{userID: u.ID, to: u.Email, mail: mail})
	return "msg", "AS", nil
}

const invitePage = "https://q.example"

// Tim de Facile is ROOT, with a session, a node that sends mail, and a binding
// signer the node trusts.
func invitingHandler(t *testing.T) (*Handler, *memUsers, string, *mailbox, ed25519.PrivateKey) {
	t.Helper()
	h, store, rootSession := switchingHandler(t)
	signerPub, signer, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	h.SetIdentities([]string{mastodonAccount}, []string{hex.EncodeToString(signerPub)})
	db := qntxtest.CreateTestDB(t)
	h.creds = newCredentialStore(db, h.logger)
	h.invitations = newInvitationTable(db)
	box := &mailbox{}
	h.SetInvitationMail(box, invitePage)
	return h, store, rootSession, box, signer
}

func inviting(h *Handler, session, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/auth/invitations", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	h.Routes()["/auth/invitations"](rec, req)
	return rec
}

func onInvitation(h *Handler, session, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	}
	rec := httptest.NewRecorder()
	h.Routes()["/auth/invitations/"](rec, req)
	return rec
}

// tokenIn is what follows marker in a link, up to the end of the line or the
// quote that closes an href.
func tokenIn(t *testing.T, body, marker string) string {
	t.Helper()
	at := strings.Index(body, marker)
	require.GreaterOrEqual(t, at, 0, "no %s in %s", marker, body)
	rest := body[at+len(marker):]
	if end := strings.IndexAny(rest, "\"\n <&"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// The invitee arrives through laye carrying the invitation they were sent.
func inviteeArrives(t *testing.T, h *Handler, browser ed25519.PrivateKey, invitation string, bindings []SignedBinding) *httptest.ResponseRecorder {
	t.Helper()
	challenge, err := h.layeChallenges.issue()
	require.NoError(t, err)
	body, err := json.Marshal(layeVerifyRequest{
		DID:        EncodeDIDKey(browser.Public().(ed25519.PublicKey)),
		Challenge:  challenge,
		Signature:  base64.RawURLEncoding.EncodeToString(ed25519.Sign(browser, []byte(challenge))),
		Bindings:   bindings,
		Invitation: invitation,
	})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/auth/laye/verify", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.handleLayeVerify(w, r)
	return w
}

func newBrowser(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return browser
}

const adaInvite = `{"email":"ada@gmail.com","accounts":[{"provider":"google","account":"ada@gmail.com"}]}`

// "if both google and apple, then we set both, and  if set then we set the mail address of that provider"

// "or the username"
const adaAnywhere = `{"display_name":"Ada","email":"ada@gmail.com","accounts":[` +
	`{"provider":"google","account":"ada@gmail.com"},{"provider":"github","account":"adalovelace"}]}`

// ROOT invites Ada: a mail goes to Ada with the link, and one to ROOT with
// the cancel.
func TestRootInvitesAndBothAreMailed(t *testing.T) {
	h, store, rootSession, box, _ := invitingHandler(t)
	root := store.held[0]

	rec := inviting(h, rootSession, adaInvite)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.Len(t, box.sent, 2)
	invitee, rootCopy := box.sent[0], box.sent[1]
	assert.Equal(t, "ada@gmail.com", invitee.to)
	assert.Contains(t, invitee.mail.HTML, invitePage+"/?invitation=")
	assert.Equal(t, root.ID, rootCopy.userID)
	assert.Contains(t, rootCopy.mail.HTML, invitePage+"/?invitation-cancel=")
	assert.Contains(t, rootCopy.mail.Text, "ada@gmail.com")
}

// The links open on the page ROOT invited from, when that page is this node's
// own: a branch's page at /branch/<name> is. Any other origin is not.
func TestTheLinksOpenOnThePageRootInvitedFrom(t *testing.T) {
	h, _, rootSession, box, _ := invitingHandler(t)
	h.ownOrigins = []string{invitePage}
	branch := invitePage + "/branch/root-invites-a-user/"

	rec := inviting(h, rootSession, `{"email":"ada@gmail.com","accounts":[{"provider":"google","account":"ada@gmail.com"}],"page":"`+branch+`"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Contains(t, box.sent[0].mail.HTML, invitePage+"/branch/root-invites-a-user/?invitation=")
	assert.Contains(t, box.sent[1].mail.HTML, invitePage+"/branch/root-invites-a-user/?invitation-cancel=")

	rec = inviting(h, rootSession, `{"email":"ada@gmail.com","accounts":[{"provider":"google","account":"ada@gmail.com"}],"page":"https://elsewhere.example/"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Len(t, box.sent, 2)
}

// Only ROOT invites.
func TestOnlyRootInvites(t *testing.T) {
	h, store, _, box, _ := invitingHandler(t)
	ada := User{ID: "US-ADA-1", Level: LevelUser, Accounts: []UserAccount{{Provider: "google", CanonicalID: "google:110"}}}
	require.NoError(t, store.Put(ada))
	adaSession, err := h.sessions.create("google:110", ada)
	require.NoError(t, err)

	rec := inviting(h, adaSession, `{"email":"bob@gmail.com","accounts":[{"provider":"google","account":"bob@gmail.com"}]}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Empty(t, box.sent)
}

// An invitation names an address, and at least one provider with the account
// there.
func TestAnInvitationNamesAnAddressAndAnAccount(t *testing.T) {
	h, _, rootSession, box, _ := invitingHandler(t)
	for _, body := range []string{
		`{"accounts":[{"provider":"google","account":"ada@gmail.com"}]}`,
		`{"email":"ada@gmail.com","accounts":[]}`,
		`{"email":"ada@gmail.com","accounts":[{"account":"ada@gmail.com"}]}`,
		`{"email":"ada@gmail.com","accounts":[{"provider":"google"}]}`,
		`{"email":"ada@gmail.com","accounts":[{"provider":"google","account":"a"},{"provider":"google","account":"b"}]}`,
	} {
		assert.Equal(t, http.StatusBadRequest, inviting(h, rootSession, body).Code, body)
	}
	assert.Empty(t, box.sent)
}

// The link says which providers the invitee signs in with, and only those.
func TestTheLinkNamesTheProvidersRootSet(t *testing.T) {
	h, _, rootSession, box, _ := invitingHandler(t)
	require.Equal(t, http.StatusCreated, inviting(h, rootSession, adaAnywhere).Code)
	token := tokenIn(t, box.sent[0].mail.HTML, "?invitation=")

	rec := onInvitation(h, "", http.MethodGet, "/auth/invitations/"+token)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var seen struct {
		Accounts []InvitationAccount `json:"accounts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &seen))
	assert.Equal(t, []InvitationAccount{
		{Provider: "github", Account: "adalovelace"},
		{Provider: "google", Account: "ada@gmail.com"},
	}, seen.Accounts)
}

// With Google and GitHub set, either one admits her: GitHub by the username.
func TestEitherProviderRootSetAdmitsTheInvitee(t *testing.T) {
	h, store, rootSession, box, signer := invitingHandler(t)
	require.Equal(t, http.StatusCreated, inviting(h, rootSession, adaAnywhere).Code)
	token := tokenIn(t, box.sent[0].mail.HTML, "?invitation=")

	browser := newBrowser(t)
	rec := inviteeArrives(t, h, browser, token, []SignedBinding{
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "github", "github:42", "adalovelace"),
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.held, 2)
	assert.True(t, store.held[1].Reaches("github:42"))
}

// Ada signs in with the Google account ROOT named, and is a User: USER, made
// by ROOT, holding that account and the address she was invited at.
func TestTheInviteeSignsInWithTheAccountRootNamed(t *testing.T) {
	h, store, rootSession, box, signer := invitingHandler(t)
	root := store.held[0]
	require.Equal(t, http.StatusCreated, inviting(h, rootSession, adaInvite).Code)
	token := tokenIn(t, box.sent[0].mail.HTML, "?invitation=")

	browser := newBrowser(t)
	vouched := vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:110", "ada@gmail.com")
	rec := inviteeArrives(t, h, browser, token, []SignedBinding{vouched})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"next":"enrol"`)

	require.Len(t, store.held, 2)
	ada := store.held[1]
	assert.Equal(t, LevelUser, ada.Level)
	assert.Equal(t, root.ID, ada.CreatedBy)
	assert.Equal(t, []string{"ada@gmail.com"}, ada.EmailAddresses)
	assert.True(t, ada.Reaches("google:110"))
	assert.Equal(t, LevelUser, h.levelOf("google:110"))

	// Spent: the link admits nobody a second time.
	again := inviteeArrives(t, h, newBrowser(t), token, []SignedBinding{
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:110", "ada@gmail.com"),
	})
	assert.NotEqual(t, http.StatusOK, again.Code)

	// And the next time she signs in she needs no link: the account is hers.
	_, _, ok := h.admits(EncodeDIDKey(browser.Public().(ed25519.PublicKey)), []SignedBinding{vouched})
	assert.True(t, ok, "Ada's own account did not admit her without the invitation")
}

// "and i want to set Name"
func TestTheInviteeCarriesTheNameRootGave(t *testing.T) {
	h, store, rootSession, box, signer := invitingHandler(t)
	rec := inviting(h, rootSession, adaAnywhere)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Contains(t, box.sent[1].mail.Text, "Ada")
	token := tokenIn(t, box.sent[0].mail.HTML, "?invitation=")

	browser := newBrowser(t)
	arrived := inviteeArrives(t, h, browser, token, []SignedBinding{
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:110", "ada@gmail.com"),
	})
	require.Equal(t, http.StatusOK, arrived.Code, arrived.Body.String())
	assert.Equal(t, "Ada", store.held[1].DisplayName)

	// root is the one name no other User may take.
	assert.Equal(t, http.StatusBadRequest,
		inviting(h, rootSession, `{"display_name":"root","email":"bob@gmail.com","accounts":[{"provider":"google","account":"bob@gmail.com"}]}`).Code)
}

// "When the invited account already has a public-registration User, should accepting the invitation raise that existing User to SUPER"

// "yes"

// "instead of SUPER, lets just mke a new level called USER"

// Ada registered at two public doors before she was invited. The invitation
// raises one of those Users to USER, and every later lookup of her account
// finds that one.
func TestAnInvitationRaisesAPublicRegistration(t *testing.T) {
	h, store, rootSession, box, signer := invitingHandler(t)
	root := store.held[0]
	for _, door := range []string{"clean", "garden"} {
		require.NoError(t, store.Put(User{
			ID: "US-ADA-" + door, Level: LevelPublicRegistration, Namespace: door,
			Accounts: []UserAccount{{Provider: "google", CanonicalID: "google:110", Handle: "ada@gmail.com"}},
		}))
	}
	require.Equal(t, http.StatusCreated, inviting(h, rootSession, adaAnywhere).Code)
	token := tokenIn(t, box.sent[0].mail.HTML, "?invitation=")

	browser := newBrowser(t)
	rec := inviteeArrives(t, h, browser, token, []SignedBinding{
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:110", "ada@gmail.com"),
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.held, 3, "a second User was made for an account that already had one")

	raised, found, err := store.ByRoute("google:110")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, LevelUser, raised.Level)
	assert.Empty(t, raised.Namespace, "a USER belongs to no door")
	assert.Equal(t, root.ID, raised.CreatedBy)
	assert.Equal(t, "Ada", raised.DisplayName)
	assert.Equal(t, LevelUser, h.levelOf("google:110"))
}

// Another account at the provider is not the one ROOT named.
func TestAnotherAccountIsNotApplicable(t *testing.T) {
	h, store, rootSession, box, signer := invitingHandler(t)
	require.Equal(t, http.StatusCreated, inviting(h, rootSession, adaInvite).Code)
	token := tokenIn(t, box.sent[0].mail.HTML, "?invitation=")

	browser := newBrowser(t)
	for _, b := range []SignedBinding{
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:999", "bob@gmail.com"),
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "apple", "apple:110", "ada@gmail.com"),
	} {
		rec := inviteeArrives(t, h, browser, token, []SignedBinding{b})
		assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	}
	assert.Len(t, store.held, 1)
}

// "zero means zero"
func TestAnInvitationCancelledAtZeroIsCancelled(t *testing.T) {
	table := newInvitationTable(qntxtest.CreateTestDB(t))
	require.NoError(t, table.put(Invitation{ID: "inv-0", Email: "ada@gmail.com", InvitedBy: "US-ROOT", CreatedAt: 1,
		Accounts: []InvitationAccount{{Provider: "google", Account: "ada@gmail.com"}}}, "token-0"))

	held, found, err := table.byID("inv-0")
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, held.open(), "an invitation nobody cancelled or used is not open")

	closed, err := table.cancel("inv-0", 0)
	require.NoError(t, err)
	require.True(t, closed)

	held, _, err = table.byID("inv-0")
	require.NoError(t, err)
	assert.False(t, held.open(), "an invitation cancelled at the second 0 still admits")
}

// ROOT cancels, and the link admits nobody.
func TestRootCancelsTheInvitation(t *testing.T) {
	h, store, rootSession, box, signer := invitingHandler(t)
	require.Equal(t, http.StatusCreated, inviting(h, rootSession, adaInvite).Code)
	token := tokenIn(t, box.sent[0].mail.HTML, "?invitation=")
	id := tokenIn(t, box.sent[1].mail.HTML, "?invitation-cancel=")

	rec := onInvitation(h, rootSession, http.MethodPost, "/auth/invitations/"+id+"/cancel")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, http.StatusGone, onInvitation(h, "", http.MethodGet, "/auth/invitations/"+token).Code)
	browser := newBrowser(t)
	arrived := inviteeArrives(t, h, browser, token, []SignedBinding{
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:110", "ada@gmail.com"),
	})
	assert.Equal(t, http.StatusForbidden, arrived.Code, arrived.Body.String())
	assert.Len(t, store.held, 1)
}
