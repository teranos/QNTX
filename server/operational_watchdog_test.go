package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

func (b *inbox) texts(t *testing.T) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var said []string
	for _, m := range b.mails {
		text, err := services.BodyOf(m.Bodies.All(), services.MailText)
		require.NoError(t, err)
		said = append(said, text)
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
	mail.Wire(services.MailWiring{From: services.MailFrom("Garden <mail@garden.test>"), Transport: services.Having[services.MailTransport](box),
		Recipients: services.Having[services.MailRecipients](s.mailRecipient), Records: s.mailRecords, Actor: services.MailActor("did:key:z6Mkgardennode")})
	s.nodeMailer = mail

	s.nodeDB.SetMaxOpenConns(1)
	s.nodeName = "garden.test"
	return s, box, tokens
}

// The whole of a minute, shrunk.
var shortPatience = operationalPatience{
	every:      20 * time.Millisecond,
	sentry:     30 * time.Millisecond,
	mailEvery:  100 * time.Millisecond,
	mailAtMost: 2 * time.Second,
	die:        500 * time.Millisecond,
	lastWords:  time.Second,
}

// stepClock is time that moves only when the test moves it, so the watch acts
// in the order the test says, however loaded the machine is.
type stepClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []stepWait
}

type stepWait struct {
	at time.Time
	c  chan time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := stepWait{at: c.now.Add(d), c: make(chan time.Time, 1)}
	c.waits = append(c.waits, w)
	return w.c
}

// step waits until the watch waits on the moment d from now, then moves time
// there and wakes everything due by then.
func (c *stepClock) step(t *testing.T, d time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool { return c.waitsOn(d) }, 10*time.Second, time.Millisecond,
		"the watch does not wait on the moment %s from now", d)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.waits[:0]
	for _, w := range c.waits {
		if w.at.After(c.now) {
			kept = append(kept, w)
			continue
		}
		w.c <- c.now
	}
	c.waits = kept
}

// handled waits until the watch has handled a mail tick: it asks for the next
// one only then. Releasing the store before that makes the answer and the
// tick ready together, and select may take the answer and drop the mail.
func (c *stepClock) handled(t *testing.T, mailEvery time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool { return c.waitsOn(mailEvery) }, 10*time.Second, time.Millisecond,
		"the watch did not handle its mail tick")
}

func (c *stepClock) waitsOn(d time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	at := c.now.Add(d)
	for _, w := range c.waits {
		if w.at.Equal(at) {
			return true
		}
	}
	return false
}

// watching starts the watch on a stepClock and waits until it has read ROOT,
// which it does while the store still answers.
func watching(t *testing.T, s *QNTXServer) (*stepClock, chan error) {
	t.Helper()
	clock := &stepClock{now: time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC)}
	var root atomic.Pointer[services.MailRecipient]
	stopped := make(chan error, 1)
	go s.watchOperationalStore(func(reason error) { stopped <- reason }, shortPatience, clock, &root)
	require.Eventually(t, func() bool { return root.Load() != nil }, 10*time.Second, time.Millisecond, "ROOT was not read")
	return clock, stopped
}

// "make it so that we can take up to a minute before it decides to die ...
// QNTX should send an email at 10 sec, 20 sec, 30 sec up to a minute. And also
// an email if it recovered back to below 3 sec and how long it took."
func TestASlowOperationalStoreIsWaitedOnAndROOTIsTold(t *testing.T) {
	s, box := watchedNode(t)
	clock, stopped := watching(t, s)

	held, err := s.nodeDB.Conn(context.Background())
	require.NoError(t, err)
	clock.step(t, shortPatience.every)
	clock.step(t, shortPatience.mailEvery)
	clock.step(t, shortPatience.mailEvery) // two mails, short of the minute
	clock.handled(t, shortPatience.mailEvery)
	require.NoError(t, held.Close())
	clock.step(t, shortPatience.every)

	// The inbox is read when the wait gives up: an argument here is read once,
	// before the first poll, while the mails are still on their way.
	if !assert.Eventually(t, func() bool {
		var waits int
		var answered bool
		for _, subject := range box.subjects() {
			// "its not indicated to me, which node its talking about"
			if strings.HasPrefix(subject, "garden.test: The operational store has not answered for") {
				waits++
			}
			if strings.HasPrefix(subject, "garden.test: The operational store answers again, after") {
				answered = true
			}
		}
		return waits == 1 && answered
	}, 10*time.Second, time.Millisecond) {
		t.Fatalf("ROOT was not mailed once about the wait and once when it ended: %v", box.subjects())
	}
	box.mu.Lock()
	for _, m := range box.mails {
		assert.Equal(t, "root@garden.test", m.To)
	}
	box.mu.Unlock()

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
	clock, stopped := watching(t, s)

	held, err := s.nodeDB.Conn(context.Background())
	require.NoError(t, err)
	defer held.Close()
	clock.step(t, shortPatience.every)
	clock.step(t, shortPatience.die)

	select {
	case reason := <-stopped:
		require.ErrorIs(t, reason, context.DeadlineExceeded)
	case <-time.After(10 * time.Second):
		t.Fatal("the store did not answer for the whole minute and the node did not stop")
	}

	subjects := box.subjects()
	require.NotEmpty(t, subjects)
	assert.True(t, strings.HasPrefix(subjects[len(subjects)-1], "garden.test: QNTX stops: the operational store has not answered for"),
		"the last mail does not say which node stops: %v", subjects)
}

// "i dont think i need to receive it more often than once per 20 min"
// A second wait inside that window is not mailed, however long it grows; the
// recovery mail answers only a wait that was mailed about.
func TestAStalledMailComesAtMostOncePerWindow(t *testing.T) {
	s, box := watchedNode(t)
	clock, stopped := watching(t, s)

	// The same steps as the wait above, twice: the second wait begins well
	// within mailAtMost of the first mail.
	stall := func() {
		// The one connection is taken while the watch is parked on its next
		// ask, not while an ask is in flight: taken then, the ask would block
		// and the watch would be waiting on Sentry, not on every.
		clock.handled(t, shortPatience.every)
		held, err := s.nodeDB.Conn(context.Background())
		require.NoError(t, err)
		clock.step(t, shortPatience.every)
		clock.step(t, shortPatience.mailEvery)
		clock.step(t, shortPatience.mailEvery)
		clock.handled(t, shortPatience.mailEvery)
		require.NoError(t, held.Close())
		clock.step(t, shortPatience.every)
	}
	stall()
	clock.step(t, shortPatience.every) // the store answers in time: the first wait is over
	stall()

	if !assert.Eventually(t, func() bool {
		var stalled, recovered int
		for _, subject := range box.subjects() {
			if strings.Contains(subject, "has not answered for") {
				stalled++
			}
			if strings.Contains(subject, "answers again, after") {
				recovered++
			}
		}
		return stalled == 1 && recovered == 1
	}, 10*time.Second, time.Millisecond) {
		t.Fatalf("two waits inside the window should mail once each way: %v", box.subjects())
	}

	select {
	case reason := <-stopped:
		t.Fatalf("a slow store stopped the node: %v", reason)
	default:
	}
}

// The recovery mail answers a mail about the wait: a wait that ended before
// ROOT was mailed about it mails nothing when it ends.
func TestAWaitROOTWasNotMailedAboutMailsNothingWhenItEnds(t *testing.T) {
	s, box := watchedNode(t)
	clock, stopped := watching(t, s)

	held, err := s.nodeDB.Conn(context.Background())
	require.NoError(t, err)
	clock.step(t, shortPatience.every)
	clock.step(t, shortPatience.sentry) // past Sentry, short of the first mail
	require.NoError(t, held.Close())
	clock.step(t, shortPatience.every) // the store answers in time again
	clock.step(t, shortPatience.every) // and the watch is past that answer

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

	clock, _ := watching(t, s)
	for range 5 {
		require.Equal(t, http.StatusOK, send(heavy))
	}
	require.Equal(t, http.StatusOK, send(light))

	held, err := s.nodeDB.Conn(context.Background())
	require.NoError(t, err)
	clock.step(t, shortPatience.every)
	clock.step(t, shortPatience.sentry)

	require.Eventually(t, func() bool {
		return send(heavy) == http.StatusTooManyRequests
	}, 10*time.Second, time.Millisecond, "the heaviest token was not turned away while the store was slow")
	assert.Equal(t, http.StatusOK, send(light), "a token that was not the heaviest was turned away")

	// Past a mail about the wait, which is what a mail about its end answers.
	clock.step(t, shortPatience.mailEvery-shortPatience.sentry)
	clock.handled(t, shortPatience.mailEvery)
	require.NoError(t, held.Close())
	clock.step(t, shortPatience.every)
	if !assert.Eventually(t, func() bool {
		for _, text := range box.texts(t) {
			if strings.Contains(text, "Let back in: ground") {
				return true
			}
		}
		return false
	}, 10*time.Second, time.Millisecond) {
		t.Fatalf("ROOT was not told the heaviest token was let back in: %v", box.subjects())
	}
	assert.Equal(t, http.StatusOK, send(heavy), "the heaviest token was still turned away once the store answered in time")
}
