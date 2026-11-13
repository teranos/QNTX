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

// told stands in for Apple: it keeps what the node said to Live Activities, and refuses what it is told to.
type told struct {
	sent    []apns.Activity
	refuses map[string]error
}

func (a *told) Show(_ context.Context, said apns.Activity) (string, error) {
	if err := a.refuses[said.Token]; err != nil {
		return "", err
	}
	a.sent = append(a.sent, said)
	return "an-apns-id", nil
}

const (
	aPhone       = "aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11"
	anotherPhone = "bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22"
	// What one started Live Activity is moved and ended by, as the app hands it over.
	anActivity = "cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33"
)

// handsOver is the app handing the node a token, as whoever ctx admits.
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

func naming(phone string) string {
	return "{\"token\":\"" + phone + "\",\"platform\":\"ios\"}"
}

// startedOf is the app handing over the own token of a Live Activity that started, of what waits for Birch.
func startedOf(activity string) string {
	return "{\"token\":\"" + activity + "\",\"platform\":\"ios\",\"waits\":\"collected/did:key:z6Mkbirch\"}"
}

// The phone what waits stands on is named by the human, in their own session.
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
	for _, body := range []string{
		"{\"token\":\"../3/device/x\",\"platform\":\"ios\"}",
		"{\"token\":\"short\",\"platform\":\"ios\"}",
		"{\"token\":\"" + aPhone + "\",\"platform\":\"android\"}",
	} {
		code, said = handsOver(t, srv, inROOTsSession(), body)
		assert.Equal(t, http.StatusBadRequest, code, said)
	}
	phones, err = srv.phones()
	require.NoError(t, err)
	assert.Equal(t, []string{aPhone}, phones)

	// The human takes it away again.
	code, said = handsOver(t, srv, inROOTsSession(), "{\"token\":\""+aPhone+"\",\"platform\":\"ios\",\"gone\":true}")
	require.Equal(t, http.StatusOK, code, said)
	phones, err = srv.phones()
	require.NoError(t, err)
	assert.Empty(t, phones)
}

// Fifteen minutes before what is collected is delivered it stands on the phone
// as a Live Activity, started once: what it is of, how many wait, who said
// what, when it goes, and ROOT's three answers as the node answers them.
func TestWhatWaitsStandsOnThePhoneFifteenMinutesBeforeItGoes(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.shows = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)

	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	hazel := asAnAgent(t, srv, "Hazel", "did:key:z6Mkhazel")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	sayTo(t, srv, hazel, "Birch", "the pond is drained")
	due := time.Now().Add(6 * time.Hour)

	// Long before: nothing.
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-16*time.Minute)))
	assert.Empty(t, apple.sent, "it stood on the phone more than fifteen minutes before")

	// Inside the fifteen minutes: started, once, however often the node looks.
	for _, before := range []time.Duration{14 * time.Minute, 13 * time.Minute, 2 * time.Minute} {
		require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-before)))
	}
	require.Len(t, apple.sent, 1, "it was not started once")
	one := apple.sent[0]
	assert.Equal(t, aPhone, one.Token)
	assert.Equal(t, apns.ActivityStarts, one.Event)
	assert.Equal(t, waitsType, one.Type)
	assert.Equal(t, waitsOf{Waits: "collected/did:key:z6Mkbirch", Kind: "collected", Title: "for Birch"}, one.Attributes)
	assert.Equal(t, "2 wait for Birch", one.Title)
	shown, isOne := one.State.(waitsShown)
	require.True(t, isOne, "what it shows is not the one shape: %T", one.State)
	assert.Equal(t, "2 wait", shown.Says)
	assert.Contains(t, shown.Preview, "Alder: pull request one is open")
	assert.Contains(t, shown.Preview, "Hazel: the pond is drained")
	assert.InDelta(t, float64(due.Unix()), shown.Due, 5, "it does not count down to when it goes")
	assert.Equal(t, int64(shown.Due), one.Stale.Unix())
	birch := map[string]string{"to": "Birch"}
	assert.Equal(t, []waitsPress{
		{Says: "send now", Does: "send", Then: "sent", Path: "/api/collected/release", Sent: birch},
		{Says: "delay", Does: "delay", Then: "delayed", Path: "/api/collected/later", Sent: birch},
		{Says: "cancel", Does: "cancel", Then: "canceled", Path: "/api/collected/cancel", Sent: birch, Armed: true},
	}, shown.Presses)
	assert.NotNil(t, shown.Segments, "the phone decodes no list from nothing")
}

// A phone Apple did not take it for has it started when the node next looks,
// and one named after the others has it too: each phone once.
func TestItIsStartedOnEachPhoneOnceWhenItIsReached(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{refuses: map[string]error{anotherPhone: &apns.Refused{Status: http.StatusServiceUnavailable, Reason: "ServiceUnavailable"}}}
	srv.shows = apple
	for _, phone := range []string{aPhone, anotherPhone} {
		code, said := handsOver(t, srv, inROOTsSession(), naming(phone))
		require.Equal(t, http.StatusOK, code, said)
	}
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	due := time.Now().Add(6 * time.Hour)

	// One is reached and one is not: said, and it stands on the one reached.
	require.Error(t, srv.showWhatWaits(context.Background(), due.Add(-10*time.Minute)))
	require.Len(t, apple.sent, 1)
	assert.Equal(t, aPhone, apple.sent[0].Token)
	phones, err := srv.phones()
	require.NoError(t, err)
	assert.Len(t, phones, 2, "a phone Apple was busy for was taken for gone")

	// Apple takes it now: the one that missed it has it, and the other does not have it twice.
	apple.refuses = nil
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-9*time.Minute)))
	require.Len(t, apple.sent, 2)
	assert.Equal(t, anotherPhone, apple.sent[1].Token)
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-8*time.Minute)))
	require.Len(t, apple.sent, 2, "it was started twice on a phone for the same time")
}

// What a person canceled is not started on a phone afterwards.
func TestWhatWasCanceledIsNotStartedOnAPhone(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.shows = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	due := time.Now().Add(6 * time.Hour)

	_, refused := askCollected(t, srv, asAPerson(), "cancel", map[string]string{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-10*time.Minute)))
	assert.Empty(t, apple.sent, "what a person canceled stood on a phone")
}

// One that stands is moved when what waits changes, by its own token, and
// ended saying what became of it when ROOT delays it: nothing is said to it after.
func TestOneThatStandsIsMovedAndEndedByItsOwnToken(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.shows = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	due := time.Now().Add(6 * time.Hour)

	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-10*time.Minute)))
	require.Len(t, apple.sent, 1)
	// The app hands over the started one's own token, in ROOT's session and in no other.
	code, said = handsOver(t, srv, alder, startedOf(anActivity))
	assert.Equal(t, http.StatusForbidden, code, said)
	code, said = handsOver(t, srv, inROOTsSession(), startedOf(anActivity))
	require.Equal(t, http.StatusOK, code, said)
	phones, err := srv.phones()
	require.NoError(t, err)
	assert.Equal(t, []string{aPhone}, phones, "a Live Activity's own token was taken for one that starts them")

	// Something more is said: it is moved to show both, and once.
	sayTo(t, srv, alder, "Birch", "pull request two is open")
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-9*time.Minute)))
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-8*time.Minute)))
	require.Len(t, apple.sent, 2, "it was not moved once")
	moved := apple.sent[1]
	assert.Equal(t, anActivity, moved.Token)
	assert.Equal(t, apns.ActivityMoves, moved.Event)
	assert.Empty(t, moved.Type, "what it is made of is said when it starts and only then")
	assert.Equal(t, "2 wait", moved.State.(waitsShown).Says)
	assert.Contains(t, moved.State.(waitsShown).Preview, "pull request two is open")

	// ROOT delays it: it ends saying so, with nothing to press, and leaves the lock screen.
	_, refused := askCollected(t, srv, asAPerson(), "later", map[string]string{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	at := due.Add(-7 * time.Minute)
	require.NoError(t, srv.showWhatWaits(context.Background(), at))
	require.Len(t, apple.sent, 3)
	last := apple.sent[2]
	assert.Equal(t, anActivity, last.Token)
	assert.Equal(t, apns.ActivityEnds, last.Event)
	assert.Equal(t, waitsShown{Says: "delayed", Segments: []waitsSegment{}, Presses: []waitsPress{}}, last.State)
	assert.Equal(t, at.Add(leavesAfter).Unix(), last.Gone.Unix())
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-6*time.Minute)))
	require.Len(t, apple.sent, 3, "something was said to one that is over")
}

// One ROOT canceled ends saying so.
func TestOneThatWasCanceledEndsSayingSo(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.shows = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	due := time.Now().Add(6 * time.Hour)
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-10*time.Minute)))
	code, said = handsOver(t, srv, inROOTsSession(), startedOf(anActivity))
	require.Equal(t, http.StatusOK, code, said)

	_, refused := askCollected(t, srv, asAPerson(), "cancel", map[string]string{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-9*time.Minute)))
	last := apple.sent[len(apple.sent)-1]
	assert.Equal(t, apns.ActivityEnds, last.Event)
	assert.Equal(t, "canceled", last.State.(waitsShown).Says)
}

// Held to a later time, it is started again before that time, with what came in meanwhile.
func TestWhatIsHeldLaterIsStartedAgainBeforeItsNewTime(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.shows = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	due := time.Now().Add(6 * time.Hour)

	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-10*time.Minute)))
	require.Len(t, apple.sent, 1)

	all, refused := askCollected(t, srv, asAPerson(), "later", map[string]string{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	later, err := time.Parse(time.RFC3339, all.GetCollected()[0].GetDue())
	require.NoError(t, err)
	sayTo(t, srv, alder, "Birch", "pull request two is open")

	// At the time it was first due it is no longer about to go: nothing.
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-time.Minute)))
	require.Len(t, apple.sent, 1, "it was started for a time it was held past")
	require.NoError(t, srv.showWhatWaits(context.Background(), later.Add(-5*time.Minute)))
	require.Len(t, apple.sent, 2, "it was not started again before the new time")
	assert.Equal(t, apns.ActivityStarts, apple.sent[1].Event)
	assert.Equal(t, "2 wait", apple.sent[1].State.(waitsShown).Says)
	assert.Contains(t, apple.sent[1].State.(waitsShown).Preview, "pull request two is open")
}

// A phone the app was removed from has nothing started on it again.
func TestAPhoneThatIsGoneHasNothingStartedOnItAgain(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{refuses: map[string]error{aPhone: &apns.Refused{Status: http.StatusGone, Reason: "Unregistered", Gone: true}}}
	srv.shows = apple
	for _, phone := range []string{aPhone, anotherPhone} {
		code, said := handsOver(t, srv, inROOTsSession(), naming(phone))
		require.Equal(t, http.StatusOK, code, said)
	}
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	due := time.Now().Add(6 * time.Hour)

	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-10*time.Minute)))
	require.Len(t, apple.sent, 1)
	assert.Equal(t, anotherPhone, apple.sent[0].Token)
	phones, err := srv.phones()
	require.NoError(t, err)
	assert.Equal(t, []string{anotherPhone}, phones, "a phone Apple says is gone is still held")
}

// With no phone named, nothing is asked of Apple, and nothing fails.
func TestWithNoPhoneNothingIsStartedAndNothingFails(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	// No stand-in and no key: asked for, either would be an error.
	require.NoError(t, srv.showWhatWaits(context.Background(), time.Now().Add(6*time.Hour-10*time.Minute)))
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
	assert.Empty(t, phones, "an agent named the phone what waits stands on")
}
