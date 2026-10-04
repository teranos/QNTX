package server

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// quote.provenance is the built-in the standing quote row reaches. ground's
// PostToolUse attests the quoted spans a write carried, sky streams the row
// here, and each span is asked of the prompts in the namespace it landed in.

// quotePromptPredicates are the rows that hold what the person typed.
var quotePromptPredicates = []string{"UserPromptSubmit", "QueuedPromptSubmit"}

// quotePromptsCeiling is a read of every prompt a namespace holds. Zero would
// be none of them.
const quotePromptsCeiling = 1_000_000

// The namespace a standing row fired in, carried to the built-in it reaches.
type namespaceKey struct{}

func withNamespace(ctx context.Context, namespace string) context.Context {
	return context.WithValue(ctx, namespaceKey{}, namespace)
}

func namespaceOf(ctx context.Context) string {
	ns, _ := ctx.Value(namespaceKey{}).(string)
	return ns
}

type quoteHandler struct {
	// prompts is every prompt the namespace holds, as typed.
	prompts func(namespace string) ([]string, error)
	news    *newsLog
	// Who the token that attested the claim speaks for, as ci.watch files it.
	mintedBy func(did string) (string, bool)
	logger   *zap.SugaredLogger
}

func (h *quoteHandler) Name() string { return watcher.QuoteProvenanceHandlerName }

func (h *quoteHandler) Execute(ctx context.Context, job *async.Job) error {
	var as types.As
	if err := json.Unmarshal(job.Payload, &as); err != nil {
		return errors.Wrap(err, "quote.provenance payload is not an attestation")
	}
	if len(as.Actors) == 0 || as.Actors[0] == "" {
		return errors.Newf("quote.provenance: attestation %s has no actor to address the verdict to", as.ID)
	}
	namespace := namespaceOf(ctx)
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

	var unsourced, stretched []string
	for _, span := range spans {
		switch verdictOf(span, said) {
		case quoteUnsourced:
			unsourced = append(unsourced, span)
		case quoteStretched:
			stretched = append(stretched, span)
		}
	}
	if len(unsourced) == 0 && len(stretched) == 0 {
		return nil
	}
	h.leave(as, namespace, unsourced, stretched, len(said))
	return nil
}

// quoteNoteSpan is how much of a span the row's note carries; the detail has
// every span whole.
const quoteNoteSpan = 80

func (h *quoteHandler) leave(as types.As, namespace string, unsourced, stretched []string, read int) {
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
	if len(shown) > quoteNoteSpan {
		shown = shown[:quoteNoteSpan]
	}
	note := word + ": \"" + shown + "\""
	if more > 0 {
		note += " and " + strconv.Itoa(more) + " more"
	}
	h.news.leave(News{
		ID:   as.ID,
		For:  addressee,
		Item: StatusItem{Name: "quote", Note: note, Symbol: SymbolUnwell},
		Detail: map[string]any{
			"session":      sessionOf(as.Contexts),
			"file_path":    attrString(as.Attributes, "file_path"),
			"unsourced":    unsourced,
			"stretched":    stretched,
			"prompts_read": read,
			"namespace":    namespace,
			"caller":       caller,
		},
		UntilMs: time.Now().Add(newsHold).UnixMilli(),
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

// namespacePrompts is every prompt a namespace holds. A store that will not
// answer is an error, not no prompts: the two read the same, and one of them
// calls every quote invented.
func (s *QNTXServer) namespacePrompts(namespace string) ([]string, error) {
	if s.held == nil {
		return nil, errors.New("no namespaces are held to read prompts from")
	}
	store, err := s.held.Read(namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "opening %s to read its prompts", namespace)
	}
	found, err := store.GetAttestations(ats.AttestationFilter{Predicates: quotePromptPredicates, Limit: quotePromptsCeiling})
	if err != nil {
		return nil, errors.Wrapf(err, "reading the prompts of %s", namespace)
	}
	prompts := make([]string, 0, len(found))
	for _, as := range found {
		if p := attrString(as.Attributes, "prompt"); p != "" {
			prompts = append(prompts, p)
		}
	}
	return prompts, nil
}

// setupQuoteProvenance registers the built-in. No schedule: the standing row
// reaches it when a claim arrives.
func (s *QNTXServer) setupQuoteProvenance() {
	if s.daemon == nil {
		return
	}
	if s.news == nil {
		s.news = newNewsLog()
	}
	s.daemon.Registry().Register(&quoteHandler{
		prompts:  s.namespacePrompts,
		news:     s.news,
		mintedBy: s.authHandler.MintedBy,
		logger:   s.logger.Named("quote.provenance"),
	})
}
