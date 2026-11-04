// Package mcpclient is the node as a client of another system's MCP server (ADR-038).
//
// "Think QNTX being a client of MCP in addition to just being the server."
//
// "tell the agent resolsible for it to just use the SDK like Qodo suggests"
//
// An MCP server the node consumes is a binary and its arguments: Start runs it and opens the session,
// and the Conn it answers with lists the server's tools and calls them until Close ends the server.
// The session is the SDK's, the one the node serves MCP with (server/mcp.go).
package mcpclient

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	errors "github.com/teranos/sacred-error"
)

// grace is how long a server is given to end once its input closed, before it is sent SIGTERM and
// then killed. Zero is the SDK's own.
var grace time.Duration

// Server is the MCP server's process: its binary, arguments, directory and environment, whole.
type Server struct {
	Binary string
	Args   []string
	Dir    string
	// Env is everything the server's environment holds. It is handed nothing of the node's own.
	Env []string
	// Stderr is where what the server writes on stderr goes. Nil is the node's own stderr.
	Stderr io.Writer
}

// Client is who the client says it is. Empty is qntx, dev.
type Client struct{ Name, Version string }

// Conn is one MCP server, its session open.
type Conn struct {
	// name is what the server is called in an error.
	name    string
	session *mcp.ClientSession
}

// Start runs the server and opens the session with it. ctx bounds the opening and not the server,
// which runs until Close.
func Start(ctx context.Context, s Server, as Client) (*Conn, error) {
	if s.Binary == "" {
		return nil, errors.New("mcp: a server needs its binary")
	}
	cmd := exec.Command(s.Binary, s.Args...)
	cmd.Dir = s.Dir
	cmd.Env = append([]string{}, s.Env...)
	cmd.Stderr = s.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	return open(ctx, &mcp.CommandTransport{Command: cmd, TerminateDuration: grace}, filepath.Base(s.Binary), as)
}

// open opens the session over a transport. The client offers no capability: the node answers
// nothing a server asks of its client.
func open(ctx context.Context, over mcp.Transport, name string, as Client) (*Conn, error) {
	if as.Name == "" {
		as.Name = "qntx"
	}
	if as.Version == "" {
		as.Version = "dev"
	}
	client := mcp.NewClient(
		&mcp.Implementation{Name: as.Name, Version: as.Version},
		&mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}},
	)
	session, err := client.Connect(ctx, over, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "the session with %s did not open", name)
	}
	return &Conn{name: name, session: session}, nil
}

// Tools is every tool the server lists, each page of them.
func (c *Conn) Tools(ctx context.Context) ([]*mcp.Tool, error) {
	var tools []*mcp.Tool
	for tool, err := range c.session.Tools(ctx, nil) {
		if err != nil {
			return nil, errors.Wrapf(err, "%s did not list its tools", c.name)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// Call calls one tool with its arguments, which marshal to a JSON object; nil sends none. A tool
// saying its own call failed is a result with IsError, in its Content: the server answered.
func (c *Conn) Call(ctx context.Context, name string, arguments any) (*mcp.CallToolResult, error) {
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return nil, errors.Wrapf(err, "%s did not answer %s", c.name, name)
	}
	return result, nil
}

// Close ends the session, and the server Start ran: its input closes, which is its word to end. One
// that runs on is sent SIGTERM, and then killed, and Close says how it ended.
func (c *Conn) Close() error {
	if err := c.session.Close(); err != nil {
		return errors.Wrapf(err, "the session with %s did not close", c.name)
	}
	return nil
}
