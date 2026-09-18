package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The document is generated, so the committed one is either what the table
// says now or it is a lie about what the node serves. Adding a line and not
// running `make openapi` fails here rather than shipping a document that
// leaves it out.
func TestTheWrittenDocumentIsWhatTheTableSays(t *testing.T) {
	root := filepath.Join("..", "..")

	document, err := build()
	require.NoError(t, err)
	fresh, err := json.MarshalIndent(document, "", "  ")
	require.NoError(t, err)
	fresh = append(fresh, '\n')

	written, err := os.ReadFile(filepath.Join(root, writtenTo))
	require.NoError(t, err, "the document has never been generated; run make openapi")

	assert.Equal(t, string(fresh), string(written),
		"%s is not what the table says; run make openapi", writtenTo)
}

// Every path in the document says who reaches it. The table is what the node
// serves, and a path nothing grants is not served.
func TestEveryPathSaysWhoReachesIt(t *testing.T) {
	document, err := build()
	require.NoError(t, err)

	for path, item := range document.Paths {
		assert.NotEmpty(t, item.Reached, "%s says nobody reaches it", path)
	}
}
