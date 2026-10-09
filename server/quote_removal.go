package server

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// quote.removal is the built-in the standing removal row reaches: the quoted
// spans a commit took out and put back nowhere, asked of the namespace's
// prompts, and the ones the person said go back to them.

// quoteRemoved is one of the person's quotes a commit took out.
type quoteRemoved struct {
	File    string `json:"file"`
	Span    string `json:"span"`
	Verdict string `json:"verdict"`
}

type quoteRemovalHandler struct {
	// prompts is every prompt the namespace holds, as typed.
	prompts func(namespace string) ([]string, error)
	news    *newsLog
	// Who the token that attested the removal speaks for, as ci.watch files it.
	mintedBy func(did string) (string, bool)
	logger   *zap.SugaredLogger
}

func (h *quoteRemovalHandler) Name() string { return watcher.QuoteRemovalHandlerName }

func (h *quoteRemovalHandler) Execute(ctx context.Context, job *async.Job) error {
	var as types.As
	if err := json.Unmarshal(job.Payload, &as); err != nil {
		return errors.Wrap(err, "quote.removal payload is not an attestation")
	}
	if len(as.Actors) == 0 || as.Actors[0] == "" {
		return errors.Newf("quote.removal: attestation %s has no actor to address the verdict to", as.ID)
	}
	namespace := namespaceOf(ctx)
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

	var removed []quoteRemoved
	for i, span := range spans {
		v := verdictOf(span, said)
		if v == quoteUnsourced {
			continue
		}
		removed = append(removed, quoteRemoved{File: files[i], Span: span, Verdict: v.String()})
	}
	if len(removed) == 0 {
		return nil
	}
	h.leave(as, namespace, attrString(as.Attributes, "commit"), removed, len(said))
	return nil
}

func (h *quoteRemovalHandler) leave(as types.As, namespace, commit string, removed []quoteRemoved, read int) {
	caller := as.Actors[0]
	addressee := caller
	if h.mintedBy != nil {
		if who, ok := h.mintedBy(caller); ok {
			addressee = who
		}
	}
	shown := removed[0].Span
	if len(shown) > quoteNoteSpan {
		shown = shown[:quoteNoteSpan]
	}
	note := "commit " + commit + " removed what you said from " + removed[0].File + ": \"" + shown + "\""
	if more := len(removed) - 1; more > 0 {
		note += " and " + strconv.Itoa(more) + " more"
	}
	h.news.leave(News{
		ID:   as.ID,
		For:  addressee,
		Item: StatusItem{Name: "quote", Note: note, Symbol: SymbolUnwell},
		Detail: map[string]any{
			"session":      sessionOf(as.Contexts),
			"commit":       commit,
			"removed":      removed,
			"prompts_read": read,
			"namespace":    namespace,
			"caller":       caller,
		},
		UntilMs: time.Now().Add(newsHold).UnixMilli(),
	})
	h.logger.Infow("quote.removal left news on the row",
		"attestation", as.ID, "commit", commit, "removed", len(removed),
		"prompts_read", read, "namespace", namespace, "for", addressee)
}

// setupQuoteRemoval registers the built-in. No schedule: the standing row
// reaches it when a commit's removals arrive.
func (s *QNTXServer) setupQuoteRemoval() {
	if s.daemon == nil {
		return
	}
	if s.news == nil {
		s.news = newNewsLog()
	}
	s.daemon.Registry().Register(&quoteRemovalHandler{
		prompts:  s.namespacePrompts,
		news:     s.news,
		mintedBy: s.authHandler.MintedBy,
		logger:   s.logger.Named("quote.removal"),
	})
}
