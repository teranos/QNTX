package qntxinbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// by is a sigil call from a User at a level, in Clean.
func by(method, target, body, user, level string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("X-Qntx-Asker", "did:key:z6Mk"+user)
	req.Header.Set("X-Qntx-Asker-User", user)
	req.Header.Set("X-Qntx-Asker-Level", level)
	req.Header.Set("X-Qntx-Store-Token", "call")
	req.Header.Set("X-Qntx-Namespace", "Clean")
	return req
}

const sending = `{"from":"timothy@example.com","to":["ada@example.com"],"subject":"Keys","text_body":"Under the mat."}`

// A User sends text from an address they hold, and it is filed under sent.
func TestAUserSendsFromTheirAddress(t *testing.T) {
	st := &heldStore{}
	st.grant("timothy@example.com", "US-TIM-7K4M3B9X")
	p := pluginWith(st)

	w := serve(t, p, by(http.MethodPost, "/send", sending, "US-TIM-7K4M3B9X", "ATTESTOR"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	sent := p.sender.(*outbox).sent
	require.Len(t, sent, 1)
	assert.Equal(t, outgoing{From: "timothy@example.com", To: []string{"ada@example.com"}, Subject: "Keys", Text: "Under the mat."}, sent[0])
	filed := st.wrote(PredicateMailSent)
	require.Len(t, filed, 1)
	assert.Equal(t, []string{"ses-1"}, filed[0].Subjects)
	assert.Equal(t, []string{"timothy@example.com"}, filed[0].Contexts)
	assert.Equal(t, MailboxSent, filed[0].Attributes["mailbox"])
	assert.Equal(t, []string{"did:key:z6MkUS-TIM-7K4M3B9X"}, filed[0].Actors)
}

// Nobody sends from an address they do not hold, ROOT included.
func TestNobodySendsFromAnotherUsersAddress(t *testing.T) {
	for _, level := range []string{"ATTESTOR", "ROOT"} {
		st := &heldStore{}
		st.grant("timothy@example.com", "US-TIM-7K4M3B9X")
		p := pluginWith(st)
		w := serve(t, p, by(http.MethodPost, "/send", sending, "US-ADA-0000000", level))
		assert.Equal(t, http.StatusForbidden, w.Code, "%s: %s", level, w.Body.String())
		assert.Empty(t, p.sender.(*outbox).sent, "%s sent from another User's address", level)
	}
}

func mailboxOf(t *testing.T, p *Plugin, user, level, mailbox string) (int, []*protocol.Email) {
	t.Helper()
	w := serve(t, p, by(http.MethodGet, "/mailbox?address=timothy@example.com&mailbox="+mailbox, "", user, level))
	var resp protocol.QueryEmailsResponse
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	}
	return w.Code, resp.List
}

// A User reads their own address's inbox, junk and sent, each its own.
func TestAUserReadsTheirOwnMailboxes(t *testing.T) {
	st := &heldStore{}
	st.grant("timothy@example.com", "US-TIM-7K4M3B9X")
	p := pluginWith(st)
	b := &bucket{objects: map[string][]byte{
		"inbound/m1": stored("timothy@example.com", "PASS", "PASS"),
		"inbound/m2": stored("timothy@example.com", "FAIL", "PASS"),
	}}
	p.bag = b
	_, err := p.receive(t.Context(), st)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serve(t, p, by(http.MethodPost, "/send", sending, "US-TIM-7K4M3B9X", "ATTESTOR")).Code)

	for mailbox, subject := range map[string]string{MailboxInbox: "m1", MailboxJunk: "m2", MailboxSent: "ses-1"} {
		code, list := mailboxOf(t, p, "US-TIM-7K4M3B9X", "ATTESTOR", mailbox)
		require.Equal(t, http.StatusOK, code, mailbox)
		require.Len(t, list, 1, mailbox)
		assert.Equal(t, []string{mailbox}, list[0].GetMailboxIds())
		assert.NotEmpty(t, list[0].GetId())
		_ = subject
	}
	assert.Empty(t, st.wrote(PredicateMailRead), "a User reading their own mail was attested as a read")
}

// Another User's mail is not read.
func TestAUserDoesNotReadAnotherUsersMail(t *testing.T) {
	st := &heldStore{}
	st.grant("timothy@example.com", "US-TIM-7K4M3B9X")
	code, _ := mailboxOf(t, pluginWith(st), "US-ADA-0000000", "SUPER", MailboxInbox)
	assert.Equal(t, http.StatusForbidden, code)
}

// "ROOT should be able to read other user's mail as well, but doing so is an attested event."
func TestROOTReadingAnotherUsersMailIsAttested(t *testing.T) {
	st := &heldStore{}
	st.grant("timothy@example.com", "US-TIM-7K4M3B9X")
	code, _ := mailboxOf(t, pluginWith(st), "US-ROOT-0000000", "ROOT", MailboxInbox)
	require.Equal(t, http.StatusOK, code)

	read := st.wrote(PredicateMailRead)
	require.Len(t, read, 1)
	assert.Equal(t, []string{"timothy@example.com"}, read[0].Subjects)
	assert.Equal(t, []string{"US-TIM-7K4M3B9X"}, read[0].Contexts)
	assert.Equal(t, []string{"did:key:z6MkUS-ROOT-0000000"}, read[0].Actors)
	assert.Equal(t, MailboxInbox, read[0].Attributes["mailbox"])
}

func TestAMailboxIsInboxJunkOrSent(t *testing.T) {
	st := &heldStore{}
	st.grant("timothy@example.com", "US-TIM-7K4M3B9X")
	code, _ := mailboxOf(t, pluginWith(st), "US-TIM-7K4M3B9X", "ATTESTOR", "drafts")
	assert.Equal(t, http.StatusBadRequest, code)
}
