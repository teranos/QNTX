package otlp

import (
	"context"
	"net/url"
	"strings"
	"sync"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// "Where is all of our spans usage going towards?"
//
// 「[Sonnet 5.5, Opus 5.5] A span is routine when nothing went wrong in it and it was quick: what a」
// 「[Sonnet 5.5, Opus 5.5] client polls on purpose, or a job that ran as it always does. Which spans」
// 「[Sonnet 5.5, Opus 5.5] are routine is said once the span ended, so an error or a slow one is still」
// 「[Sonnet 5.5, Opus 5.5] sent, and what the node counts of every request is counted all the same.」

var (
	routineMu sync.Mutex
	routine   func(sdktrace.ReadOnlySpan) bool
)

// 「[Sonnet 5.5, Opus 5.5] DropRoutine says which ended spans are not sent. Called before Start.」
func DropRoutine(isRoutine func(sdktrace.ReadOnlySpan) bool) {
	routineMu.Lock()
	defer routineMu.Unlock()
	routine = isRoutine
}

// 「[Sonnet 5.5, Opus 5.5] keepingAll is the rule Start wraps every processor with.」
func keepingAll() func(sdktrace.ReadOnlySpan) bool {
	routineMu.Lock()
	defer routineMu.Unlock()
	return routine
}

// 「[Sonnet 5.5, Opus 5.5] notRoutine passes on every span the rule does not call routine.」
type notRoutine struct {
	next      sdktrace.SpanProcessor
	isRoutine func(sdktrace.ReadOnlySpan) bool
}

func (p notRoutine) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) { p.next.OnStart(ctx, s) }
func (p notRoutine) OnEnd(s sdktrace.ReadOnlySpan) {
	if p.isRoutine(s) {
		return
	}
	p.next.OnEnd(s)
}
func (p notRoutine) Shutdown(ctx context.Context) error { return p.next.Shutdown(ctx) }
func (p notRoutine) ForceFlush(ctx context.Context) error { return p.next.ForceFlush(ctx) }

// 「[Sonnet 5.5, Opus 5.5] SaidIsSentry is whether the traces endpoint the environment names is」
// 「[Sonnet 5.5, Opus 5.5] Sentry's own OTLP ingest. Spans that go there need not go to Sentry a」
// 「[Sonnet 5.5, Opus 5.5] second time by Sentry's span processor.」
func SaidIsSentry() bool {
	endpoint, said := Said()
	if !said {
		return false
	}
	at, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	host := strings.ToLower(at.Hostname())
	return host == "sentry.io" || strings.HasSuffix(host, ".sentry.io")
}
