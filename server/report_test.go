package server

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/sentryread"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
)

// "for a user that is one of the root identities, i want to receive a weekly report."

var reportEnd = time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

func madeAt(t *testing.T, store ats.AttestationStore, id string, at time.Time, predicate, subject string) {
	t.Helper()
	require.NoError(t, store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{subject}, Predicates: []string{predicate}, Contexts: []string{"garden"},
		Actors: []string{"did:key:z6Mkgarden"}, Timestamp: at, CreatedAt: at, Source: "test",
	}))
}

// "y attestations got created per namespace"
//
// No store counts by time and no read pages past its limit, so a week that
// fills a read is split and counted in halves. The store is the one production
// reads through, because the split depends on the store reading by time.
func TestAWeekOfAttestationsIsCountedPastTheReadLimit(t *testing.T) {
	store, err := storage.NewStore(":memory:", zap.NewNop().Sugar(), auth.NamespaceDefault)
	require.NoError(t, err)
	start := reportEnd.Add(-reportWeek)
	for i := range 7 {
		madeAt(t, store, "AS-IN-"+string(rune('A'+i)), start.Add(time.Duration(i+1)*time.Hour), "said", "tim")
	}
	madeAt(t, store, "AS-BEFORE", start.Add(-time.Hour), "said", "tim")
	madeAt(t, store, "AS-AFTER", reportEnd.Add(time.Hour), "said", "tim")

	n, err := attestationsMadeIn(store, start, reportEnd, 3)
	require.NoError(t, err)
	assert.Equal(t, 7, n)
}

// A store that does not read by time cannot be counted by splitting the week,
// and says so rather than splitting forever or answering a wrong number.
func TestAStoreThatDoesNotReadByTimeIsNotCounted(t *testing.T) {
	store, _ := createTestStore(t)
	start := reportEnd.Add(-reportWeek)
	madeAt(t, store, "AS-IN", start.Add(time.Hour), "said", "tim")
	for i := range 3 {
		madeAt(t, store, "AS-BEFORE-"+string(rune('A'+i)), start.Add(-time.Duration(i+1)*time.Hour), "said", "tim")
	}

	_, err := attestationsMadeIn(store, start, reportEnd, 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not read by time")
}

// "x users registered per namespace"
func TestRegistrationsAreCountedPerDoorForTheWeek(t *testing.T) {
	w := sentryread.Window{Start: reportEnd.Add(-reportWeek), End: reportEnd}
	in := reportEnd.Add(-24 * time.Hour).UnixMilli()
	counts := registrationsIn([]auth.User{
		{ID: "USroot", Level: auth.LevelRoot, CreatedAt: in},
		{ID: "UStim", Level: auth.LevelPublicRegistration, Namespace: "garden", CreatedAt: in},
		{ID: "USjenny", Level: auth.LevelPublicRegistration, Namespace: "garden", CreatedAt: in},
		{ID: "USspike", Level: auth.LevelPublicRegistration, Namespace: "orchard", CreatedAt: in},
		{ID: "USold", Level: auth.LevelPublicRegistration, Namespace: "orchard", CreatedAt: reportEnd.Add(-30 * 24 * time.Hour).UnixMilli()},
	}, w)
	assert.Equal(t, []namespaceCount{{Namespace: "garden", Count: 2}, {Namespace: "orchard", Count: 1}}, counts)
}

// "z node restarts"
func TestRestartsAreThisNodesStartsInTheWeek(t *testing.T) {
	store, _ := createTestStore(t)
	w := sentryread.Window{Start: reportEnd.Add(-reportWeek), End: reportEnd}
	madeAt(t, store, "AS-S1", reportEnd.Add(-48*time.Hour), PredicateStarted, "did:key:z6Mkgarden")
	madeAt(t, store, "AS-S2", reportEnd.Add(-2*time.Hour), PredicateStarted, "did:key:z6Mkgarden")
	madeAt(t, store, "AS-OTHER", reportEnd.Add(-2*time.Hour), PredicateStarted, "did:key:z6Mkorchard")
	madeAt(t, store, "AS-OLD", reportEnd.Add(-10*24*time.Hour), PredicateStarted, "did:key:z6Mkgarden")

	n, said := startsOf(store, "did:key:z6Mkgarden", w)
	assert.Empty(t, said)
	assert.Equal(t, 2, n)
}

// "downtime over last week", "downtime over last 3 weeks"
//
// Minutes without a CPU sample, from the first hour Sentry holds one for.
func TestDowntimeIsTheMinutesWithoutASample(t *testing.T) {
	end := reportEnd
	var points []sentryread.Point
	for h := 21 * 24; h > 0; h-- {
		at := end.Add(-time.Duration(h) * time.Hour)
		count := 60.0
		switch {
		case at.Before(end.Add(-15 * 24 * time.Hour)):
			count = 0 // before Sentry held anything
		case at.Equal(end.Add(-10 * 24 * time.Hour)):
			count = 30 // down half an hour, two weeks ago
		case at.Equal(end.Add(-3 * time.Hour)):
			count = 55 // a restart this week
		case at.Equal(end.Add(-2 * time.Hour)):
			count = 61 // two processes overlapping is not negative downtime
		}
		points = append(points, sentryread.Point{At: at, Value: count})
	}

	d := downtimeFrom(points, end)
	assert.Empty(t, d.Err)
	assert.Equal(t, 5, d.WeekMinutes)
	assert.Equal(t, 35, d.ThreeWeeksMinutes)
	assert.Equal(t, end.Add(-15*24*time.Hour), d.Since)
}

// A Sentry that answers, for the report's questions: every series a steady
// hour, every table one row.
type answeringSentry struct{}

func (answeringSentry) Series(_ context.Context, q sentryread.SeriesQuery) ([]sentryread.Point, error) {
	var points []sentryread.Point
	for at := q.Window.Start; at.Before(q.Window.End); at = at.Add(time.Hour) {
		v := 40.0
		if strings.HasPrefix(q.YAxis, "count(") {
			v = 60
		}
		points = append(points, sentryread.Point{At: at, Value: v})
	}
	return points, nil
}

func (answeringSentry) Table(_ context.Context, q sentryread.TableQuery) ([]map[string]any, error) {
	row := map[string]any{}
	for _, f := range q.Fields {
		switch f {
		case "subsystem":
			row[f] = "store-proof"
		case "path":
			row[f] = "/api/attestations"
		case "tags[status,number]":
			row[f] = 404.0
		default:
			row[f] = 2.0
		}
	}
	return []map[string]any{row}, nil
}

// The report is one mail: html with the three graphs inline, and text beside.
func TestTheReportRendersEverySectionWithItsGraphs(t *testing.T) {
	s := rootKnowingServer(t)
	r := s.gatherReport(context.Background(), reportEnd, answeringSentry{}, nil)
	mail, err := renderReport(r)
	require.NoError(t, err)

	assert.Equal(t, reportHandlerName, mail.Name)
	require.Len(t, mail.Inline, 3)
	for _, img := range mail.Inline {
		assert.Contains(t, mail.HTML, `src="cid:`+img.ContentID+`"`)
		_, err := png.Decode(bytes.NewReader(img.Data))
		require.NoError(t, err, img.ContentID)
	}
	for _, section := range []string{
		"Attestations created per namespace", "Users registered per namespace",
		"Downtime", "CPU over 7 days", "Memory over 7 days", "Swap over 7 days", "Network over 7 days",
		"boot.subsystem.took over 7 days", "Query took over 7 days", "Top 3 4xx", "Top 3 5xx", "Top 3 handler failures",
	} {
		assert.Contains(t, mail.HTML, section)
		assert.Contains(t, mail.Text, section)
	}
	assert.Contains(t, mail.HTML, "Restarts:")
	assert.Contains(t, mail.Text, "Node restarts")
	assert.Contains(t, mail.Text, "no query's text is recorded")
}

// "I DONT VARE ABOUT X 4XX'S"
// "I WANT TO SEE WHAT WAS TRIED TO ACCESS INSTEAD"
//
// A ranked status is the path that was asked for, not how often.
func TestAStatusIsThePathThatWasAskedFor(t *testing.T) {
	s := rootKnowingServer(t)
	mail, err := renderReport(s.gatherReport(context.Background(), reportEnd, answeringSentry{}, nil))
	require.NoError(t, err)
	assert.Contains(t, mail.HTML, "/api/attestations")
	assert.Contains(t, mail.Text, "  404 /api/attestations\n")
}

// The graph stands in the report's window, drawn in QNTX's own colours.
func TestTheGraphStandsInTheWindow(t *testing.T) {
	s := rootKnowingServer(t)
	mail, err := renderReport(s.gatherReport(context.Background(), reportEnd, answeringSentry{}, nil))
	require.NoError(t, err)
	ink, err := services.MailGraphColours()
	require.NoError(t, err)

	require.NotEmpty(t, mail.Inline)
	img, err := png.Decode(bytes.NewReader(mail.Inline[0].Data))
	require.NoError(t, err)
	at := func(x, y int) string {
		r, g, b, _ := img.At(x, y).RGBA()
		return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	hex := func(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }
	assert.Equal(t, hex(ink.Background), at(0, 0))
	assert.Equal(t, hex(ink.Line), at(0, yOf(40)), "a steady 40%")
}

// "can you do it please to the degree that is feasible given sentry access?"
//
// Without Sentry the report still goes out, and every section Sentry would
// have filled says why it is empty.
func TestAReportWithoutSentrySaysWhy(t *testing.T) {
	s := rootKnowingServer(t)
	_, why := sentryReaderFrom(context.Background(), appcfg.SentryConfig{API: "https://sentry.example"})
	require.Error(t, why)

	r := s.gatherReport(context.Background(), reportEnd, nil, why)
	mail, err := renderReport(r)
	require.NoError(t, err)
	assert.Empty(t, mail.Inline)
	assert.Contains(t, mail.Text, "Node restarts")

	// Why Sentry was not asked is said once, not under every section it
	// would have filled.
	assert.Equal(t, 1, strings.Count(mail.Text, "sentry.read_token"), mail.Text)
	assert.Equal(t, 1, strings.Count(mail.HTML, "sentry.read_token"))
	assert.NotContains(t, mail.Text, "CPU over 7 days")
}

// A token created after the node started is read by the next report: Sentry's
// settings are resolved when the report is written, not once at boot.
func TestSentryIsResolvedWhenTheReportIsWritten(t *testing.T) {
	s := rootKnowingServer(t)
	s.sentryConfig = appcfg.SentryConfig{API: "https://sentry.example", Organization: "garden", Project: "1234"}

	t.Setenv("QNTX_TEST_SENTRY_TOKEN", "")
	s.sentryConfig.ReadTokenRef = "env:QNTX_TEST_SENTRY_TOKEN"
	_, why := s.sentryReader(context.Background())
	require.Error(t, why)

	t.Setenv("QNTX_TEST_SENTRY_TOKEN", "sntryu_read")
	reader, why := s.sentryReader(context.Background())
	require.NoError(t, why)
	assert.Equal(t, "sntryu_read", reader.Token)
}

// The report goes to the ROOT User's primary address, as the node's own mail.
func TestTheReportIsMailedToTheRootUser(t *testing.T) {
	s := rootKnowingServer(t)
	users, _, err := auth.OpenUserTable(s.nodeDB, nil)
	require.NoError(t, err)
	require.NoError(t, users.Put(auth.User{ID: "USroot", Level: auth.LevelRoot, EmailAddresses: []string{"root@garden.test"}}))
	s.authHandler, err = auth.New(nil, "localhost", nil, 8770, 8820, 24, zap.NewNop().Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		nil, users, false, []string{rootAccount}, nil)
	require.NoError(t, err)

	box := &kept{}
	mail := services.NewMailServer("token", zap.NewNop().Sugar())
	mail.Wire(services.MailWiring{From: "Garden <mail@garden.test>", Transport: box,
		Recipients: s.mailRecipient, Records: s.mailRecords, Actor: "did:key:z6Mkgardennode"})
	s.nodeMailer = mail

	// Sentry is not set up here; the report says so and goes.
	s.sentryConfig = appcfg.SentryConfig{}
	sent, err := s.sendReport(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "root@garden.test", sent.To)
	require.Len(t, box.mails, 1)
	assert.Equal(t, "root@garden.test", box.mails[0].To)
	assert.NotEmpty(t, sent.AttestationID)
}

// A ROOT User without an address gets no report, and the refusal says so.
func TestNoReportGoesToARootUserWithoutAnAddress(t *testing.T) {
	s := rootKnowingServer(t)
	users, _, err := auth.OpenUserTable(s.nodeDB, nil)
	require.NoError(t, err)
	require.NoError(t, users.Put(auth.User{ID: "USroot", Level: auth.LevelRoot}))
	s.authHandler, err = auth.New(nil, "localhost", nil, 8770, 8820, 24, zap.NewNop().Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		nil, users, false, []string{rootAccount}, nil)
	require.NoError(t, err)

	box := &kept{}
	mail := services.NewMailServer("token", zap.NewNop().Sugar())
	mail.Wire(services.MailWiring{From: "Garden <mail@garden.test>", Transport: box,
		Recipients: s.mailRecipient, Records: s.mailRecords, Actor: "did:key:z6Mkgardennode"})
	s.nodeMailer = mail
	s.sentryConfig = appcfg.SentryConfig{}

	_, err = s.sendReport(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no email address")
	assert.Empty(t, box.mails)
}
