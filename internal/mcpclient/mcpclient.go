// Package mcpclient is the node as a client of another system's MCP server (ADR-038).
//
// "Think QNTX being a client of MCP in addition to just being the server."
//
// "i think for Obsidian MCP, we want to write our own client."
//
// An MCP server the node consumes is a binary and its arguments: Start runs it, opens the session, and
// the Conn it answers with lists the server's tools and calls them until Close ends the server. Every
// message is written and read here; the SDK the node serves MCP with (server/mcp.go) is not asked.
package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	errors "github.com/teranos/sacred-error"
)

// speaks is each MCP revision whose session opens with initialize and initialized, newest first. The
// first is the one offered, and a server answering with any other of them is spoken to in that one.
var speaks = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// maxFrame is the longest line a server may write: the SDK's own bound for one frame.
const maxFrame = 16 * 1024 * 1024

// grace is how long a server is given to end at each step of Close, and to say how it ended.
var grace = time.Second

// methodNotFound is JSON-RPC's code for a method the receiver does not have.
const methodNotFound = -32601

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

// Peer is the server as it answered initialize.
type Peer struct {
	Name, Title, Version string
	// Protocol is the MCP revision the session is spoken in.
	Protocol string
	// Instructions is what the server says to whoever uses it.
	Instructions string
	// Capabilities is each capability the server offers, by name, as it wrote it.
	Capabilities map[string]json.RawMessage
}

// Offers is whether the server said it has a capability: tools, resources, prompts.
func (p Peer) Offers(capability string) bool {
	_, offered := p.Capabilities[capability]
	return offered
}

// Tool is one tool as the server lists it. Raw is the tool whole.
type Tool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
	Annotations  json.RawMessage `json:"annotations"`
	Raw          json.RawMessage `json:"-"`
}

// Content is one piece of what a tool answered. Text is a text piece's text, and Raw the piece whole.
type Content struct {
	Type string          `json:"type"`
	Text string          `json:"text"`
	Raw  json.RawMessage `json:"-"`
}

// Result is what a tool answered. IsError is the tool saying its own call failed, in Content: the
// server answered, so it is a result and no error.
type Result struct {
	Content    []Content
	Structured json.RawMessage
	IsError    bool
	// Raw is the result whole.
	Raw json.RawMessage
}

// Error is a JSON-RPC error the server answered a request with.
type Error struct {
	Method        string
	Code          int
	Message, Data string
}

func (e *Error) Error() string {
	s := e.Method + " was answered with error " + strconv.Itoa(e.Code) + ": " + e.Message
	if e.Data != "" {
		s += " " + e.Data
	}
	return s
}

// message is one JSON-RPC message, read. A request has a method and an id, a notification a method
// alone, and an answer an id with a result or an error.
type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

// Conn is one MCP server, its session open.
type Conn struct {
	to   io.WriteCloser
	from io.ReadCloser
	// name is what the server is called in an error before it said its own name.
	name string
	peer Peer

	// The server's process, when Start ran one: exited closes once it ended, with how in waitErr.
	process *exec.Cmd
	exited  chan struct{}
	waitErr error

	writing sync.Mutex
	mu      sync.Mutex
	next    int64
	waiting map[int64]chan message
	// unsaid is the first answer to the server that could not be written.
	unsaid error

	// ended closes when the server's output ended, with why in endErr when it was no plain end.
	ended  chan struct{}
	endErr error

	closing  sync.Once
	closeErr error
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
	cmd.WaitDelay = grace
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.Wrapf(err, "could not write to %s", s.Binary)
	}
	// The reading end is the client's own, so the server's exit is waited on while it is still read.
	out, wrote, err := os.Pipe()
	if err != nil {
		return nil, errors.WithSecondaryError(errors.Wrapf(err, "could not read %s", s.Binary), in.Close())
	}
	cmd.Stdout = wrote
	if err := cmd.Start(); err != nil {
		return nil, errors.WithSecondaryError(errors.Wrapf(err, "could not start %s", s.Binary), both(out.Close(), wrote.Close()))
	}
	c := newConn(out, in, filepath.Base(s.Binary))
	c.process, c.exited = cmd, make(chan struct{})
	go func() {
		c.waitErr = cmd.Wait()
		close(c.exited)
	}()
	// The writing end is the server's alone from here, so its output ends when it does.
	if err := wrote.Close(); err != nil {
		return nil, errors.WithSecondaryError(errors.Wrapf(err, "could not leave %s its output", s.Binary), c.Close())
	}
	if err := c.open(ctx, as); err != nil {
		return nil, errors.WithSecondaryError(err, c.Close())
	}
	return c, nil
}

// Over opens the session with a server already running: what it writes is read from from, and what it
// reads is written to to, one JSON-RPC message a line. Close closes both.
func Over(ctx context.Context, from io.ReadCloser, to io.WriteCloser, as Client) (*Conn, error) {
	c := newConn(from, to, "the server")
	if err := c.open(ctx, as); err != nil {
		return nil, errors.WithSecondaryError(err, c.Close())
	}
	return c, nil
}

func newConn(from io.ReadCloser, to io.WriteCloser, name string) *Conn {
	return &Conn{from: from, to: to, name: name, waiting: map[int64]chan message{}, ended: make(chan struct{})}
}

// open is the lifecycle's start: initialize, the revision agreed, and initialized.
func (c *Conn) open(ctx context.Context, as Client) error {
	go c.read()
	if as.Name == "" {
		as.Name = "qntx"
	}
	if as.Version == "" {
		as.Version = "dev"
	}
	raw, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": speaks[0],
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": as.Name, "version": as.Version},
	})
	if err != nil {
		return err
	}
	var said struct {
		ProtocolVersion string                     `json:"protocolVersion"`
		Capabilities    map[string]json.RawMessage `json:"capabilities"`
		Instructions    string                     `json:"instructions"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &said); err != nil {
		return errors.Wrapf(err, "%s answered initialize with what does not read: %s", c.name, raw)
	}
	if said.ServerInfo.Name != "" {
		c.name = said.ServerInfo.Name
	}
	// A revision this client does not speak ends the session before anything else is said in it.
	if !slices.Contains(speaks, said.ProtocolVersion) {
		return errors.Newf("%s speaks MCP %q, and this client speaks %s", c.name, said.ProtocolVersion, strings.Join(speaks, ", "))
	}
	c.peer = Peer{
		Name:         said.ServerInfo.Name,
		Title:        said.ServerInfo.Title,
		Version:      said.ServerInfo.Version,
		Protocol:     said.ProtocolVersion,
		Instructions: said.Instructions,
		Capabilities: said.Capabilities,
	}
	if err := c.send(ctx, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		return errors.Wrapf(err, "%s could not be told the session is open", c.name)
	}
	return nil
}

// Peer is the server as it answered initialize.
func (c *Conn) Peer() Peer { return c.peer }

// Tools is every tool the server lists, each page of them.
func (c *Conn) Tools(ctx context.Context) ([]Tool, error) {
	if !c.peer.Offers("tools") {
		return nil, errors.Newf("%s offers no tools", c.name)
	}
	var tools []Tool
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var page struct {
			Tools      []json.RawMessage `json:"tools"`
			NextCursor string            `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, errors.Wrapf(err, "%s answered tools/list with what does not read: %s", c.name, raw)
		}
		for _, listed := range page.Tools {
			tool := Tool{Raw: listed}
			if err := json.Unmarshal(listed, &tool); err != nil {
				return nil, errors.Wrapf(err, "%s lists a tool that does not read: %s", c.name, listed)
			}
			tools = append(tools, tool)
		}
		if page.NextCursor == "" {
			return tools, nil
		}
		// The same page named as the next one would be asked for without end.
		if page.NextCursor == cursor {
			return nil, errors.Newf("%s names %q as the page after %q", c.name, page.NextCursor, cursor)
		}
		cursor = page.NextCursor
	}
}

// Call calls one tool with its arguments, which marshal to a JSON object; nil sends none.
func (c *Conn) Call(ctx context.Context, name string, arguments any) (Result, error) {
	if !c.peer.Offers("tools") {
		return Result{}, errors.Newf("%s offers no tools, so %s cannot be called", c.name, name)
	}
	params := map[string]any{"name": name}
	if arguments != nil {
		params["arguments"] = arguments
	}
	raw, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return Result{}, err
	}
	var answered struct {
		Content           []json.RawMessage `json:"content"`
		StructuredContent json.RawMessage   `json:"structuredContent"`
		IsError           bool              `json:"isError"`
	}
	if err := json.Unmarshal(raw, &answered); err != nil {
		return Result{}, errors.Wrapf(err, "%s answered %s with what does not read: %s", c.name, name, raw)
	}
	result := Result{Structured: answered.StructuredContent, IsError: answered.IsError, Raw: raw}
	for _, piece := range answered.Content {
		content := Content{Raw: piece}
		if err := json.Unmarshal(piece, &content); err != nil {
			return Result{}, errors.Wrapf(err, "%s answered %s with content that does not read: %s", c.name, name, piece)
		}
		result.Content = append(result.Content, content)
	}
	return result, nil
}

// call asks one request and waits for its answer, the server's end, or ctx.
func (c *Conn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	answer := make(chan message, 1)
	c.waiting[id] = answer
	c.mu.Unlock()
	forget := func() {
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
	}
	read := func(m message) (json.RawMessage, error) {
		if m.Error != nil {
			return nil, errors.WithStack(&Error{Method: method, Code: m.Error.Code, Message: m.Error.Message, Data: string(m.Error.Data)})
		}
		return m.Result, nil
	}

	// A request ctx ended the writing of may still reach the server, and is cancelled below.
	if err := c.send(ctx, map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil && ctx.Err() == nil {
		forget()
		err = errors.Wrapf(err, "%s could not be asked of %s", method, c.name)
		// A server that is gone cannot be written to, and why it went is the failure.
		select {
		case <-c.ended:
			return nil, errors.WithSecondaryError(errors.Wrapf(c.why(), "%s was not answered", method), err)
		case <-time.After(grace):
			return nil, err
		}
	}
	select {
	case m := <-answer:
		return read(m)
	case <-c.ended:
		forget()
		// The answer may have been the last thing the server wrote.
		select {
		case m := <-answer:
			return read(m)
		default:
		}
		return nil, errors.Wrapf(c.why(), "%s was not answered", method)
	case <-ctx.Done():
		forget()
		err := errors.Wrapf(ctx.Err(), "%s was not answered by %s", method, c.name)
		// The server is told to stop working on it. initialize is the one request never cancelled.
		if method == "initialize" {
			return nil, err
		}
		told := c.send(context.Background(), map[string]any{
			"jsonrpc": "2.0",
			"method":  "notifications/cancelled",
			"params":  map[string]any{"requestId": id, "reason": ctx.Err().Error()},
		})
		return nil, errors.WithSecondaryError(err, told)
	}
}

// send writes one message as one line. A message is written whole or not at all: when ctx ends first
// the line still goes out behind it, so the server never reads half of one.
func (c *Conn) send(ctx context.Context, v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return errors.Wrap(err, "the message does not marshal")
	}
	line = append(line, '\n')
	wrote := make(chan error, 1)
	go func() {
		c.writing.Lock()
		defer c.writing.Unlock()
		_, err := c.to.Write(line)
		wrote <- err
	}()
	select {
	case err := <-wrote:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// read reads the server's output until it ends: each answer to whoever asked, each request answered.
func (c *Conn) read() {
	defer close(c.ended)
	lines := bufio.NewScanner(c.from)
	lines.Buffer(make([]byte, 64*1024), maxFrame)
	for lines.Scan() {
		line := bytes.TrimSpace(lines.Bytes())
		if len(line) == 0 {
			continue
		}
		var m message
		if err := json.Unmarshal(line, &m); err != nil {
			c.endErr = errors.Wrapf(err, "%s wrote a line that is no JSON-RPC message: %s", c.name, line)
			return
		}
		switch {
		case m.Method != "" && len(m.ID) > 0:
			c.asked(m)
		case m.Method != "":
			// A notification. This client asked for none, and nothing here listens for one.
		default:
			id, err := strconv.ParseInt(string(m.ID), 10, 64)
			if err != nil {
				// An answer to nothing that was asked: the server could not read a request.
				c.endErr = errors.Newf("%s answered a request it does not name: %s", c.name, line)
				return
			}
			c.mu.Lock()
			answer := c.waiting[id]
			delete(c.waiting, id)
			c.mu.Unlock()
			// Nobody waits on a request that was cancelled, and its late answer is let go.
			if answer != nil {
				answer <- m
			}
		}
	}
	c.endErr = lines.Err()
}

// asked answers a request the server made of the client. ping is answered, and anything else is a
// method the client does not have: it offered no capability, so no other request is the server's to make.
func (c *Conn) asked(m message) {
	answer := map[string]any{"jsonrpc": "2.0", "id": m.ID}
	if m.Method == "ping" {
		answer["result"] = map[string]any{}
	} else {
		answer["error"] = map[string]any{"code": methodNotFound, "message": "this client does not have " + m.Method}
	}
	method := m.Method
	// Written apart from the reading, so a server waiting to be read before it reads is not waited on.
	go func() {
		if err := c.send(context.Background(), answer); err != nil {
			c.mu.Lock()
			if c.unsaid == nil {
				c.unsaid = errors.Wrapf(err, "%s could not be answered its %s", c.name, method)
			}
			c.mu.Unlock()
		}
	}()
}

// why is why the server's output ended, once it has.
func (c *Conn) why() error {
	c.mu.Lock()
	unsaid := c.unsaid
	c.mu.Unlock()
	if c.endErr != nil {
		return errors.WithSecondaryError(c.endErr, unsaid)
	}
	if c.process == nil {
		return errors.WithSecondaryError(errors.Newf("%s closed its side", c.name), unsaid)
	}
	select {
	case <-c.exited:
		if c.waitErr != nil {
			return errors.WithSecondaryError(errors.Wrapf(c.waitErr, "%s ended", c.name), unsaid)
		}
		return errors.WithSecondaryError(errors.Newf("%s ended", c.name), unsaid)
	case <-time.After(grace):
		return errors.WithSecondaryError(errors.Newf("%s closed its output and runs on", c.name), unsaid)
	}
}

// Close ends the session, and the server Start ran: its input closes, which is its word to end. One
// that runs on is sent SIGTERM, and then killed, and Close says so: an end that was forced is an error.
func (c *Conn) Close() error {
	c.closing.Do(func() { c.closeErr = c.close() })
	return c.closeErr
}

func (c *Conn) close() error {
	// Closed apart from the lock, which a write the server never read still holds.
	closed := c.to.Close()
	if c.process == nil {
		err := both(closed, c.from.Close())
		<-c.ended
		return err
	}
	waited := func() bool {
		select {
		case <-c.exited:
			return true
		case <-time.After(grace):
			return false
		}
	}
	forced := ""
	if !waited() {
		forced = "SIGTERM"
		if err := c.process.Process.Signal(syscall.SIGTERM); err != nil {
			closed = both(closed, errors.Wrapf(err, "%s could not be sent SIGTERM", c.name))
		}
		if !waited() {
			forced = "SIGKILL"
			if err := c.process.Process.Kill(); err != nil {
				closed = both(closed, errors.Wrapf(err, "%s could not be killed", c.name))
			}
			<-c.exited
		}
	}
	closed = both(closed, c.from.Close())
	<-c.ended
	if forced != "" {
		return errors.WithSecondaryError(errors.Newf("%s ran on after its input closed, and was ended with %s", c.name, forced), closed)
	}
	if c.waitErr != nil {
		return errors.WithSecondaryError(errors.Wrapf(c.waitErr, "%s ended", c.name), closed)
	}
	return closed
}

// both is the first failure carrying the second, or whichever one there is.
func both(first, second error) error {
	if first == nil {
		return second
	}
	return errors.WithSecondaryError(first, second)
}
