package logger

import (
	"context"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// recordingLogger stands in for the SDK's logger so a test can read what the
// core built without a network in the way. Every level hands back the same
// entry, and the test asserts on what landed in it.
type recordingLogger struct {
	entry *sentry.MockLogEntry
	level sentry.LogLevel
}

func newRecordingLogger() *recordingLogger {
	return &recordingLogger{entry: sentry.NewMockLogEntry()}
}

func (r *recordingLogger) at(level sentry.LogLevel) sentry.LogEntry {
	r.level = level
	return r.entry
}

func (r *recordingLogger) Trace() sentry.LogEntry  { return r.at(sentry.LogLevelTrace) }
func (r *recordingLogger) Debug() sentry.LogEntry  { return r.at(sentry.LogLevelDebug) }
func (r *recordingLogger) Info() sentry.LogEntry   { return r.at(sentry.LogLevelInfo) }
func (r *recordingLogger) Warn() sentry.LogEntry   { return r.at(sentry.LogLevelWarn) }
func (r *recordingLogger) Error() sentry.LogEntry  { return r.at(sentry.LogLevelError) }
func (r *recordingLogger) Fatal() sentry.LogEntry  { return r.at(sentry.LogLevelFatal) }
func (r *recordingLogger) Panic() sentry.LogEntry  { return r.at(sentry.LogLevelFatal) }
func (r *recordingLogger) LFatal() sentry.LogEntry { return r.at(sentry.LogLevelFatal) }

func (r *recordingLogger) Write(p []byte) (int, error)        { return len(p), nil }
func (r *recordingLogger) SetAttributes(...attribute.Builder) {}
func (r *recordingLogger) GetCtx() context.Context            { return context.Background() }

// coreWith returns a core writing into a recording logger, plus the recorder.
func coreWith(t *testing.T, level zapcore.Level, redactKeys []string) (*sentryCore, *recordingLogger) {
	t.Helper()
	rec := newRecordingLogger()
	return &sentryCore{
		LevelEnabler: level,
		log:          rec,
		hide:         redactor(redactKeys),
	}, rec
}

// A DSN is the switch. Without one nothing starts, and the global logger is
// left exactly as it was.
func TestAddSentryOutputWithoutDSNStartsNothing(t *testing.T) {
	before := Logger

	if err := AddSentryOutput(SentryOptions{DSN: ""}); err != nil {
		t.Fatalf("AddSentryOutput with no DSN returned %v, want nil", err)
	}
	if SentryRunning() {
		t.Error("SentryRunning() is true after a start with no DSN")
	}
	if Logger != before {
		t.Error("the global logger was replaced by a start that was supposed to do nothing")
	}
}

// FlushSentry is called on paths that run whether or not Sentry ever started.
func TestFlushSentryWithoutStart(t *testing.T) {
	FlushSentry()
}

// A field naming a way in never leaves, whatever it is called around it.
func TestRedactorHidesCredentialsAnywhereInTheKey(t *testing.T) {
	hide := redactor(nil)

	for _, key := range []string{
		"token", "access_token", "TokenHash", "refresh_token_id",
		"secret", "client_secret", "password", "passphrase",
		"api_key", "apikey", "private_key", "authorization", "Cookie",
	} {
		if !hide(key) {
			t.Errorf("redactor lets %q through", key)
		}
	}
}

// The operator's own names match the whole key, so a word that merely contains
// one is not swallowed. "candidate" holds "did" and is not a DID.
func TestRedactorMatchesExtraKeysWhole(t *testing.T) {
	hide := redactor([]string{"email", "did"})

	if !hide("email") || !hide("DID") {
		t.Error("redactor lets a configured key through")
	}
	for _, key := range []string{"candidate", "emails_sent", "did_change"} {
		if hide(key) {
			t.Errorf("redactor hides %q, which only contains a configured key", key)
		}
	}
}

// An empty list ships identity. Zero means zero.
func TestRedactorWithNoExtraKeysShipsIdentity(t *testing.T) {
	hide := redactor([]string{})

	if hide("email") || hide("did") {
		t.Error("redactor hides identity when no key was configured to hide")
	}
}

// What the core builds is what leaves. This asserts on the whole attribute set
// for one entry: the fields, the redaction, and the two things a log line loses
// when it leaves the machine it was written on.
func TestWriteBuildsRedactedAttributes(t *testing.T) {
	core, rec := coreWith(t, zapcore.InfoLevel, []string{"email"})

	err := core.Write(zapcore.Entry{
		Level:      zapcore.InfoLevel,
		Message:    "token minted",
		LoggerName: "auth",
	}, []zapcore.Field{
		zap.String("namespace", "acme"),
		zap.String("email", "someone@example.com"),
		zap.String("access_token", "qntx_live_abc123"),
		zap.Int("scopes", 2),
		zap.Bool("scoped", true),
		zap.Duration("took", 250*time.Millisecond),
	})
	if err != nil {
		t.Fatalf("Write returned %v", err)
	}

	want := map[string]interface{}{
		"namespace":    "acme",
		"email":        redactedValue,
		"access_token": redactedValue,
		// zap flattens every integer width to int64, and the attribute keeps
		// that width rather than narrowing it back.
		"scopes":  int64(2),
		"scoped":  true,
		"took_ms": int64(250),
		"logger":  "auth",
	}
	for key, expected := range want {
		got, ok := rec.entry.Attributes[key]
		if !ok {
			t.Errorf("attribute %q never reached the entry", key)
			continue
		}
		if got != expected {
			t.Errorf("attribute %q = %v (%T), want %v (%T)", key, got, got, expected, expected)
		}
	}
	if len(rec.entry.Attributes) != len(want) {
		t.Errorf("entry carries %d attributes, want %d: %v", len(rec.entry.Attributes), len(want), rec.entry.Attributes)
	}
}

// Fields set with With travel with every later entry, which is how the Named
// children across the codebase carry their context.
func TestWithCarriesFieldsOntoEveryEntry(t *testing.T) {
	base, rec := coreWith(t, zapcore.InfoLevel, nil)
	core := base.With([]zapcore.Field{zap.String("component", "pulse")})

	if err := core.Write(zapcore.Entry{Level: zapcore.InfoLevel, Message: "tick"}, []zapcore.Field{zap.Int("jobs", 3)}); err != nil {
		t.Fatalf("Write returned %v", err)
	}

	if rec.entry.Attributes["component"] != "pulse" {
		t.Errorf("the field set by With did not travel: %v", rec.entry.Attributes)
	}
	if rec.entry.Attributes["jobs"] != int64(3) {
		t.Errorf("the field on the entry did not travel: %v", rec.entry.Attributes)
	}
}

// With must not write back into the core it was called on, or two Named
// children would collect each other's fields.
func TestWithDoesNotMutateTheCoreItCameFrom(t *testing.T) {
	base, rec := coreWith(t, zapcore.InfoLevel, nil)

	base.With([]zapcore.Field{zap.String("component", "pulse")})

	if err := base.Write(zapcore.Entry{Level: zapcore.InfoLevel, Message: "tick"}, nil); err != nil {
		t.Fatalf("Write returned %v", err)
	}
	if _, found := rec.entry.Attributes["component"]; found {
		t.Error("a field added to a derived core came back on the original")
	}
}

// The Sentry level a zap level lands on.
func TestEntryForMapsLevels(t *testing.T) {
	cases := []struct {
		zap  zapcore.Level
		want sentry.LogLevel
	}{
		{zapcore.DebugLevel, sentry.LogLevelDebug},
		{zapcore.InfoLevel, sentry.LogLevelInfo},
		{zapcore.WarnLevel, sentry.LogLevelWarn},
		{zapcore.ErrorLevel, sentry.LogLevelError},
		{zapcore.DPanicLevel, sentry.LogLevelFatal},
		{zapcore.PanicLevel, sentry.LogLevelFatal},
		{zapcore.FatalLevel, sentry.LogLevelFatal},
	}

	for _, c := range cases {
		core, rec := coreWith(t, zapcore.DebugLevel, nil)
		core.entryFor(c.zap)
		if rec.level != c.want {
			t.Errorf("zap %s lands on sentry %q, want %q", c.zap, rec.level, c.want)
		}
	}
}

// sentry.Logger.Fatal ends the process on Emit. A sink that exits would take
// the node down on a line zap was going to handle itself, so the fatal levels
// go through LFatal. This test is the reason that call is not Fatal().
func TestFatalLevelDoesNotEndTheProcess(t *testing.T) {
	core, _ := coreWith(t, zapcore.DebugLevel, nil)

	// Reaching the line after this is the assertion.
	if err := core.Write(zapcore.Entry{Level: zapcore.FatalLevel, Message: "the store is gone"}, nil); err != nil {
		t.Fatalf("Write returned %v", err)
	}
}

// Below the configured level nothing is checked, so nothing is built and
// nothing leaves.
func TestCheckHonoursMinLevel(t *testing.T) {
	core, _ := coreWith(t, zapcore.WarnLevel, nil)

	if ce := core.Check(zapcore.Entry{Level: zapcore.InfoLevel}, nil); ce != nil {
		t.Error("an info entry was checked by a core set to warn")
	}
	if ce := core.Check(zapcore.Entry{Level: zapcore.ErrorLevel}, nil); ce == nil {
		t.Error("an error entry was not checked by a core set to warn")
	}
}

// The flattened map holds only an error's text. The error itself is what
// carries the stack, and that is what an issue is grouped on.
func TestFirstErrorFindsTheErrorItself(t *testing.T) {
	wrapped := errors.New("the store is gone")

	found := firstError(
		[]zapcore.Field{zap.String("namespace", "acme")},
		[]zapcore.Field{zap.Int("attempt", 2), zap.Error(wrapped)},
	)
	if found != wrapped {
		t.Errorf("firstError = %v, want the error that was logged", found)
	}

	if found := firstError([]zapcore.Field{zap.String("error", "just text")}); found != nil {
		t.Errorf("firstError = %v on fields carrying no error", found)
	}
}

// An issue is raised alongside the log line at error and above, and the
// redaction that applies to the log applies to what the issue carries.
func TestCaptureIssueRaisesARedactedEvent(t *testing.T) {
	transport := &sentry.MockTransport{}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://key@example.invalid/1",
		Transport: transport,
	}); err != nil {
		t.Fatalf("sentry.Init returned %v", err)
	}
	defer func() {
		// Leave no client behind for the next test in this package.
		sentry.CurrentHub().BindClient(nil)
	}()

	core, _ := coreWith(t, zapcore.InfoLevel, []string{"email"})
	core.captureErrors = true

	failure := errors.New("the store is gone")
	if err := core.Write(zapcore.Entry{
		Level:      zapcore.ErrorLevel,
		Message:    "attestation refused",
		LoggerName: "auth",
	}, []zapcore.Field{
		zap.Error(failure),
		zap.String("email", "someone@example.com"),
		zap.String("access_token", "qntx_live_abc123"),
	}); err != nil {
		t.Fatalf("Write returned %v", err)
	}

	events := transport.Events()
	if len(events) != 1 {
		t.Fatalf("captureIssue raised %d events, want 1", len(events))
	}

	event := events[0]
	if event.Level != sentry.LevelError {
		t.Errorf("event level = %q, want %q", event.Level, sentry.LevelError)
	}
	if event.Tags["logger"] != "auth" {
		t.Errorf("event logger tag = %q, want %q", event.Tags["logger"], "auth")
	}

	logContext, ok := event.Contexts["log"]
	if !ok {
		t.Fatalf("event carries no log context: %v", event.Contexts)
	}
	if logContext["message"] != "attestation refused" {
		t.Errorf("log context message = %v, want the entry's message", logContext["message"])
	}
	if logContext["email"] != redactedValue {
		t.Errorf("log context email = %v, want %q", logContext["email"], redactedValue)
	}
	if logContext["access_token"] != redactedValue {
		t.Errorf("log context access_token = %v, want %q", logContext["access_token"], redactedValue)
	}
	if len(event.Exception) == 0 {
		t.Fatal("the event carries no exception, so it groups on the message instead of the error")
	}
	if event.Exception[0].Value != failure.Error() {
		t.Errorf("exception value = %q, want %q", event.Exception[0].Value, failure.Error())
	}
}

// At info and below there is a log line and no issue: an issue that fires on
// every info line is an inbox nobody reads.
func TestCaptureIssueStaysBelowError(t *testing.T) {
	transport := &sentry.MockTransport{}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://key@example.invalid/1",
		Transport: transport,
	}); err != nil {
		t.Fatalf("sentry.Init returned %v", err)
	}
	defer func() { sentry.CurrentHub().BindClient(nil) }()

	core, _ := coreWith(t, zapcore.InfoLevel, nil)
	core.captureErrors = true

	if err := core.Write(zapcore.Entry{Level: zapcore.WarnLevel, Message: "slow query"}, nil); err != nil {
		t.Fatalf("Write returned %v", err)
	}
	if got := len(transport.Events()); got != 0 {
		t.Errorf("a warn entry raised %d events, want 0", got)
	}
}
