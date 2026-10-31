package quote

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	quoted "github.com/teranos/QNTX/ats/quote"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/statusline"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// The numbers this package is made of, so none is read out of the code below.
const (
	// noteSpan is how much of a span the row's note carries; the detail has
	// every span whole.
	noteSpan = 80

	// promptsCeiling is a read of every prompt a namespace holds. Zero would be
	// none of them.
	promptsCeiling = 1_000_000
)

// The namespace a standing row fired in, carried to the built-in it reaches.
type namespaceKey struct{}

// WithNamespace carries the namespace a standing row fired in.
func WithNamespace(ctx context.Context, namespace string) context.Context {
	return context.WithValue(ctx, namespaceKey{}, namespace)
}

// NamespaceOf is the namespace carried, or the empty string when none was,
// which the caller refuses out loud.
func NamespaceOf(ctx context.Context) string {
	ns, ok := ctx.Value(namespaceKey{}).(string)
	if !ok {
		return ""
	}
	return ns
}

// IMPLEMENTME: speak spec at the boundary. A span measured here is a protocol.Quote:
// content the span, who the person whose prompt it matched, source that prompt's
// row, resp the actor that relayed it, cert the measurement. degree stays inside.

// degree is one span that did not comply, and by how much.
type degree struct {
	Span        string `json:"span"`
	Kind        string `json:"verdict"`
	Corrections int    `json:"corrections"`
	Length      int    `json:"length"`
	// The prompt the span is nearest to: its row, who said it, when, in which session.
	Row     string `json:"row,omitempty"`
	Who     string `json:"who,omitempty"`
	At      string `json:"at,omitempty"`
	Session string `json:"session,omitempty"`
}

// Watch is quote.provenance: the claims a write carried, asked of the prompts.
type Watch struct {
	// prompts is every prompt the namespace holds, as typed.
	prompts func(namespace string) ([]Said, error)
	// measure is what was measured of each span against the prompts
	// (ats/quote), which is Rust's.
	measure func(spans, said []string) ([]quoted.Measurement, error)
	news    *statusline.NewsLog
	// Who the token that attested the claim speaks for, as ci.watch files it.
	mintedBy func(did string) (string, bool)
	logger   *zap.SugaredLogger
}

func (h *Watch) Name() string { return watcher.QuoteProvenanceHandlerName }

func (h *Watch) Execute(ctx context.Context, job *async.Job) error {
	var as types.As
	if err := json.Unmarshal(job.Payload, &as); err != nil {
		return errors.Wrap(err, "quote.provenance payload is not an attestation")
	}
	if len(as.Actors) == 0 || as.Actors[0] == "" {
		return errors.Newf("quote.provenance: attestation %s has no actor to address the result to", as.ID)
	}
	namespace := NamespaceOf(ctx)
	if namespace == "" {
		return errors.Newf("quote.provenance: attestation %s reached the built-in with no namespace to read prompts in", as.ID)
	}
	spans := attrStrings(as.Attributes, "spans")
	if len(spans) == 0 {
		return errors.Newf("quote.provenance: attestation %s claims no span", as.ID)
	}
	said, err := h.prompts(namespace)
	if err != nil {
		return errors.Wrapf(err, "quote.provenance: reading the prompts of %s for %s", namespace, as.ID)
	}

	measurements, err := h.measure(spans, Texts(said))
	if err != nil {
		return errors.Wrapf(err, "quote.provenance: measuring the spans of %s against the prompts of %s", as.ID, namespace)
	}

	var unsourced, stretched []string
	var degrees []degree
	for i, span := range spans {
		m := measurements[i]
		switch {
		case accepts(m):
			continue
		case near(m):
			stretched = append(stretched, span)
		default:
			unsourced = append(unsourced, span)
		}
		d := degree{Span: span, Kind: kind(m), Corrections: m.Corrections, Length: m.Length}
		if near, ok := saidAt(said, m); ok {
			d.Row, d.Who, d.At, d.Session = near.Row, near.Who, near.At, near.Session
		}
		degrees = append(degrees, d)
	}
	if len(degrees) == 0 {
		return nil
	}
	h.leave(as, namespace, unsourced, stretched, degrees, len(said))
	return nil
}

func (h *Watch) leave(as types.As, namespace string, unsourced, stretched []string, degrees []degree, read int) {
	caller := as.Actors[0]
	addressee := caller
	if h.mintedBy != nil {
		if who, ok := h.mintedBy(caller); ok {
			addressee = who
		}
	}
	first, word, more := unsourced, "no source", len(unsourced)-1
	if len(unsourced) == 0 {
		first, word, more = stretched, "stretched past four per forty", len(stretched)-1
	}
	shown := first[0]
	if len(shown) > noteSpan {
		shown = shown[:noteSpan]
	}
	note := word + ": \"" + shown + "\", " + strconv.Itoa(degrees[0].Corrections) +
		" corrections over " + strconv.Itoa(degrees[0].Length) + " characters"
	if more > 0 {
		note += " and " + strconv.Itoa(more) + " more"
	}
	h.news.Leave(statusline.News{
		ID:   as.ID,
		For:  addressee,
		Item: statusline.StatusItem{Name: "quote", Note: note, Symbol: statusline.SymbolUnwell},
		Detail: map[string]any{
			"session":      statusline.SessionOf(as.Contexts),
			"file_path":    attrString(as.Attributes, "file_path"),
			"unsourced":    unsourced,
			"stretched":    stretched,
			"degrees":      degrees,
			"prompts_read": read,
			"namespace":    namespace,
			"caller":       caller,
		},
		UntilMs: time.Now().Add(statusline.NewsHold).UnixMilli(),
	})
	h.logger.Infow("quote.provenance left news on the row",
		"attestation", as.ID, "unsourced", len(unsourced), "stretched", len(stretched),
		"prompts_read", read, "namespace", namespace, "for", addressee)
}

// attrStrings is a list attribute's strings; anything else in it is skipped.
func attrStrings(attrs map[string]any, key string) []string {
	raw, ok := attrs[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// attrString reads a string attribute, and the empty string when it is absent
// or not a string.
func attrString(attrs map[string]any, key string) string {
	if v, ok := attrs[key].(string); ok {
		return v
	}
	return ""
}

// NewWatch is quote.provenance, asking prompts for what a namespace was told.
func NewWatch(prompts func(namespace string) ([]Said, error),
	measure func(spans, said []string) ([]quoted.Measurement, error), news *statusline.NewsLog,
	mintedBy func(did string) (string, bool), logger *zap.SugaredLogger) *Watch {
	return &Watch{prompts: prompts, measure: measure, news: news, mintedBy: mintedBy, logger: logger}
}
