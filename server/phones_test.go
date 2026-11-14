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

// "ANY PENDING IS LIVE ACTIVITY"
// What is collected stands on the phone as a Live Activity for as long as it
// is pending, started once: what it is of, how many wait, who said what, when
// it goes, and ROOT's three answers as the node answers them.
func TestWhatIsPendingStandsOnThePhone(t *testing.T) {
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

	// From when it is pending, hours before it goes: started, once, however often the node looks.
	for _, before := range []time.Duration{5*time.Hour + 59*time.Minute, 3 * time.Hour, 14 * time.Minute, 2 * time.Minute} {
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
	assert.Equal(t, one.Stale, one.Expires, "a phone not reached before it goes would be shown it afterwards")
	// By the agent's DID, which stays its own whoever holds its name later.
	birch := map[string]string{"to": "did:key:z6Mkbirch"}
	assert.Equal(t, []waitsPress{
		{Says: "send now", Does: "approve", Then: "sent", Path: "/api/collected/release", Sent: birch},
		{Says: "delay", Does: "delay", Then: "delayed", Path: "/api/collected/later", Sent: birch},
		{Says: "cancel", Does: "cancel", Then: "canceled", Path: "/api/collected/cancel", Sent: birch, Armed: true},
	}, shown.Presses)
	assert.NotNil(t, shown.Segments, "the phone decodes no list from nothing")
}

// "SO THE 6H ONE STAYS FOR 6H"
// "SO WE NEVER BREAK APPLES RULE YOU KNOW"
// One stands for its wait and no longer: what a person holds for longer
// stands for the wait before it goes, and not before.
func TestWhatIsHeldLongerStandsForTheWaitBeforeItGoes(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.shows = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	began := time.Now()
	goes := began.Add(20 * time.Hour)
	_, refused := askCollected(t, srv, asAPerson(), "hold", map[string]string{"to": "Birch", "until": goes.Format(time.RFC3339)})
	require.Nil(t, refused, refused.GetSays())

	require.NoError(t, srv.showWhatWaits(context.Background(), began))
	require.NoError(t, srv.showWhatWaits(context.Background(), goes.Add(-6*time.Hour-time.Minute)))
	assert.Empty(t, apple.sent, "it stood longer before it goes than its wait")
	require.NoError(t, srv.showWhatWaits(context.Background(), goes.Add(-6*time.Hour+time.Minute)))
	require.Len(t, apple.sent, 1, "it did not stand for the wait before it goes")
	assert.Equal(t, apns.ActivityStarts, apple.sent[0].Event)
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

// "WHEN DELAY CANT WE MAKE IT BE DISMISSED UNTIL ITS TIME TO SHOW AGAIN /"
// One that stands is moved when what waits changes, by its own token. Delayed,
// it is dismissed at once, and stands again for the wait before its new time,
// with what came in meanwhile.
func TestOneThatStandsIsMovedAndADelayDismissesIt(t *testing.T) {
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

	// ROOT delays it: it is dismissed at once, saying so, and nothing is said to it after.
	all, refused := askCollected(t, srv, asAPerson(), "later", map[string]string{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	later, err := time.Parse(time.RFC3339, all.GetCollected()[0].GetDue())
	require.NoError(t, err)
	at := due.Add(-7 * time.Minute)
	require.NoError(t, srv.showWhatWaits(context.Background(), at))
	require.Len(t, apple.sent, 3)
	dismissed := apple.sent[2]
	assert.Equal(t, anActivity, dismissed.Token)
	assert.Equal(t, apns.ActivityEnds, dismissed.Event)
	assert.Equal(t, waitsShown{Says: "delayed", Segments: []waitsSegment{}, Presses: []waitsPress{}}, dismissed.State)
	assert.Equal(t, at.Unix(), dismissed.Gone.Unix(), "what was delayed was left standing")
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-6*time.Minute)))
	require.Len(t, apple.sent, 3, "something was said to one that was dismissed, or it was started before its time")

	// The wait before its new time: it stands again, with what came in meanwhile.
	require.NoError(t, srv.showWhatWaits(context.Background(), later.Add(-5*time.Hour)))
	require.Len(t, apple.sent, 4, "what was delayed did not stand again before its new time")
	again := apple.sent[3]
	assert.Equal(t, aPhone, again.Token)
	assert.Equal(t, apns.ActivityStarts, again.Event)
	assert.Equal(t, "2 wait", again.State.(waitsShown).Says)
	assert.InDelta(t, float64(later.Unix()), again.State.(waitsShown).Due, 5)
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

// A token of another kind, held from before the node started Live Activities,
// starts none: Apple would refuse it each time the node looked.
func TestATokenOfAnotherKindStartsNothing(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.shows = apple
	require.NoError(t, srv.nodeRecords().nodeRecord(rootAccount, "PHONES", aPhone, "_", map[string]any{"platform": "ios", "gone": false}))
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")

	require.NoError(t, srv.showWhatWaits(context.Background(), time.Now().Add(6*time.Hour-10*time.Minute)))
	assert.Empty(t, apple.sent, "a Live Activity was started by a token that starts none")
}

// What is due and not delivered yet is left as it stands: it has not been
// delayed, and it ends when it is sent.
func TestWhatIsDueAndNotDeliveredYetIsLeftAsItStands(t *testing.T) {
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
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-9*time.Minute)))
	stood := len(apple.sent)

	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(30*time.Second)))
	assert.Len(t, apple.sent, stood, "what is due and not delivered yet was ended, or said to be delayed")
}

// A press names the batch by its agent's DID, and the node answers it by that.
func TestAPressNamesTheBatchByItsAgentsDID(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")

	all, refused := askCollected(t, srv, asAPerson(), "cancel", map[string]string{"to": "did:key:z6Mkbirch"})
	require.Nil(t, refused, refused.GetSays())
	for _, one := range all.GetCollected() {
		assert.Empty(t, one.GetWaiting(), "what was canceled by its agent's DID still waits")
	}
}

// "SO WE NEVER BREAK APPLES RULE YOU KNOW"
// One stands for one time. Held to another while it stands, and standing
// still by the new one, it is dismissed and another is started for that
// time: none stands longer than its wait.
func TestOneThatIsHeldToAnotherTimeIsStartedAnewForIt(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.shows = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	due := time.Now().Add(6 * time.Hour)
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-time.Hour)))
	require.Len(t, apple.sent, 1)
	code, said = handsOver(t, srv, inROOTsSession(), startedOf(anActivity))
	require.Equal(t, http.StatusOK, code, said)

	// Held an hour longer: by the new time it stands still, for that is within its wait.
	goes := due.Add(time.Hour)
	_, refused := askCollected(t, srv, asAPerson(), "hold", map[string]string{"to": "Birch", "until": goes.Format(time.RFC3339)})
	require.Nil(t, refused, refused.GetSays())
	at := due.Add(-50 * time.Minute)
	require.NoError(t, srv.showWhatWaits(context.Background(), at))
	last := apple.sent[len(apple.sent)-1]
	assert.Equal(t, anActivity, last.Token)
	assert.Equal(t, apns.ActivityEnds, last.Event, "the one that stood for the old time was left standing for the new")
	assert.Equal(t, at.Unix(), last.Gone.Unix())

	// The node looks again: another is started, for the new time.
	require.NoError(t, srv.showWhatWaits(context.Background(), at.Add(time.Minute)))
	again := apple.sent[len(apple.sent)-1]
	assert.Equal(t, aPhone, again.Token)
	assert.Equal(t, apns.ActivityStarts, again.Event)
	assert.InDelta(t, float64(goes.Unix()), again.State.(waitsShown).Due, 5)
}

// A phone named while a collection is pending has it started there, also when
// one stands elsewhere whose phone the app did not name.
func TestAPhoneNamedLaterHasWhatIsPendingStartedOnIt(t *testing.T) {
	srv, _ := namedOverA2A(t)
	apple := &told{}
	srv.shows = apple
	code, said := handsOver(t, srv, inROOTsSession(), naming(aPhone))
	require.Equal(t, http.StatusOK, code, said)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	due := time.Now().Add(6 * time.Hour)
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-time.Hour)))
	require.Len(t, apple.sent, 1)
	code, said = handsOver(t, srv, inROOTsSession(), startedOf(anActivity))
	require.Equal(t, http.StatusOK, code, said)

	code, said = handsOver(t, srv, inROOTsSession(), naming(anotherPhone))
	require.Equal(t, http.StatusOK, code, said)
	require.NoError(t, srv.showWhatWaits(context.Background(), due.Add(-50*time.Minute)))
	starts := []string{}
	for _, one := range apple.sent {
		if one.Event == apns.ActivityStarts {
			starts = append(starts, one.Token)
		}
	}
	assert.Equal(t, []string{aPhone, anotherPhone}, starts, "a phone named later was kept from what waits, or the first was started on again")
}
