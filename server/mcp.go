package server

// What an MCP client reaches, and on what (ADR-038).

import (
	"context"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/ax"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// mcpSource is what the node writes on an attestation a tool made, so a row
// says which way in it came.
const mcpSource = "mcp"

// askedInAX is one query in the language the CLI asks in.
type askedInAX struct {
	Query string `json:"query" jsonschema:"an ax query: SUBJECTS is PREDICATES of CONTEXTS by ACTORS"`
}

// foundByAX is what the node answers with.
type foundByAX struct {
	Attestations []types.As `json:"attestations"`
}

// saidInAS is one claim in the language the CLI says in.
type saidInAS struct {
	Claim string `json:"claim" jsonschema:"an as claim: SUBJECTS is PREDICATES of CONTEXTS"`
}

// writtenByAS names what was written.
type writtenByAS struct {
	ID string `json:"id"`
}

// HandleMCP answers an MCP client. MCP is a level the OAuth flow issues and
// the mint never does, so what arrives here is an app a person let in rather
// than a token minted by hand.
//
// Stateless: a call is served under the request that carried it. A stateful
// session would dispatch under the context captured when it was opened, and
// every later call would act as whoever opened it.
func (s *QNTXServer) HandleMCP(w http.ResponseWriter, r *http.Request) {
	s.mcpOnce.Do(func() {
		s.mcpHTTP = mcp.NewStreamableHTTPHandler(s.mcpServerFor,
			&mcp.StreamableHTTPOptions{Stateless: true})
	})
	s.mcpHTTP.ServeHTTP(w, r)
}

// mcpServerFor is the server one request is answered by.
//
// The admission is read here and closed over rather than read from the context
// a tool is handed: the SDK detaches that context for a long-running stream,
// and a tool acting as the wrong caller is the one mistake this endpoint must
// not make.
func (s *QNTXServer) mcpServerFor(r *http.Request) *mcp.Server {
	admitted, gated := auth.AdmissionFrom(r.Context())
	server := mcp.NewServer(&mcp.Implementation{Name: "qntx", Version: version.VersionTag}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ax",
		Description: "Ask what this node holds. An ax query reads as a sentence: SUBJECTS is PREDICATES of CONTEXTS by ACTORS.",
	}, s.askingInAX(admitted, gated))
	mcp.AddTool(server, &mcp.Tool{
		Name:        "as",
		Description: "Say something this node will hold. An as claim reads as a sentence: SUBJECTS is PREDICATES of CONTEXTS.",
	}, s.sayingInAS(admitted, gated))
	return server
}

// askingInAX reads the query, narrows it to what the caller may read, and
// answers what the store holds.
func (s *QNTXServer) askingInAX(admitted auth.Admission, gated bool) mcp.ToolHandlerFor[askedInAX, foundByAX] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, asked askedInAX) (*mcp.CallToolResult, foundByAX, error) {
		universe, err := s.universeFor(admitted, gated)
		if err != nil {
			return nil, foundByAX{}, err
		}

		// A warning is a query that parsed with something to say, so the filter
		// stands. Anything else is a query that did not read.
		filter, err := parser.ParseAxCommandWithContext(strings.Fields(asked.Query), 0, parser.ErrorContextPlain)
		if err != nil {
			var warning *parser.ParseWarning
			if !errors.As(err, &warning) {
				return nil, foundByAX{}, errors.Wrapf(err, "the query did not read: %q", asked.Query)
			}
		}
		if filter == nil {
			return nil, foundByAX{}, errors.Newf("the query named nothing to look for: %q", asked.Query)
		}

		// The same narrowing the REST read does: what the READ lines name, and
		// the caller's own rows unless a line said all.
		var narrowAfter []string
		if scope, narrowed := admitted.ReadScope(); narrowed {
			predicates, atTheStore := narrowToScope(filter.Predicates, scope)
			if atTheStore {
				filter.Predicates = predicates
				if len(filter.Predicates) == 0 {
					return nil, foundByAX{Attestations: []types.As{}}, nil
				}
			} else {
				narrowAfter = scope
			}
		}
		if admitted.OwnOnly() {
			filter.Actors = []string{admitted.ActsAs()}
		}

		executor := ax.NewAxExecutor(universe.Queries(), alias.NewResolver(universe.Aliases()))
		answer, err := executor.ExecuteAsk(ctx, *filter)
		if err != nil {
			return nil, foundByAX{}, errors.Wrapf(err, "the query did not run: %q", asked.Query)
		}

		found := answer.Attestations
		if narrowAfter != nil {
			kept := make([]types.As, 0, len(found))
			for _, as := range found {
				if mayReadEvery(narrowAfter, as.Predicates) {
					kept = append(kept, as)
				}
			}
			found = kept
		}
		return nil, foundByAX{Attestations: found}, nil
	}
}

// sayingInAS reads the claim, refuses what the caller may not write, and
// writes it as the caller.
func (s *QNTXServer) sayingInAS(admitted auth.Admission, gated bool) mcp.ToolHandlerFor[saidInAS, writtenByAS] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, said saidInAS) (*mcp.CallToolResult, writtenByAS, error) {
		universe, err := s.universeFor(admitted, gated)
		if err != nil {
			return nil, writtenByAS{}, err
		}

		cmd, err := parser.ParseAsCommand(strings.Fields(said.Claim))
		if err != nil {
			return nil, writtenByAS{}, errors.Wrapf(err, "the claim did not read: %q", said.Claim)
		}

		// An empty list is written down as `_` (types.AsCommand.ToAs), so the
		// gate is asked about what will be written rather than about nothing.
		predicates := cmd.Predicates
		if len(predicates) == 0 {
			predicates = []string{"_"}
		}
		for _, predicate := range predicates {
			if !admitted.MayWrite(predicate) {
				return nil, writtenByAS{}, errors.Newf("%s holding %v may not write %q",
					admitted.LevelName(), admitted.Roles(), predicate)
			}
		}

		// The parser fills an actor in from whoever is running the process,
		// which here is the node. A tool writes as the caller and nobody else.
		cmd.Actors = nil
		switch {
		case admitted.ActsAs() != "":
			cmd.Actors = []string{admitted.ActsAs()}
		case admitted.Identity != "":
			cmd.Actors = []string{admitted.Identity}
		}
		cmd.Source = mcpSource

		written, err := universe.Store().GenerateAndCreateAttestation(ctx, cmd)
		if err != nil {
			return nil, writtenByAS{}, errors.Wrapf(err, "the claim was not written: %q", said.Claim)
		}
		return nil, writtenByAS{ID: written.ID}, nil
	}
}

// mayReadEvery reports whether every predicate on an attestation is one these
// words permit. The question onlyWhatMayBeRead asks of the REST read, asked of
// the shape the executor answers in.
func mayReadEvery(words, predicates []string) bool {
	if len(predicates) == 0 {
		return false
	}
	for _, predicate := range predicates {
		if !auth.Permits(words, predicate) {
			return false
		}
	}
	return true
}
