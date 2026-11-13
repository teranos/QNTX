package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/apns"
	"github.com/teranos/QNTX/server/auth"
)

// told stands in for Apple: it keeps what the node sent, and refuses what it is told to.
type told struct {
	sent    []apns.Notification
	refuses map[string]error
}

func (a *told) Send(_ context.Context, n apns.Notification) (string, error) {
	if err := a.refuses[n.Token]; err != nil {
		return "", err
	}
	a.sent = append(a.sent, n)
	return "an-apns-id", nil
}

const (
	aPhone       = "aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11"
	anotherPhone = "bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22"
)

// handsOver is the app handing the node a phone's token, as whoever ctx admits.
func handsOver(t *testing.T, srv *QNTXServer, ctx context.Context, body string) (int, string) {
	t.Helper()
	asked := httptest.NewRequest(http.MethodPost, phonesPath, strings.NewReader(body)).WithContext(ctx)
	answered := httptest.NewRecorder()
	srv.HandlePhone(answered, asked)
	return answered.Code, answered.Body.String()
}

func inROOTsSession() context.Context {
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount
	return auth.WithAdmission(context.Background(), root)
}

func naming(phone string) string { return `{"token":"` + phone + `","platform":"ios"}` }

// The phone that is told is named by the human, in their own session.
func TestAPhoneIsNamedByTheHumanAndByNoToken(t *testing.T) {
	srv, _ := namedOverA2A(t)

	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)
	phones, err := srv.phones()
	require.NoError(t, err)
	assert.Equal(t, []string{aPhone}, phones)

	// An agent, by a key of its own, names no phone.
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	code, said = handsOver(t, srv, alder, naming(anotherPhone))
	assert.Equal(t, http.StatusForbidden, code, said)
	// Nobody at all names none.
	code, _ = handsOver(t, srv, context.Background(), naming(anotherPhone))
	assert.Equal(t, http.StatusUnauthorized, code)
	// What is no token is none: it would be a path at Apple.
	for _, body := range []string{`{"token":"../3/device/x","platform":"ios"}`, `{"token":"short","platform":"ios"}`, `{"token":"` + aPhone + `","platform":"android"}`} {
		code, said = handsOver(t, srv, inROOTsSession(), body)
		assert.Equal(t, http.StatusBadRequest, code, said)
	}
	phones, err = srv.phones()
	require.NoError(t, err)
	assert.Equal(t, []string{aPhone}, phones)

	// The human takes it away again.
	code, said = handsOver(t, srv, inROOTsSession(), `{"token":"`+aPhone+`","platform":"ios","gone":true}`)
	require.Equal(t, http.StatusOK, code, said)
	phones, err = srv.phones()
	require.NoError(t, err)
	assert.Empty(t, phones)
}

// Fifteen minutes before what is collected is delivered, the phone is told
// once: how many wait, for whom, who said what, and where a tap goes.
func TestThePhoneIsToldFifteenMinutesBeforeWhatIsCollectedGoes(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.notifies = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)

	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	hazel := asAnAgent(t, srv, "Hazel", "did:key:z6Mkhazel")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	sayTo(t, srv, hazel, "Birch", "the pond is drained")
	due := time.Now().Add(6 * time.Hour)

	// Long before: nothing.
	require.NoError(t, srv.announceDue(context.Background(), due.Add(-16*time.Minute)))
	assert.Empty(t, apple.sent, "the phone was told more than fifteen minutes before")

	// Inside the fifteen minutes: told, once, however often the node looks.
	for _, before := range []time.Duration{14 * time.Minute, 13 * time.Minute, 2 * time.Minute} {
		require.NoError(t, srv.announceDue(context.Background(), due.Add(-before)))
	}
	require.Len(t, apple.sent, 1, "the phone was not told once")
	one := apple.sent[0]
	assert.Equal(t, aPhone, one.Token)
	assert.Contains(t, one.Title, "2 wait for Birch")
	assert.Contains(t, one.Title, "14 min")
	assert.Contains(t, one.Body, "Alder: pull request one is open")
	assert.Contains(t, one.Body, "Hazel: the pond is drained")
	assert.Equal(t, "did:key:z6Mkbirch", one.Opens["collected"], "a tap does not say what waits it opens on")
	// Apple takes 64 bytes and no more, and an agent's DID alone is 56: it is named shorter, the same each time.
	assert.Equal(t, replacesFor("did:key:z6Mkbirch"), one.Replaces)
	assert.LessOrEqual(t, len(replacesFor("did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK")), 64)
	assert.NotEqual(t, replacesFor("did:key:z6Mkbirch"), replacesFor("did:key:z6Mkalder"))
}

// A phone Apple did not take it for is told when the node next looks, and one
// named after the others were told is told too: each phone once.
func TestEachPhoneIsToldOnceWhenItIsReached(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{refuses: map[string]error{anotherPhone: &apns.Refused{Status: http.StatusServiceUnavailable, Reason: "ServiceUnavailable"}}}
	srv.notifies = apple
	for _, phone := range []string{aPhone, anotherPhone} {
		code, said := handsOver(t, srv, inROOTsSession(), naming(phone))
		require.Equal(t, http.StatusOK, code, said)
	}
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	due := time.Now().Add(6 * time.Hour)

	// One is reached and one is not: said, and the one reached is told.
	require.Error(t, srv.announceDue(context.Background(), due.Add(-10*time.Minute)))
	require.Len(t, apple.sent, 1)
	assert.Equal(t, aPhone, apple.sent[0].Token)
	phones, err := srv.phones()
	require.NoError(t, err)
	assert.Len(t, phones, 2, "a phone Apple was busy for was taken for gone")

	// Apple takes it now: the one that missed it is told, and the other is not told twice.
	apple.refuses = nil
	require.NoError(t, srv.announceDue(context.Background(), due.Add(-9*time.Minute)))
	require.Len(t, apple.sent, 2)
	assert.Equal(t, anotherPhone, apple.sent[1].Token)
	require.NoError(t, srv.announceDue(context.Background(), due.Add(-8*time.Minute)))
	require.Len(t, apple.sent, 2, "a phone was told twice of the same time")
}

// What a person canceled is not shown to a phone afterwards.
func TestWhatWasCanceledIsNotToldToAPhone(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.notifies = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	due := time.Now().Add(6 * time.Hour)

	_, refused := askCollected(t, srv, asAPerson(), "cancel", map[string]string{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	require.NoError(t, srv.announceDue(context.Background(), due.Add(-10*time.Minute)))
	assert.Empty(t, apple.sent, "what a person canceled was shown to a phone")
}

// Held to a later time, it is told again before that time, with what came in meanwhile.
func TestWhatIsHeldLaterIsToldAgainBeforeItsNewTime(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.notifies = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	due := time.Now().Add(6 * time.Hour)

	require.NoError(t, srv.announceDue(context.Background(), due.Add(-10*time.Minute)))
	require.Len(t, apple.sent, 1)

	all, refused := askCollected(t, srv, asAPerson(), "later", map[string]string{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	later, err := time.Parse(time.RFC3339, all.GetCollected()[0].GetDue())
	require.NoError(t, err)
	sayTo(t, srv, alder, "Birch", "pull request two is open")

	// At the time it was first due it is no longer about to go: nothing.
	require.NoError(t, srv.announceDue(context.Background(), due.Add(-time.Minute)))
	require.Len(t, apple.sent, 1, "the phone was told of a time it was held past")
	require.NoError(t, srv.announceDue(context.Background(), later.Add(-5*time.Minute)))
	require.Len(t, apple.sent, 2, "the phone was not told again before the new time")
	assert.Contains(t, apple.sent[1].Title, "2 wait for Birch")
	assert.Contains(t, apple.sent[1].Body, "pull request two is open")
}

// A phone the app was removed from is told nothing again.
func TestAPhoneThatIsGoneIsToldNothingAgain(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{refuses: map[string]error{aPhone: &apns.Refused{Status: http.StatusGone, Reason: "Unregistered", Gone: true}}}
	srv.notifies = apple
	for _, phone := range []string{aPhone, anotherPhone} {
		code, said := handsOver(t, srv, inROOTsSession(), naming(phone))
		require.Equal(t, http.StatusOK, code, said)
	}
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	due := time.Now().Add(6 * time.Hour)

	require.NoError(t, srv.announceDue(context.Background(), due.Add(-10*time.Minute)))
	require.Len(t, apple.sent, 1)
	assert.Equal(t, anotherPhone, apple.sent[0].Token)
	phones, err := srv.phones()
	require.NoError(t, err)
	assert.Equal(t, []string{anotherPhone}, phones, "a phone Apple says is gone is still held")
}

// With no phone named, nothing is asked of Apple, and nothing fails.
func TestWithNoPhoneNothingIsToldAndNothingFails(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	// No notifier and no key: asked for, either would be an error.
	require.NoError(t, srv.announceDue(context.Background(), time.Now().Add(6*time.Hour-10*time.Minute)))
}

// An agent in a turn a person started speaks for them where what is collected
// is concerned. It names no phone all the same: neither by its own key on the
// person's word, nor as the person themselves through its door.
func TestAnAgentOnAPersonsWordNamesNoPhone(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	for _, ctx := range []context.Context{
		context.WithValue(alder, onAPersonsWord{}, true),
		context.WithValue(inROOTsSession(), onAPersonsWord{}, true),
		context.WithValue(inROOTsSession(), deliversCollected{}, true),
	} {
		code, said := handsOver(t, srv, ctx, naming(aPhone))
		assert.Equal(t, http.StatusForbidden, code, said)
	}
	phones, err := srv.phones()
	require.NoError(t, err)
	assert.Empty(t, phones, "an agent named the phone the node tells")
}
