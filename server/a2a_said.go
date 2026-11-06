package server

import (
	"context"
	"log/slog"

	a2alog "github.com/a2aproject/a2a-go/v2/log"
	"github.com/getsentry/sentry-go"
	sentryslog "github.com/getsentry/sentry-go/slog"
)

// a2aSaid is the logger a2a-go says what went wrong in. "Observability, this
// kind, needs to be owned by sentry/otel": while Sentry runs, its warnings and
// errors are Sentry logs and nothing else, written to no disk. A node without
// Sentry leaves a2a-go as it is.
func a2aSaid() *slog.Logger {
	if sentry.CurrentHub().Client() == nil {
		return slog.Default()
	}
	toSentry := sentryslog.Option{
		EventLevel: []slog.Level{},
		LogLevel:   []slog.Level{slog.LevelWarn, slog.LevelError},
	}.NewSentryHandler(context.Background())
	return slog.New(a2alog.AttachFormatter(toSentry, a2alog.DefaultA2ATypeFormatter))
}

// withA2ASaid is ctx carrying a2aSaid, for what a2a-go says outside a handler it was given it in.
func withA2ASaid(ctx context.Context) context.Context {
	return a2alog.AttachLogger(ctx, a2aSaid())
}
