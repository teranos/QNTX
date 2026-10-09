package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/parity"
	"github.com/teranos/QNTX/server/sigil"
)

// "parity the sigil is what an Agent should deal with through MCP."
//
// "A way to keep Agents honest, of course": what a signum declares it follows,
// held to the reference as the node pins it.

// everySignumFollows is what every signum follows by its shape alone, declared
// once and not signum by signum: "On the Agent Card a signum is described as
// an AgentSkill" (sigil.proto),
// and to MCP "a tool is one sigil" (mcp.go).
func everySignumFollows() []*protocol.Follows {
	return []*protocol.Follows{
		{Reference: "a2a", Columns: []*protocol.Corresponds{
			// "there cant be two signa of the same name"
			{Field: "protocol.Signum.name", Column: "AgentSkill.id"},
			{Field: "protocol.Signum.name", Column: "AgentSkill.name"},
			{Field: "protocol.Signum.description", Column: "AgentSkill.description"},
			{Field: "protocol.Signum.tags", Column: "AgentSkill.tags"},
		}},
		{Reference: "mcp", Columns: []*protocol.Corresponds{
			{Field: "protocol.Signum.name", Column: "Tool.name"},
			{Field: "protocol.Sigil.name", Column: "Tool.name"},
			{Field: "protocol.Signum.name", Column: "Tool.title"},
			{Field: "protocol.Sigil.name", Column: "Tool.title"},
			{Field: "protocol.Sigil.does", Column: "Tool.description"},
			{Field: "protocol.Sigil.http", Column: "Tool.annotations"},
		}, Folds: []*protocol.Fold{
			// A property per param and per field, by its name (signa.go).
			{Field: "protocol.Sigil.takes", Column: "Tool.inputSchema", Key: "name"},
			{Field: "protocol.Sigil.gives", Column: "Tool.outputSchema", Key: "name"},
		}},
	}
}

func (s *QNTXServer) paritySignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "parity",
			Sigils: []*protocol.Sigil{
				{
					Name: "hold",
					Does: "Hold a signum, or one sigil of it, to a reference it follows: per model of the reference a score from 0 to 100, per column what follows it and how it departs, and what the signum carries that follows nothing.",
					Takes: []*protocol.Param{
						{Name: "signum", Required: true, Says: "The signum to hold, by name."},
						{Name: "sigil", Says: "One sigil of it, held by the messages it gives. Naming none holds the whole signum."},
						{Name: "reference", Says: "The reference, by name. Naming none is the one the signum follows, when it follows one. a2a holds any signum by its shape, as a skill, and mcp each of its sigils, as a tool."},
					},
					Gives: []*protocol.Field{
						{Name: "signum", Says: "The signum that was held."},
						{Name: "sigil", Says: "The sigil that was held, or empty for the whole signum."},
						{Name: "reference", Says: "The reference it was held to."},
						{Name: "clades", Says: "One per model of the reference, in its order: what the reference says of it, its score, and per column what the reference says of it and where that was read when not from its schema, whether it requires it, the fields that follow it and how they depart."},
						{Name: "unfollowed", Says: "Per message in scope, its fields that follow no column."},
						{Name: "missing", Says: "What the signum follows into a column the reference does not have."},
						{Name: "required", Says: "Of the models anything follows, each column the reference requires and nothing follows."},
						{Name: "ours", Says: "What each message in scope and each of its fields says of itself, in its own .proto's words, by full name."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/parity/hold"},
				},
				{
					Name: "storage",
					Does: "For every thing QNTX persists, whether SQLite and DuckDB each hold it, as make parity read the source this build was made from. It says nothing of this node's own stores.",
					Gives: []*protocol.Field{
						{Name: "describes", Says: "What was read: source, the code this build was made from, and never this node."},
						{Name: "things", Says: "One per thing, by name: sqlite, duckdb, rebuilt by a take-in, and the Go files that reach it with SQL written by hand."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/parity/storage"},
				},
				{
					Name: "follows",
					Does: "Every signum the node serves, and each reference it can be held to: the ones it declares it follows, and the ones every signum follows by its shape.",
					Gives: []*protocol.Field{
						{Name: "signum", Says: "The signum, by name."},
						{Name: "declares", Says: "The references it declares it follows."},
						{Name: "by_shape", Says: "The references every signum follows by its shape."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/parity/follows"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"hold": s.parityHold, "storage": s.parityStorage, "follows": s.parityFollows},
	}
}

func (s *QNTXServer) parityHold(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	var held *protocol.Signum
	var names []string
	for _, signum := range s.signa() {
		names = append(names, signum.GetName())
		if signum.GetName() == sent["signum"] {
			held = signum.Signum
		}
	}
	if held == nil {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "signum",
			Says: "no signum " + sent["signum"] + "; the node holds " + strings.Join(names, ", ")}
	}

	reference := sent["reference"]
	if reference == "" {
		var followed []string
		for _, f := range held.GetFollows() {
			followed = append(followed, f.GetReference())
		}
		switch len(followed) {
		case 0:
			return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "signum",
				Says: held.GetName() + " follows nothing of its own; a2a and mcp hold it by its shape, when named"}
		case 1:
			reference = followed[0]
		default:
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "reference",
				Says: held.GetName() + " follows " + strings.Join(followed, ", ") + ", so the reference is to be named"}
		}
	}

	schema, refused := parity.Reference(reference)
	if refused != nil {
		return nil, refused
	}
	declared := &protocol.Signum{Name: held.GetName(), Sigils: held.GetSigils(),
		Follows: byReference(append(append([]*protocol.Follows{}, held.GetFollows()...), everySignumFollows()...))}
	p, refused := parity.Hold(declared, sent["sigil"], reference, schema)
	if refused != nil {
		return nil, refused
	}
	return p, nil
}

// byReference is one Follows per reference: what a signum declares of a
// reference and what every signum follows of it by its shape are held together.
func byReference(follows []*protocol.Follows) []*protocol.Follows {
	var merged []*protocol.Follows
	at := map[string]*protocol.Follows{}
	for _, f := range follows {
		held, ok := at[f.GetReference()]
		if !ok {
			held = &protocol.Follows{Reference: f.GetReference()}
			at[f.GetReference()] = held
			merged = append(merged, held)
		}
		held.Columns = append(held.Columns, f.GetColumns()...)
		held.Folds = append(held.Folds, f.GetFolds()...)
	}
	return merged
}

// parityFollows is what the parity window offers to hold: every signum, with
// what it declares it follows and what every signum follows by its shape.
func (s *QNTXServer) parityFollows(context.Context, sigil.Sent) (any, *protocol.Refusal) {
	var byShape []string
	for _, f := range everySignumFollows() {
		byShape = append(byShape, f.GetReference())
	}
	rows := []map[string]any{}
	for _, signum := range s.signa() {
		declares := []string{}
		for _, f := range signum.GetFollows() {
			declares = append(declares, f.GetReference())
		}
		rows = append(rows, map[string]any{"signum": signum.GetName(), "declares": declares, "by_shape": byShape})
	}
	return rows, nil
}

func (s *QNTXServer) parityStorage(context.Context, sigil.Sent) (any, *protocol.Refusal) {
	things, err := parity.Storage()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return map[string]any{"describes": "source", "things": things}, nil
}
