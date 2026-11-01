package quote

import (
	"context"
	"encoding/json"
	quoted "github.com/teranos/QNTX/ats/quote"
	"github.com/teranos/QNTX/server/namespaces"
	"strconv"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/statusline"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// quote.removal is the built-in the standing removal row reaches: the quoted
// spans a commit took out and put back nowhere, asked of the namespace's
// prompts, and the ones the person said go back to them.

// removedQuote is one of the person's quotes a commit took out.
type removedQuote struct {
	File    string `json:"file"`
	Span    string `json:"span"`
	Kind    string `json:"verdict"`
	Row     string `json:"row,omitempty"`
	Who     string `json:"who,omitempty"`
	At      string `json:"at,omitempty"`
	Session string `json:"session,omitempty"`
}

// Removal is quote.removal: the quotes a commit took out, asked of the prompts.
type Removal struct {
	// prompts is every prompt the namespace holds, as typed.
	prompts func(namespace string) ([]Said, error)
	// measure is what was measured of each span against the prompts
	// (ats/quote), which is Rust's.
	measure func(spans, said []string) ([]quoted.Measurement, error)
	news    *statusline.NewsLog
	// Who the token that attested the removal speaks for, as ci.watch files it.
	mintedBy func(did string) (string, bool)
	logger   *zap.SugaredLogger
}

func (h *Removal) Name() string { return watcher.QuoteRemovalHandlerName }

func (h *Removal) Execute(ctx context.Context, job *async.Job) error {
	var as types.As
	if err := json.Unmarshal(job.Payload, &as); err != nil {
		return errors.Wrap(err, "quote.removal payload is not an attestation")
	}
	if len(as.Actors) == 0 || as.Actors[0] == "" {
		return errors.Newf("quote.removal: attestation %s has no actor to address the result to", as.ID)
	}
	namespace := namespaces.NamespaceOf(ctx)
	if namespace == "" {
		return errors.Newf("quote.removal: attestation %s reached the built-in with no namespace to read prompts in", as.ID)
	}
	spans := attrStrings(as.Attributes, "spans")
	files := attrStrings(as.Attributes, "files")
	if len(spans) == 0 {
		return errors.Newf("quote.removal: attestation %s names no span", as.ID)
	}
	if len(files) != len(spans) {
		return errors.Newf("quote.removal: attestation %s names %d spans and %d files; each span is one file's",
			as.ID, len(spans), len(files))
	}
	said, err := h.prompts(namespace)
	if err != nil {
		return errors.Wrapf(err, "quote.removal: reading the prompts of %s for %s", namespace, as.ID)
	}

	measurements, err := h.measure(spans, Texts(said))
	if err != nil {
		return errors.Wrapf(err, "quote.removal: measuring the spans of %s against the prompts of %s", as.ID, namespace)
	}

	var removed []removedQuote
	for i, span := range spans {
		m := measurements[i]
		if !near(m) {
			continue
		}
		r := removedQuote{File: files[i], Span: span, Kind: kind(m)}
		if near, ok := SaidAt(said, m); ok {
			r.Row, r.Who, r.At, r.Session = near.Row, near.Who, near.At, near.Session
		}
		removed = append(removed, r)
	}
	if len(removed) == 0 {
		return nil
	}
	h.leave(as, namespace, attrString(as.Attributes, "commit"), removed, len(said))
	return nil
}

func (h *Removal) leave(as types.As, namespace, commit string, removed []removedQuote, read int) {
	caller := as.Actors[0]
	addressee := caller
	if who, ok := h.mintedBy(caller); ok {
		addressee = who
	}
	shown := removed[0].Span
	if len(shown) > noteSpan {
		shown = shown[:noteSpan]
	}
	note := "commit " + commit + " removed what you said from " + removed[0].File + ": \"" + shown + "\""
	if more := len(removed) - 1; more > 0 {
		note += " and " + strconv.Itoa(more) + " more"
	}
	h.news.Leave(statusline.News{
		ID:   as.ID,
		For:  addressee,
		Item: statusline.StatusItem{Name: "quote", Note: note, Symbol: statusline.SymbolUnwell},
		Detail: map[string]any{
			"session":      statusline.SessionOf(as.Contexts),
			"commit":       commit,
			"removed":      removed,
			"prompts_read": read,
			"namespace":    namespace,
			"caller":       caller,
		},
		UntilMs: time.Now().Add(statusline.NewsHold).UnixMilli(),
	})
	h.logger.Infow("quote.removal left news on the row",
		"attestation", as.ID, "commit", commit, "removed", len(removed),
		"prompts_read", read, "namespace", namespace, "for", addressee)
}

// NewRemoval is quote.removal, asking prompts for what a namespace was told.
func NewRemoval(prompts func(namespace string) ([]Said, error),
	measure func(spans, said []string) ([]quoted.Measurement, error), news *statusline.NewsLog,
	mintedBy func(did string) (string, bool), logger *zap.SugaredLogger) *Removal {
	return &Removal{prompts: prompts, measure: measure, news: news, mintedBy: mintedBy, logger: logger}
}
