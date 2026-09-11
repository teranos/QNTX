package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A namespace is its own universe and nothing crosses (ADR-026). A message
// pushed down a socket crosses as surely as a query does: a page told that
// something happened has learned it happened, whatever the payload says.
//
// So a broadcast either names the namespace it is about, or it is about the
// node — its daemon, its plugins, its spend, its own liveness — and is the same
// fact for every reader. Which of the two is a claim somebody makes here.

// nodeWideBroadcasts is every function that sends to every connected client,
// and why what it sends is the node's rather than a namespace's.
var nodeWideBroadcasts = map[string]string{
	"broadcastUsageUpdate": "spend on AI models is the node's bill, metered per process " +
		"and not per universe",
	"broadcastDaemonStatus": "the async daemon is one pool of workers for the node, and " +
		"its load and budget are what that pool is doing",
	"broadcastJobUpdate": "the async job queue is the node's, one queue however many " +
		"namespaces it runs",
	"broadcastLLMStream": "an LLM stream is addressed by job id, and the job it belongs " +
		"to came off the node's own queue",
	"BroadcastPluginHealth": "plugins are loaded once however many namespaces the node " +
		"runs, so one going unhealthy is the same fact for every reader",

	// The four Pulse ones are the same claim: an execution is a run of the
	// node's scheduler, reported by scheduled-job id.
	"BroadcastPulseExecutionStarted":   "a Pulse execution is a run of the node's scheduler",
	"BroadcastPulseExecutionFailed":    "a Pulse execution is a run of the node's scheduler",
	"BroadcastPulseExecutionCompleted": "a Pulse execution is a run of the node's scheduler",
	"BroadcastPulseExecutionLogStream": "a Pulse execution is a run of the node's scheduler",
}

// A broadcast that reaches everybody and says nothing about why fails this
// test. That is the whole of how the boundary stays true when a new message is
// added: it names a namespace, or somebody says out loud that it is the node's.
func TestEveryBroadcastSaysWhereItGoes(t *testing.T) {
	found, err := nodeWideBroadcastersIn(".")
	require.NoError(t, err)

	var named []string
	for name := range nodeWideBroadcasts {
		named = append(named, name)
	}
	sort.Strings(named)
	sort.Strings(found)

	assert.Equal(t, named, found,
		"a function sends to every connected client and nothing says why that is the "+
			"node's rather than one namespace's; say so in nodeWideBroadcasts, or send it "+
			"with broadcastIn so it reaches the namespace it came from")
}

// theHelpers are the three functions the check is about rather than a subject
// of: two of them are how a message is addressed, and one is the queue.
var theHelpers = map[string]bool{
	"broadcastMessage": true,
	"broadcastIn":      true,
	"queueBroadcast":   true,
}

// nodeWideBroadcastersIn is every function in the package at dir that sends to
// every client: by calling broadcastMessage, or by building a request that
// names neither a namespace nor one client.
func nodeWideBroadcastersIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	seen := map[string]bool{}
	var senders []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || theHelpers[fn.Name.Name] || seen[fn.Name.Name] {
				continue
			}
			if !sendsToEveryone(fn.Body) {
				continue
			}
			seen[fn.Name.Name] = true
			senders = append(senders, fn.Name.Name)
		}
	}
	return senders, nil
}

// sendsToEveryone reports whether a body reaches every client: a call to
// broadcastMessage, or a broadcastRequest naming neither in nor clientID.
func sendsToEveryone(body *ast.BlockStmt) bool {
	everyone := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if to, ok := node.Fun.(*ast.SelectorExpr); ok && to.Sel.Name == "broadcastMessage" {
				everyone = true
				return false
			}
		case *ast.CompositeLit:
			named, ok := node.Type.(*ast.Ident)
			if !ok || named.Name != "broadcastRequest" {
				return true
			}
			if !addressed(node) {
				everyone = true
				return false
			}
		}
		return true
	})
	return everyone
}

// addressed reports whether a request names who it is for: one namespace, one
// client, or a close, which is about a connection the worker already holds.
func addressed(lit *ast.CompositeLit) bool {
	for _, field := range lit.Elts {
		kv, ok := field.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "in", "clientID", "client":
			return true
		}
	}
	return false
}
