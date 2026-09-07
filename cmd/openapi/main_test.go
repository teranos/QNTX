package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The document is generated, so the committed one is either what the source
// says now or it is a lie about what the node serves. Adding a route and not
// running `make openapi` fails here rather than shipping a document that
// leaves it out.
func TestTheWrittenDocumentIsWhatTheSourceSays(t *testing.T) {
	root := filepath.Join("..", "..")

	document, err := build(root)
	require.NoError(t, err)
	fresh, err := json.MarshalIndent(document, "", "  ")
	require.NoError(t, err)
	fresh = append(fresh, '\n')

	written, err := os.ReadFile(filepath.Join(root, writtenTo))
	require.NoError(t, err, "the document has never been generated; run make openapi")

	assert.Equal(t, string(fresh), string(written),
		"%s is not what the source says; run make openapi", writtenTo)
}

// Every path the table grants is in the document. The table is what the node
// serves, so a path missing here is a route the document does not admit to.
func TestEveryServedPathIsInTheDocument(t *testing.T) {
	document, err := build(filepath.Join("..", ".."))
	require.NoError(t, err)

	for path, operations := range document.Paths {
		assert.NotEmpty(t, operations, "%s has no operation", path)
		for method, op := range operations {
			assert.NotEmpty(t, op.Reached,
				"%s %s says nobody reaches it, and a path nothing grants is not served", method, path)
		}
	}
}
