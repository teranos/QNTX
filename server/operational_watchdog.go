package server

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"strings"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/services"
	errors "github.com/teranos/sacred-error"
)

// How often the node asks whether it can still read what it cannot run without.
const operationalCheckInterval = 5 * time.Second

// operationalPatience is how long the node waits on the operational store, and
// what it says while it waits.
//
// "make it so that we can take up to a minute before it decides to die,
// But 3+ sec is sentry, and QNTX should send an email at 10 sec, 20 sec, 30 sec
// up to a minute. And also an email if it recovered back to below 3 sec and how
// long it took."
type operationalPatience struct {
	every     time.Duration // How often the store is asked.
	sentry    time.Duration // A wait this long is an issue in Sentry.
	mailEvery time.Duration // ROOT is mailed each time the wait grows by this.
	die       time.Duration // A wait this long ends the process.
	lastWords time.Duration // How long the mail saying so may take before it does.
}

var operationalPatienceDefault = operationalPatience{
	every:     operationalCheckInterval,
	sentry:    3 * time.Second,
	mailEvery: 10 * time.Second,
	die:       time.Minute,
	lastWords: 10 * time.Second,
}

// WatchOperationalStore ends the process when the operational store stops being
// readable. Passkeys, jobs, schedules and the canvas live there, so a node that
// cannot read it is a port that answers rather than a node.
//
// A store that answers slowly is still one the node runs on, and a restart
// serves nothing while it boots, so a slow store is waited on for up to a
// minute before it is given up on.
func (s *QNTXServer) WatchOperationalStore(stop func(reason error)) {
	var root atomic.Pointer[services.MailRecipient]
	s.watchOperationalStore(stop, operationalPatienceDefault, wallClock{}, &root)
}

// watchClock is the time the watch reads and waits on.
type watchClock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// wallClock is the time the node runs on.
type wallClock struct{}

func (wallClock) Now() time.Time                         { return time.Now() }
func (wallClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// watchOperationalStore keeps the ROOT User it mails in root, once read.
func (s *QNTXServer) watchOperationalStore(stop func(reason error), p operationalPatience, clock watchClock, root *atomic.Pointer[services.MailRecipient]) {
	// ROOT is read while the store answers: the Users are in the store that is
	// being waited on, so reading them when it does not answer waits too.
	sacred.Go("operational.root", func() { s.rememberRoot(root, p.every, clock) })

	var stalled time.Time // When the store stopped answering within p.sentry; zero while it does.
	var longest time.Duration
	var told bool // ROOT was mailed that the store is not answering, so is mailed when it does.
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-clock.After(p.every):
		}

		asked := clock.Now()
		took, mailed, err := s.askOperationalStore(p, clock, asked, root.Load)
		told = told || mailed
		if s.ctx.Err() != nil {
			return
		}

		if err != nil {
			s.logger.Errorw("the operational store is unreadable; QNTX cannot function",
				"error", err,
				"waited", took,
				"holds", "passkeys, jobs, schedules, canvas",
			)
			if errors.Is(err, context.DeadlineExceeded) {
				s.lastWords(p, root.Load(), stalledMail(took, p, s.operationalPool(), s.turnedAway(), true))
			}
			stop(err)
			return
		}

		if took >= p.sentry {
			if stalled.IsZero() {
				stalled = asked
			}
			longest = max(longest, took)
			continue
		}
		var lifted []string
		if s.shed != nil {
			lifted = s.shed.Lift()
		}
		if !stalled.IsZero() {
			answered := asked.Add(took)
			s.logger.Infow("The operational store answers within the time again",
				"within", p.sentry, "since", stalled, "took", answered.Sub(stalled), "longest_wait", longest,
				"let_back_in", lifted)
			if told {
				s.mailRoot(root.Load(), recoveredMail(stalled, answered, longest, lifted, p))
			}
			stalled, longest, told = time.Time{}, 0, false
		}
	}
}

// askOperationalStore pings the operational store and waits up to p.die for
// the answer, saying so at p.sentry and mailing ROOT every p.mailEvery. It says
// whether ROOT was mailed.
func (s *QNTXServer) askOperationalStore(p operationalPatience, clock watchClock, asked time.Time, root func() *services.MailRecipient) (time.Duration, bool, error) {
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	answered := make(chan error, 1)
	sacred.Go("operational.ping", func() {
		var err error
		defer func() { answered <- err }()
		defer sacred.Recovered("operational.ping", &err)
		err = s.nodeDB.PingContext(ctx)
	})

	die := clock.After(p.die)
	sentry := clock.After(p.sentry)
	mail := clock.After(p.mailEvery)
	mailed := false
	for {
		select {
		case err := <-answered:
			return clock.Now().Sub(asked), mailed, err
		case <-die:
			return clock.Now().Sub(asked), mailed, errors.Wrapf(context.DeadlineExceeded, "the operational store did not answer in %s", p.die)
		case <-sentry:
			s.turnAwayHeaviest()
			s.logger.Errorw("The operational store has not answered for "+p.sentry.String(),
				"asked_at", asked, "pool", s.operationalPool().String(), "dies_at", p.die,
				"turned_away", s.turnedAway(), "in_the_pool", inThePool())
		case <-mail:
			mail = clock.After(p.mailEvery)
			waited := clock.Now().Sub(asked)
			if waited >= p.die {
				continue
			}
			s.turnAwayHeaviest()
			s.mailRoot(root(), stalledMail(waited, p, s.operationalPool(), s.turnedAway(), false))
			mailed = true
		}
	}
}

// turnAwayHeaviest has the gate turn away the token that sent the most since
// the store last answered in time, and says who.
func (s *QNTXServer) turnAwayHeaviest() {
	if s.shed == nil {
		return
	}
	label, sent, found := s.shed.Heaviest()
	if !found {
		return
	}
	s.logger.Warnw("The operational store is slow; the token that sends the most is turned away until it is not",
		"token", label, "requests", sent)
}

// turnedAway is who the gate is turning away, by label.
func (s *QNTXServer) turnedAway() []string {
	if s.shed == nil {
		return nil
	}
	return s.shed.Turned()
}

// operationalPool is where the connections to the operational store are, and
// who holds its write lock. Neither asks the store anything.
type operationalPool struct {
	stats  sql.DBStats
	holder string
	held   time.Duration
}

func (s *QNTXServer) operationalPool() operationalPool {
	pool := operationalPool{stats: s.nodeDB.Stats()}
	if s.writeLockInspector != nil {
		pool.holder, pool.held = s.writeLockInspector.WriteHolderInfo()
	}
	return pool
}

func (p operationalPool) String() string {
	said := fmt.Sprintf("%d of %d connections in use, %d idle; %d waits for one, %s waited in all",
		p.stats.InUse, p.stats.MaxOpenConnections, p.stats.Idle, p.stats.WaitCount, p.stats.WaitDuration.Round(time.Millisecond))
	if p.holder != "" {
		said += fmt.Sprintf("; the write lock held by %s for %s", p.holder, p.held.Round(time.Millisecond))
	}
	return said
}

// rememberRoot reads the ROOT User every tick until there is one to mail.
func (s *QNTXServer) rememberRoot(root *atomic.Pointer[services.MailRecipient], every time.Duration, clock watchClock) {
	for {
		if s.authHandler != nil {
			u, found, err := s.authHandler.RootUser()
			if err == nil && found {
				root.Store(&services.MailRecipient{ID: u.ID, Email: u.PrimaryEmail(), DisabledBy: u.DisabledBy})
				return
			}
		}
		select {
		case <-s.ctx.Done():
			return
		case <-clock.After(every):
		}
	}
}

// mailRoot mails ROOT off the watch, so a slow transport does not hold the
// watch up; a mail that cannot go says why.
func (s *QNTXServer) mailRoot(root *services.MailRecipient, m services.NodeMail) {
	if s.nodeMailer == nil || root == nil {
		s.logger.Errorw("The operational store is not answering in time and ROOT is not mailed",
			"mail", m.Subject, "has_mail", s.nodeMailer != nil, "knows_root", root != nil)
		return
	}
	to := *root
	sacred.Go("operational.mail", func() {
		if _, _, err := s.nodeMailer.SendAsNodeTo(s.ctx, to, m); err != nil {
			s.logger.Errorw("ROOT was not mailed about the operational store", "mail", m.Subject, "user", to.ID, "error", err)
		}
	})
}

// lastWords mails ROOT that the node stops, and waits at most p.lastWords for
// it: past that the process ends with the mail unsent.
func (s *QNTXServer) lastWords(p operationalPatience, root *services.MailRecipient, m services.NodeMail) {
	if s.nodeMailer == nil || root == nil {
		s.mailRoot(root, m)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.lastWords)
	defer cancel()
	sent := make(chan error, 1)
	to := *root
	sacred.Go("operational.last_words", func() {
		var err error
		defer func() { sent <- err }()
		defer sacred.Recovered("operational.last_words", &err)
		_, _, err = s.nodeMailer.SendAsNodeTo(ctx, to, m)
	})
	select {
	case err := <-sent:
		if err != nil {
			s.logger.Errorw("ROOT was not mailed that the node stops", "user", to.ID, "error", err)
		}
	case <-ctx.Done():
		s.logger.Errorw("ROOT was not mailed that the node stops: the mail took too long",
			"user", to.ID, "waited", p.lastWords)
	}
}

// stalledMail is the mail a wait on the operational store is, to ROOT.
func stalledMail(waited time.Duration, p operationalPatience, pool operationalPool, turned []string, dies bool) services.NodeMail {
	waited = waited.Truncate(time.Second)
	var text strings.Builder
	subject := fmt.Sprintf("The operational store has not answered for %s", waited)
	if dies {
		subject = fmt.Sprintf("QNTX stops: the operational store has not answered for %s", waited)
		fmt.Fprintf(&text, "The operational store has not answered for %s, so the node stops.\n\n", waited)
	} else {
		fmt.Fprintf(&text, "The operational store has not answered for %s.\n", waited)
		fmt.Fprintf(&text, "The node stops if it has not answered at %s.\n\n", p.die)
	}
	fmt.Fprintf(&text, "It holds the passkeys, jobs, schedules and canvas.\n\n%s\n", pool)
	if len(turned) > 0 {
		fmt.Fprintf(&text, "\nTurned away until it answers in time: %s\n", strings.Join(turned, ", "))
	}
	return services.NodeMail{
		Name:    "operational.stalled",
		Subject: subject,
		Text:    text.String(),
		HTML:    "<pre>" + html.EscapeString(text.String()) + "</pre>",
	}
}

// recoveredMail is the mail saying the operational store answers in time again.
func recoveredMail(stalled, answered time.Time, longest time.Duration, lifted []string, p operationalPatience) services.NodeMail {
	took := answered.Sub(stalled).Round(time.Second)
	var text strings.Builder
	fmt.Fprintf(&text, "The operational store answers within %s again.\n\n", p.sentry)
	fmt.Fprintf(&text, "It stopped doing so at %s and did again at %s: %s.\n",
		stalled.UTC().Format(time.RFC3339), answered.UTC().Format(time.RFC3339), took)
	fmt.Fprintf(&text, "The longest it kept the node waiting was %s.\n", longest.Round(time.Millisecond))
	if len(lifted) > 0 {
		fmt.Fprintf(&text, "\nLet back in: %s\n", strings.Join(lifted, ", "))
	}
	return services.NodeMail{
		Name:    "operational.recovered",
		Subject: fmt.Sprintf("The operational store answers again, after %s", took),
		Text:    text.String(),
		HTML:    "<pre>" + html.EscapeString(text.String()) + "</pre>",
	}
}
