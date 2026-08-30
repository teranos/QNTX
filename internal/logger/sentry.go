package logger

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Sentry is where a node's logs go when the box they run on is not the box the
// operator is sitting at. It is a second sink, not a second logging API: the
// core below is teed onto the global logger exactly the way AddFileOutput tees
// a file, so every one of the call sites already writing through logger.Logger
// ships without being touched.
//
// What leaves the process is decided here and nowhere else. A field whose name
// says it carries a way in never leaves at all.

// redactedValue stands in for a value that was not shipped. It is a fixed
// string so that a redacted field is visibly redacted rather than absent —
// an absent field reads as "nothing was set".
const redactedValue = "[redacted]"

// credentialWords name a value that is a way in. A field whose key contains
// one of these is replaced before the SDK ever sees it, and no configuration
// lifts that: there is no debugging worth shipping a token for.
var credentialWords = []string{
	"secret",
	"password",
	"passphrase",
	"token",
	"credential",
	"api_key",
	"apikey",
	"private_key",
	"privatekey",
	"authorization",
	"cookie",
}

// SentryOptions is everything the node needs to know before it ships anything.
type SentryOptions struct {
	// DSN is the project logs are shipped to. Empty is the off switch: nothing
	// starts, and no other field here is read.
	DSN string

	// Environment separates one node's stream from another's inside the same
	// project — the box, not the build.
	Environment string

	// Release is the build. Set it from internal/version so an issue names the
	// commit it came out of instead of leaving that to be worked out later.
	Release string

	// ServerName is which node this is. Empty lets the SDK read the hostname.
	ServerName string

	// MinLevel is the lowest zap level that reaches Sentry. Everything below it
	// stays in the console and the log file.
	MinLevel zapcore.Level

	// CaptureErrors raises an issue, and not only a log line, for Error and
	// above. A log line is one entry in a stream; an issue is grouped across
	// occurrences, counted, and carries the stack of the error that caused it.
	CaptureErrors bool

	// RedactKeys are field names replaced on top of the credential names that
	// are always replaced. Whole-key matches, because "did" sits inside
	// "candidate" and a substring rule would swallow it. This is where identity
	// goes — an email or a DID is not a credential and is still not the third
	// party's to hold.
	RedactKeys []string

	// Debug prints what the SDK itself is doing to stderr. It answers "did the
	// log leave" without guessing.
	Debug bool

	// FlushTimeout is how long the process waits for the batch to drain when it
	// ends. See FlushSentry.
	FlushTimeout time.Duration
}

var (
	sentryMu      sync.Mutex
	sentryRunning bool
	sentryFlushIn time.Duration
	// sentryHides is the running redaction test. What may leave this process is
	// one policy, not one per sink, so metrics ask the same question logs do.
	sentryHides atomic.Pointer[func(string) bool]
)

// Redacts reports whether a field or attribute by this name has its value
// replaced before it leaves the process. It answers for credential names
// whether or not Sentry ever started; the operator's own names are known only
// once the config has been read.
func Redacts(key string) bool {
	if hide := sentryHides.Load(); hide != nil {
		return (*hide)(key)
	}
	return redactor(nil)(key)
}

// AddSentryOutput starts the Sentry client and tees a Sentry core onto the
// global logger. Everything logged through logger.Logger from this point —
// Named children included — is shipped, so no call site changes.
//
// A DSN of "" returns nil without starting anything. That is the switch.
func AddSentryOutput(opt SentryOptions) error {
	if opt.DSN == "" {
		return nil
	}

	sentryMu.Lock()
	defer sentryMu.Unlock()
	if sentryRunning {
		return errors.New("AddSentryOutput called twice — the second client would replace the first and every entry would ship twice")
	}

	hide := redactor(opt.RedactKeys)
	sentryHides.Store(&hide)

	if err := sentry.Init(sentry.ClientOptions{
		Dsn:         opt.DSN,
		EnableLogs:  true,
		Environment: opt.Environment,
		Release:     opt.Release,
		ServerName:  opt.ServerName,
		Debug:       opt.Debug,
		// An issue without a stack is a string. With one it names the line.
		AttachStacktrace: true,
		// A request's cookies, headers and body are where a DID, an email and a
		// session live. false is also the SDK's default; it is written out
		// because it is a decision, not an omission.
		SendDefaultPII: false,
		// The core below redacts what it builds. This catches the same keys on
		// anything emitted straight through sentry.NewLogger, which is the
		// shape the SDK's own documentation reaches for.
		BeforeSendLog: func(l *sentry.Log) *sentry.Log {
			for key := range l.Attributes {
				if hide(key) {
					l.Attributes[key] = attribute.StringValue(redactedValue)
				}
			}
			return l
		},
	}); err != nil {
		return errors.Wrapf(err, "failed to start Sentry (environment=%q, release=%q, min_level=%s)",
			opt.Environment, opt.Release, opt.MinLevel)
	}

	core := &sentryCore{
		LevelEnabler:  opt.MinLevel,
		log:           sentry.NewLogger(context.Background()),
		hide:          hide,
		captureErrors: opt.CaptureErrors,
	}

	combined := zapcore.NewTee(Logger.Desugar().Core(), core)
	Logger = zap.New(combined).Sugar()

	sentryRunning = true
	sentryFlushIn = opt.FlushTimeout
	return nil
}

// FlushSentry waits for the batch to reach Sentry. The SDK buffers, so a
// process that exits without this drops whatever is still held — including the
// error that ended it, which is the one worth having.
//
// Safe to call when Sentry never started; it does nothing.
func FlushSentry() {
	sentryMu.Lock()
	defer sentryMu.Unlock()
	if !sentryRunning {
		return
	}
	sentry.Flush(sentryFlushIn)
}

// SentryRunning reports whether logs are being shipped. The startup banner and
// the health endpoint use it so that "is this node observed" is answerable
// without reading config and guessing.
func SentryRunning() bool {
	sentryMu.Lock()
	defer sentryMu.Unlock()
	return sentryRunning
}

// redactor returns the test applied to every field name. Credential words match
// anywhere in the key; the operator's extra names must match the whole key.
func redactor(extra []string) func(string) bool {
	whole := make(map[string]bool, len(extra))
	for _, key := range extra {
		whole[strings.ToLower(strings.TrimSpace(key))] = true
	}

	return func(key string) bool {
		lower := strings.ToLower(key)
		if whole[lower] {
			return true
		}
		for _, word := range credentialWords {
			if strings.Contains(lower, word) {
				return true
			}
		}
		return false
	}
}

// sentryCore is a zapcore.Core that emits to Sentry. It holds no buffer of its
// own — the SDK batches — so Sync has nothing to do and FlushSentry is what
// drains it.
type sentryCore struct {
	zapcore.LevelEnabler
	log           sentry.Logger
	fields        []zapcore.Field
	hide          func(string) bool
	captureErrors bool
}

func (c *sentryCore) With(fields []zapcore.Field) zapcore.Core {
	joined := make([]zapcore.Field, 0, len(c.fields)+len(fields))
	joined = append(joined, c.fields...)
	joined = append(joined, fields...)

	clone := *c
	clone.fields = joined
	return &clone
}

func (c *sentryCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *sentryCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	attrs := c.attributes(ent, fields)

	entry := c.entryFor(ent.Level)
	// Keys in a sorted order, so two occurrences of the same log read the same
	// way in the UI rather than differing by map iteration.
	for _, key := range sortedKeys(attrs) {
		entry = attach(entry, key, attrs[key])
	}
	entry.Emit(ent.Message)

	if c.captureErrors && ent.Level >= zapcore.ErrorLevel {
		c.captureIssue(ent, attrs, fields)
	}
	return nil
}

// Sync is where zap asks a core to flush. Sentry's batch is drained on a timer
// and at exit by FlushSentry, and blocking every logger.Sync on a network
// round trip would make the console wait on the network.
func (c *sentryCore) Sync() error { return nil }

// attributes flattens the zap fields the same way the JSON encoder does, then
// applies redaction. The logger name and the caller are added because they are
// the two things a log line loses when it leaves the machine it was written on.
func (c *sentryCore) attributes(ent zapcore.Entry, fields []zapcore.Field) map[string]interface{} {
	enc := zapcore.NewMapObjectEncoder()
	for _, field := range c.fields {
		field.AddTo(enc)
	}
	for _, field := range fields {
		field.AddTo(enc)
	}

	attrs := make(map[string]interface{}, len(enc.Fields)+2)
	for key, value := range enc.Fields {
		if c.hide(key) {
			attrs[key] = redactedValue
			continue
		}
		attrs[key] = value
	}

	if ent.LoggerName != "" {
		attrs["logger"] = ent.LoggerName
	}
	if ent.Caller.Defined {
		attrs["caller"] = ent.Caller.TrimmedPath()
	}
	return attrs
}

// entryFor maps a zap level onto a Sentry one.
//
// Fatal is deliberately LFatal: sentry.Logger.Fatal ends the process on Emit,
// and a logging sink that exits would take the node down on a line zap was
// going to handle itself.
func (c *sentryCore) entryFor(level zapcore.Level) sentry.LogEntry {
	switch {
	case level <= zapcore.DebugLevel:
		return c.log.Debug()
	case level == zapcore.InfoLevel:
		return c.log.Info()
	case level == zapcore.WarnLevel:
		return c.log.Warn()
	case level == zapcore.ErrorLevel:
		return c.log.Error()
	default:
		return c.log.LFatal()
	}
}

// captureIssue raises the entry as an issue alongside the log line. When a
// field carries the error itself the exception is captured from it, which is
// what carries the type and the stack; otherwise the message is all there is.
func (c *sentryCore) captureIssue(ent zapcore.Entry, attrs map[string]interface{}, fields []zapcore.Field) {
	hub := sentry.CurrentHub().Clone()
	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentryLevel(ent.Level))
		if ent.LoggerName != "" {
			scope.SetTag("logger", ent.LoggerName)
		}

		// The message travels in the context too. CaptureException groups on
		// the error, and the message is what says which call site raised it.
		payload := make(sentry.Context, len(attrs)+1)
		for key, value := range attrs {
			payload[key] = value
		}
		payload["message"] = ent.Message
		scope.SetContext("log", payload)

		if err := firstError(c.fields, fields); err != nil {
			hub.CaptureException(err)
			return
		}
		hub.CaptureMessage(ent.Message)
	})
}

// firstError finds the error a zap.Error field carries. The flattened map holds
// only its text; the error itself is what has a stack on it.
func firstError(sets ...[]zapcore.Field) error {
	for _, set := range sets {
		for _, field := range set {
			if field.Type != zapcore.ErrorType {
				continue
			}
			if err, ok := field.Interface.(error); ok {
				return err
			}
		}
	}
	return nil
}

func sentryLevel(level zapcore.Level) sentry.Level {
	switch {
	case level <= zapcore.DebugLevel:
		return sentry.LevelDebug
	case level == zapcore.InfoLevel:
		return sentry.LevelInfo
	case level == zapcore.WarnLevel:
		return sentry.LevelWarning
	case level == zapcore.ErrorLevel:
		return sentry.LevelError
	default:
		return sentry.LevelFatal
	}
}

func sortedKeys(attrs map[string]interface{}) []string {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// attach puts one flattened field onto the entry as its own type, so a count
// stays a number and a duration stays comparable in the Sentry UI. Anything
// with no typed home is printed rather than dropped.
func attach(entry sentry.LogEntry, key string, value interface{}) sentry.LogEntry {
	switch v := value.(type) {
	case string:
		return entry.String(key, v)
	case bool:
		return entry.Bool(key, v)
	case int:
		return entry.Int(key, v)
	case int8:
		return entry.Int64(key, int64(v))
	case int16:
		return entry.Int64(key, int64(v))
	case int32:
		return entry.Int64(key, int64(v))
	case int64:
		return entry.Int64(key, v)
	case uint:
		return entry.Int64(key, int64(v))
	case uint8:
		return entry.Int64(key, int64(v))
	case uint16:
		return entry.Int64(key, int64(v))
	case uint32:
		return entry.Int64(key, int64(v))
	case uint64:
		return entry.Int64(key, int64(v))
	case float32:
		return entry.Float64(key, float64(v))
	case float64:
		return entry.Float64(key, v)
	case []string:
		return entry.StringSlice(key, v)
	case time.Duration:
		// Milliseconds, named as such, because a bare int64 of nanoseconds is
		// unreadable next to every other duration in the UI.
		return entry.Int64(key+"_ms", v.Milliseconds())
	case time.Time:
		return entry.String(key, v.Format(time.RFC3339Nano))
	case error:
		return entry.String(key, v.Error())
	default:
		return entry.String(key, fmt.Sprint(v))
	}
}
