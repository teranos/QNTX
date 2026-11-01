package quote

import (
	quoted "github.com/teranos/QNTX/ats/quote"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// An Arrival is material that reached the node by webhook and not by a row of
// ground's: a push's commit messages, a pull request's title and body, for a
// repository the node is told of by its forge.
type Arrival struct {
	// Project is the repository as ground names it in a row's context, owner/repo.
	Project string
	// Ref is the commit or pull request the texts came with.
	Ref string
	// Texts are the words that arrived, each measured for its quoted spans.
	Texts []string
}

// A Span is one quoted span of an Arrival, measured.
type Span struct {
	Span        string `json:"span"`
	Kind        string `json:"kind"`
	Corrections int    `json:"corrections"`
	Length      int    `json:"length"`
	// The prompt the span is nearest to: its row, who said it, when, in which session.
	Row     string `json:"row,omitempty"`
	Who     string `json:"who,omitempty"`
	At      string `json:"at,omitempty"`
	Session string `json:"session,omitempty"`
}

// Arrived is an Arrival measured. Known is whether some held namespace has a
// row of ground's for the project; without one, ground does not know the
// material, and every span is unsourced: that is what unprovenanced means.
type Arrived struct {
	Project   string `json:"project"`
	Ref       string `json:"ref"`
	Namespace string `json:"namespace"`
	Known     bool   `json:"known"`
	Spans     []Span `json:"spans"`
}

// Arrivals measures what arrives by webhook against the prompts of the
// namespace that holds the project's rows.
type Arrivals struct {
	namespaceOf func(project string) (string, bool)
	prompts     func(namespace string) ([]Said, error)
	measure     func(spans, said []string) ([]quoted.Measurement, error)
	logger      *zap.SugaredLogger
}

// NewArrivals is the trigger on the webhook side of the quote subsystem.
func NewArrivals(namespaceOf func(project string) (string, bool),
	prompts func(namespace string) ([]Said, error),
	measure func(spans, said []string) ([]quoted.Measurement, error), logger *zap.SugaredLogger) *Arrivals {
	return &Arrivals{namespaceOf: namespaceOf, prompts: prompts, measure: measure, logger: logger}
}

// Spans is every span inside double quotes in text, in order. A span inside
// an agent's quotation, between the corner brackets, is the agent's words and
// is not a claim about what a person said, so it is left out.
func Spans(text string) []string {
	var spans []string
	var open []rune
	agent := 0
	quoting := false
	for _, r := range text {
		switch {
		case r == '「':
			agent++
		case r == '」':
			if agent > 0 {
				agent--
			}
		case agent > 0:
			continue
		case r == '"' || r == '“' || r == '”':
			if quoting {
				if len(open) > 0 {
					spans = append(spans, string(open))
				}
				open = open[:0]
			}
			quoting = !quoting
		case quoting:
			open = append(open, r)
		}
	}
	return spans
}

// Measure is the Arrival measured: its spans found, the namespace that knows
// the project found, and each span asked of that namespace's prompts.
func (a *Arrivals) Measure(arrival Arrival) (Arrived, error) {
	arrived := Arrived{Project: arrival.Project, Ref: arrival.Ref}
	var spans []string
	for _, text := range arrival.Texts {
		spans = append(spans, Spans(text)...)
	}
	if len(spans) == 0 {
		return arrived, nil
	}
	namespace, known := a.namespaceOf(arrival.Project)
	arrived.Namespace, arrived.Known = namespace, known
	if !known {
		for _, span := range spans {
			arrived.Spans = append(arrived.Spans, Span{Span: span, Kind: "unsourced", Corrections: len(span), Length: len(span)})
		}
		return arrived, nil
	}
	said, err := a.prompts(namespace)
	if err != nil {
		return arrived, errors.Wrapf(err, "quote arrival: reading the prompts of %s for %s", namespace, arrival.Project)
	}
	measurements, err := a.measure(spans, Texts(said))
	if err != nil {
		return arrived, errors.Wrapf(err, "quote arrival: measuring %d spans of %s against the prompts of %s", len(spans), arrival.Ref, namespace)
	}
	for i, span := range spans {
		m := measurements[i]
		s := Span{Span: span, Kind: kind(m), Corrections: m.Corrections, Length: m.Length}
		if near, ok := SaidAt(said, m); ok {
			s.Row, s.Who, s.At, s.Session = near.Row, near.Who, near.At, near.Session
		}
		arrived.Spans = append(arrived.Spans, s)
	}
	return arrived, nil
}
