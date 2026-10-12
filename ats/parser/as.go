package parser

import (
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/named"
	errors "github.com/teranos/sacred-error"
)

// ParseAsCommand parses CLI arguments into an AsCommand. An attestation that
// says no `by` is made by the node: whoever writes it names who wrote it. One
// that says no `on` is made now. The grammar is read in
// crates/ats/src/parser/as_command.rs, where `on` reads time as ax does.
// Grammar: qntx as SUBJECTS [is PREDICATES] [of CONTEXTS] [by ACTOR] [on DATE]
func ParseAsCommand(args []string) (*types.AsCommand, error) {
	parsed, err := parseAsCommandDispatch(args, timeNow())
	if err != nil {
		return nil, err
	}

	by, err := parsed.By.authors()
	if err != nil {
		return nil, err
	}
	when, err := parsed.When.moment()
	if err != nil {
		return nil, err
	}

	return &types.AsCommand{
		Subjects:   parsed.Subjects,
		Predicates: parsed.Predicates,
		Contexts:   parsed.Contexts,
		By:         by,
		When:       when,
		Attributes: make(map[string]any),
	}, nil
}

// asParsed is the command as the Rust parser returns it.
type asParsed struct {
	Subjects   []string `json:"subjects"`
	Predicates []string `json:"predicates"`
	Contexts   []string `json:"contexts"`
	By         asBy     `json:"by"`
	When       asWhen   `json:"when"`
}

// asBy is who the parser says made it: kind "node", or kind "actors" with the
// actors `by` named.
type asBy struct {
	Kind   string   `json:"kind"`
	Actors []string `json:"actors"`
}

// asWhen is when the parser says it was made: kind "now", or kind "at" with
// the epoch milliseconds `on` named.
type asWhen struct {
	Kind string `json:"kind"`
	Ms   int64  `json:"ms"`
}

func (b asBy) authors() (types.Authors, error) {
	if b.Kind == "node" {
		return types.ByNode{}, nil
	}
	if b.Kind == "actors" {
		said, err := named.ItemsOf("by", b.Actors)
		if err != nil {
			return nil, err
		}
		return types.ActorsNamed{Actors: said}, nil
	}
	return nil, errors.Newf("the as parser says by is %q, which names neither the node nor actors", b.Kind)
}

func (w asWhen) moment() (types.Moment, error) {
	if w.Kind == "now" {
		return types.Now{}, nil
	}
	if w.Kind == "at" {
		return types.At{Time: timeAtMs(w.Ms)}, nil
	}
	return nil, errors.Newf("the as parser says when is %q, which names neither now nor a time", w.Kind)
}
