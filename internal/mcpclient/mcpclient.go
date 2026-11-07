// 「[Fable 5.1] Package mcpclient is the node as a client of another system's MCP server (ADR-038).」
//
// "Think QNTX being a client of MCP in addition to just being the server."
//
// "tell the agent resolsible for it to just use the SDK like Qodo suggests"
//
// 「[Fable 5.1] An MCP server the node consumes is a binary and its arguments: Start runs it and opens the session,」
// 「[Fable 5.1] and the Conn it answers with lists the server's tools and calls them until Close ends the server.」
// 「[Fable 5.1] The session is the SDK's, the one the node serves MCP with (server/mcp.go).」
package mcpclient

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	errors "github.com/teranos/sacred-error"
)

// 「[Fable 5.1] grace is how long a server is given to end once its input closed, before it is sent SIGTERM and」
// 「[Fable 5.1] then killed. Zero is the SDK's own.」
var grace time.Duration

// 「[Fable 5.1] Server is the MCP server's process: its binary, arguments, directory and environment, whole.」
type Server struct {
	Binary string
	Args   []string
	Dir    string
	// 「[Fable 5.1] Env is everything the server's environment holds. It is handed nothing of the node's own.」
	Env []string
	// 「[Fable 5.1] Stderr is where what the server writes on stderr goes. Nil is the node's own stderr.」
	Stderr io.Writer
}

// 「[Fable 5.1] Client is who the client says it is. Empty is qntx, dev.」
type Client struct{ Name, Version string }

// 「[Fable 5.1] Conn is one MCP server, its session open.」
type Conn struct {
	// 「[Fable 5.1] name is what the server is called in an error.」
	name    string
	session *mcp.ClientSession
}

// 「[Fable 5.1] Start runs the server and opens the session with it. ctx bounds the opening and not the server,」
// 「[Fable 5.1] which runs until Close.」
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

// 「[Opus 5.5] Remote is an MCP server another system serves at a URL, over Streamable HTTP.」
type Remote struct {
	URL string
	// 「[Opus 5.5] HTTPClient is what reaches it. Nil is the SDK's own.」
	HTTPClient *http.Client
	// 「[Opus 5.5] OAuth authorizes each request, and answers a 401 with the authorization flow. Nil sends none.」
	OAuth auth.OAuthHandler
}

// 「[Opus 5.5] Dial opens the session with a remote server. ctx bounds the opening, an authorization」
// 「[Opus 5.5] the server asks for on the way included.」
func Dial(ctx context.Context, r Remote, as Client) (*Conn, error) {
	at, err := url.Parse(r.URL)
	if err != nil || at.Host == "" {
		return nil, errors.Newf("mcp: a remote server needs its URL, and %q is none", r.URL)
	}
	return open(ctx, &mcp.StreamableClientTransport{Endpoint: r.URL, HTTPClient: r.HTTPClient, OAuthHandler: r.OAuth}, at.Host, as)
}

// 「[Fable 5.1] open opens the session over a transport. The client offers no capability: the node answers」
// 「[Fable 5.1] nothing a server asks of its client.」
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

// 「[Fable 5.1] Tools is every tool the server lists, each page of them.」
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

// 「[Fable 5.1] Call calls one tool with its arguments, which marshal to a JSON object; nil sends none. A tool」
// 「[Fable 5.1] saying its own call failed is a result with IsError, in its Content: the server answered.」
func (c *Conn) Call(ctx context.Context, name string, arguments any) (*mcp.CallToolResult, error) {
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return nil, errors.Wrapf(err, "%s did not answer %s", c.name, name)
	}
	return result, nil
}

// 「[Fable 5.1] Close ends the session, and the server Start ran: its input closes, which is its word to end. One」
// 「[Fable 5.1] that runs on is sent SIGTERM, and then killed, and Close says how it ended.」
func (c *Conn) Close() error {
	if err := c.session.Close(); err != nil {
		return errors.Wrapf(err, "the session with %s did not close", c.name)
	}
	return nil
}
