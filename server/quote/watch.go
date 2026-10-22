package quote

import (
	"context"
	"encoding/json"
	quoted "github.com/teranos/QNTX/ats/quote"
	"strconv"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/statusline"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// quote.provenance is the built-in the standing quote row reaches. ground's
// PostToolUse attests the quoted spans a write carried, sky streams the row
// here, and each span is asked of the prompts in the namespace it landed in.

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

// degree is one span that did not comply, and by how much.
type degree struct {
	Span        string `json:"span"`
	Verdict     string `json:"verdict"`
	Corrections int    `json:"corrections"`
	Length      int    `json:"length"`
}

// Watch is quote.provenance: the claims a write carried, asked of the prompts.
type Watch struct {
	// prompts is every prompt the namespace holds, as typed.
	prompts func(namespace string) ([]string, error)
	// judge is what the prompts say of each span (ats/quote), which is Rust's.
	judge func(spans, said []string) ([]quoted.Judgement, error)
	news  *statusline.NewsLog
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
		return errors.Newf("quote.provenance: attestation %s has no actor to address the verdict to", as.ID)
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

	judgements, err := h.judge(spans, said)
	if err != nil {
		return errors.Wrapf(err, "quote.provenance: judging the spans of %s against the prompts of %s", as.ID, namespace)
	}

	var unsourced, stretched []string
	var degrees []degree
	for i, span := range spans {
		j := judgements[i]
		switch j.Verdict {
		case quoted.Unsourced:
			unsourced = append(unsourced, span)
		case quoted.Stretched:
			stretched = append(stretched, span)
		default:
			continue
		}
		corrections, measured := j.Corrections()
		if !measured {
			return errors.Newf("quote.provenance: %s span %d of %s was judged %s and not measured", as.ID, i+1, namespace, j.Verdict)
		}
		degrees = append(degrees, degree{Span: span, Verdict: j.Verdict.String(),
			Corrections: corrections, Length: len(span)})
	}
	if len(degrees) == 0 {
		return nil
	}
	h.leave(as, namespace, unsourced, stretched, degrees, len(said))
	return nil
}

// noteSpan is how much of a span the row's note carries; the detail has
// every span whole.
const noteSpan = 80

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
func NewWatch(prompts func(namespace string) ([]string, error),
	judge func(spans, said []string) ([]quoted.Judgement, error), news *statusline.NewsLog,
	mintedBy func(did string) (string, bool), logger *zap.SugaredLogger) *Watch {
	return &Watch{prompts: prompts, judge: judge, news: news, mintedBy: mintedBy, logger: logger}
}
