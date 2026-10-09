package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
)

// Every subsystem handles a store error locally and continues, so losing the
// store is a decision nothing else makes.
func TestAnUnreadableOperationalStoreStopsTheProcess(t *testing.T) {
	store, db := createTestStore(t)

	srv, err := NewQNTXServer(db, servingOne(db, store), ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	stopped := make(chan error, 1)
	go srv.WatchOperationalStore(func(reason error) { stopped <- reason })

	db.Close()

	select {
	case reason := <-stopped:
		if reason == nil {
			t.Fatal("stopped with no reason")
		}
	case <-time.After(3 * operationalCheckInterval):
		t.Fatal("the store was unreadable and the process was not stopped")
	}
}

// A readable store is the ordinary case, and a watchdog that fires on it would
// restart the node every few seconds forever.
func TestAReadableOperationalStoreStopsNothing(t *testing.T) {
	store, db := createTestStore(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, servingOne(db, store), ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	stopped := make(chan error, 1)
	go srv.WatchOperationalStore(func(reason error) { stopped <- reason })

	select {
	case reason := <-stopped:
		t.Fatalf("stopped a healthy node: %v", reason)
	case <-time.After(2 * operationalCheckInterval):
	}
}

// inbox is a transport that keeps what it is handed, from any goroutine.
type inbox struct {
	mu    sync.Mutex
	mails []services.OutgoingMail
}

func (b *inbox) Send(_ context.Context, m services.OutgoingMail) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mails = append(b.mails, m)
	return "ses-" + m.Subject, nil
}

func (b *inbox) texts() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var said []string
	for _, m := range b.mails {
		said = append(said, m.Text)
	}
	return said
}

func (b *inbox) subjects() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var said []string
	for _, m := range b.mails {
		said = append(said, m.Subject)
	}
	return said
}

// watchedNode is a node with a ROOT User to mail and a mail service to mail
// them with, whose operational store answers one caller at a time, so holding
// that one connection is a store that does not answer.
func watchedNode(t *testing.T) (*QNTXServer, *inbox) {
	s, box, _ := watchedNodeWithTokens(t)
	return s, box
}

// watchedNodeWithTokens is a watchedNode whose gate admits access tokens and
// turns away the heaviest while the store is slow.
func watchedNodeWithTokens(t *testing.T) (*QNTXServer, *inbox, *auth.TokenTable) {
	t.Helper()
	s := rootKnowingServer(t)
	s.ctx, s.cancel = context.WithCancel(context.Background())
	t.Cleanup(s.cancel)

	users, _, err := auth.OpenUserTable(s.nodeDB, nil)
	require.NoError(t, err)
	require.NoError(t, users.Put(auth.User{ID: "USroot", Level: auth.LevelRoot, EmailAddresses: []string{"root@garden.test"}}))
	tokens, _, err := auth.OpenTokenTable(s.nodeDB, nil)
	require.NoError(t, err)
	s.authHandler, err = auth.New(nil, "localhost", nil, 8770, 8820, 24, zap.NewNop().Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		tokens, users, false, []string{rootAccount}, nil)
	require.NoError(t, err)
	s.shed = auth.NewShed(operationalCheckInterval)
	s.authHandler.SetShed(s.shed)

	box := &inbox{}
	mail := services.NewMailServer("token", zap.NewNop().Sugar())
	mail.Wire(services.MailWiring{From: "Garden <mail@garden.test>", Transport: box,
		Recipients: s.mailRecipient, Records: s.mailRecords, Actor: "did:key:z6Mkgardennode"})
	s.nodeMailer = mail

	s.nodeDB.SetMaxOpenConns(1)
	return s, box, tokens
}

// The whole of a minute, shrunk so a test can wait it out.
var shortPatience = operationalPatience{
	every:     20 * time.Millisecond,
	sentry:    30 * time.Millisecond,
	mailEvery: 100 * time.Millisecond,
	die:       500 * time.Millisecond,
	lastWords: time.Second,
}

// "make it so that we can take up to a minute before it decides to die ...
// QNTX should send an email at 10 sec, 20 sec, 30 sec up to a minute. And also
// an email if it recovered back to below 3 sec and how long it took."
func TestASlowOperationalStoreIsWaitedOnAndROOTIsTold(t *testing.T) {
	s, box := watchedNode(t)

	stopped := make(chan error, 1)
	go s.watchOperationalStore(func(reason error) { stopped <- reason }, shortPatience)
	time.Sleep(3 * shortPatience.every) // ROOT is read while the store answers

	held, err := s.nodeDB.Conn(context.Background())
	require.NoError(t, err)
	time.Sleep(250 * time.Millisecond) // past two mails, short of the minute
	require.NoError(t, held.Close())

	require.Eventually(t, func() bool {
		for _, subject := range box.subjects() {
			if strings.HasPrefix(subject, "The operational store answers again, after") {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond, "the store answered again and ROOT was not told: %v", box.subjects())

	var waits int
	for _, subject := range box.subjects() {
		if strings.HasPrefix(subject, "The operational store has not answered for") {
			waits++
		}
	}
	assert.GreaterOrEqual(t, waits, 2, "ROOT was not mailed as the wait grew: %v", box.subjects())
	for _, m := range box.mails {
		assert.Equal(t, "root@garden.test", m.To)
	}

	select {
	case reason := <-stopped:
		t.Fatalf("a store that answered within the minute stopped the node: %v", reason)
	default:
	}
}

// A store that has not answered in a minute is given up on, and ROOT is told
// before the process ends.
func TestAStoreThatDoesNotAnswerInAMinuteStopsTheNodeAndROOTIsTold(t *testing.T) {
	s, box := watchedNode(t)

	stopped := make(chan error, 1)
	go s.watchOperationalStore(func(reason error) { stopped <- reason }, shortPatience)
	time.Sleep(3 * shortPatience.every)

	held, err := s.nodeDB.Conn(context.Background())
	require.NoError(t, err)
	defer held.Close()

	select {
	case reason := <-stopped:
		require.ErrorIs(t, reason, context.DeadlineExceeded)
	case <-time.After(5 * shortPatience.die):
		t.Fatal("the store did not answer for the whole minute and the node did not stop")
	}

	subjects := box.subjects()
	require.NotEmpty(t, subjects)
	assert.True(t, strings.HasPrefix(subjects[len(subjects)-1], "QNTX stops: the operational store has not answered for"),
		"the last mail does not say the node stops: %v", subjects)
}

// The recovery mail answers a mail about the wait: a wait that ended before
// ROOT was mailed about it mails nothing when it ends.
func TestAWaitROOTWasNotMailedAboutMailsNothingWhenItEnds(t *testing.T) {
	s, box := watchedNode(t)

	stopped := make(chan error, 1)
	go s.watchOperationalStore(func(reason error) { stopped <- reason }, shortPatience)
	time.Sleep(3 * shortPatience.every)

	held, err := s.nodeDB.Conn(context.Background())
	require.NoError(t, err)
	time.Sleep(2 * shortPatience.sentry) // past Sentry, short of the first mail
	require.NoError(t, held.Close())

	time.Sleep(10 * shortPatience.every) // the store answers in time again
	assert.Empty(t, box.subjects())

	select {
	case reason := <-stopped:
		t.Fatalf("a short wait stopped the node: %v", reason)
	default:
	}
}

// "a stall under load should shed or throttle the heaviest caller, not end
// the process." — "Yes"
//
// While the store is slow the token that sent the most is turned away, and it
// is let back in, and ROOT told so, once the store answers in time again.
func TestTheHeaviestTokenIsTurnedAwayWhileTheStoreIsSlow(t *testing.T) {
	s, box, tokens := watchedNodeWithTokens(t)
	heavy, _, err := tokens.Create(auth.NewToken{Label: "ground", MintedBy: rootAccount, Level: auth.LevelAttestor, ExpiresAt: auth.NeverEnds()})
	require.NoError(t, err)
	light, _, err := tokens.Create(auth.NewToken{Label: "laptop-cron", MintedBy: rootAccount, Level: auth.LevelAttestor, ExpiresAt: auth.NeverEnds()})
	require.NoError(t, err)

	gated := s.authHandler.Middleware("/api/attestations", auth.Also(auth.LevelAttestor), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	send := func(raw string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/attestations", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, req)
		return rec.Code
	}

	stopped := make(chan error, 1)
	go s.watchOperationalStore(func(reason error) { stopped <- reason }, shortPatience)
	time.Sleep(3 * shortPatience.every)

	held, err := s.nodeDB.Conn(context.Background())
	require.NoError(t, err)
	// Sent while the store is still answering in time, so it is counted and
	// not lifted before the stall begins.
	for range 5 {
		send(heavy)
	}
	send(light)

	require.Eventually(t, func() bool {
		return send(heavy) == http.StatusTooManyRequests
	}, 2*shortPatience.mailEvery, 5*time.Millisecond, "the heaviest token was not turned away while the store was slow")
	assert.Equal(t, http.StatusOK, send(light), "a token that was not the heaviest was turned away")

	// Past a mail about the wait, which is what a mail about its end answers.
	time.Sleep(2 * shortPatience.mailEvery)
	require.NoError(t, held.Close())
	require.Eventually(t, func() bool {
		for _, text := range box.texts() {
			if strings.Contains(text, "Let back in: ground") {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond, "ROOT was not told the heaviest token was let back in: %v", box.subjects())
	assert.Equal(t, http.StatusOK, send(heavy), "the heaviest token was still turned away once the store answered in time")
}
