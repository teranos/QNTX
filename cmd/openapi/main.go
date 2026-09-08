// Command openapi writes the machine-readable half of what the node serves.
//
// Three sources, all code, and none of them written for this tool:
//
//   - server/reach's table. It is what the node serves — a path nothing grants
//     is not served at all — so it is the path list, and it says who reaches
//     each one. Read through reach.Reached, which goes through the same parser
//     the mux does: a document cannot say something the mux does not do.
//
//   - server/routing.go and server/auth. Both offer a handler on a path the
//     same way, so one pass over the source says which Go function answers
//     where.
//
//   - The handler's own doc comment, which is what a route is for, and the
//     http.Method constants its body names, which is which methods it answers.
//
// Nothing is invented. A handler with no prose gets an operation with no
// summary; a handler that names no method gets one operation and a line
// saying it does not discriminate. Request and response bodies are absent
// rather than guessed — a handler reads an anonymous struct and writes a map,
// and a schema derived from that would be a claim nobody checked.
//
// Every operation carries x-qntx-handler, the Go function that answers it, so
// a reader can go and read the code rather than trust this file.
//
// No regex (see CLAUDE.md).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/errors"
)

// Where the document is written. It lives beside the package that serves it,
// because Go embeds a file only from its own directory downward.
const writtenTo = "server/openapi/openapi.json"

func main() {
	root := flag.String("root", ".", "the QNTX checkout to read")
	out := flag.String("out", writtenTo, "where to write the document")
	flag.Parse()

	document, err := build(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	body, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, errors.Wrap(err, "the document did not marshal"))
		os.Exit(1)
	}
	body = append(body, '\n')

	path := filepath.Join(*root, *out)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, errors.Wrapf(err, "failed to make the directory for %s", path))
		os.Exit(1)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, errors.Wrapf(err, "failed to write %s", path))
		os.Exit(1)
	}
	fmt.Printf("%s: %d paths\n", path, len(document.Paths))
}

// Document is an OpenAPI 3.1 document. 3.1 rather than 3.0 because an
// operation there may have no responses, and this tool knows none.
type Document struct {
	OpenAPI string                    `json:"openapi"`
	Info    Info                      `json:"info"`
	Paths   map[string]map[string]*Op `json:"paths"`
}

type Info struct {
	Title string `json:"title"`
	// Version is absent from the written file and filled in at serve time.
	// OpenAPI requires it, so a reader of the file alone sees an empty string
	// rather than a version that was true when somebody last ran the tool.
	Version     string `json:"version"`
	Description string `json:"description"`
}

// Op is one operation. The x-qntx- fields are what QNTX knows and OpenAPI has
// no word for: who reaches the path, and which Go function answers.
type Op struct {
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	Reached     []string `json:"x-qntx-reach"`
	Handler     string   `json:"x-qntx-handler,omitempty"`
	Socket      bool     `json:"x-qntx-websocket,omitempty"`
	// Prefix is a path Go's mux matches by prefix rather than exactly —
	// /api/prose/ answers /api/prose/anything. It is not OpenAPI templating,
	// and saying so is the difference between a path and a family of them.
	Prefix bool `json:"x-qntx-prefix,omitempty"`
}

// unnamed is what an operation says about a path the table names and no
// literal in the source answers: the auth handler's map, or a route built at
// runtime. The path is served; which function answers it is not readable here.
const unnamed = "answered by a handler this document could not name from source: " +
	"the path is offered at runtime rather than as a literal."

// undiscriminating is what an operation says when its handler names no HTTP
// method. The handler answers whatever arrives, so one operation is listed and
// the fact is written down rather than a set of methods being invented.
const undiscriminating = "This handler does not discriminate on method; it answers whatever arrives."

// build reads the three sources and lays them over each other.
func build(root string) (*Document, error) {
	reached, err := reach.Reached()
	if err != nil {
		return nil, errors.Wrap(err, "the reach table did not read")
	}
	offered, err := offeredPaths(filepath.Join(root, "server"))
	if err != nil {
		return nil, err
	}
	// Handlers live in two places: the server package tree, and glyph/handlers,
	// which the canvas routes are offered from.
	answers, err := handlerAnswers(filepath.Join(root, "server"), filepath.Join(root, "glyph"))
	if err != nil {
		return nil, err
	}

	paths := map[string]map[string]*Op{}
	for _, path := range sorted(reached) {
		offer := offered[path]
		answer := answers[offer.Handler]

		op := &Op{
			Summary:     summaryOf(answer.Doc),
			Description: strings.TrimSpace(answer.Doc),
			Reached:     reached[path],
			Handler:     offer.Handler,
			Socket:      offer.Socket,
			Prefix:      strings.HasSuffix(path, "/") && path != "/",
		}
		if offer.Handler == "" {
			op.Description = unnamed
		}

		methods := answer.Methods
		if len(methods) == 0 {
			methods = []string{"get"}
			op.Description = strings.TrimSpace(op.Description + "\n\n" + undiscriminating)
		}

		paths[path] = map[string]*Op{}
		for _, method := range methods {
			// One Op value per method: a reader editing the JSON should not
			// find two operations that are the same object.
			each := *op
			paths[path][method] = &each
		}
	}

	// No version. The tag is the only source of one and it does not exist yet:
	// this file is committed, and the tag is cut on the commit that carries
	// it. Regenerating after tagging would move the tree the tag points at.
	// server/openapi_handler.go fills it in when a node serves the document,
	// which is the first moment there is a build to name.
	return &Document{
		OpenAPI: "3.1.0",
		Info: Info{
			Title: "QNTX",
			Description: "Generated by cmd/openapi from server/reach's table, the routing " +
				"source, and the handlers' own doc comments. Never edited by hand. " +
				"The version is the serving node's, filled in as it answers.",
		},
		Paths: paths,
	}, nil
}

func sorted[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
