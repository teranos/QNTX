//go:build qntxwasm

// A prompt glyph's 200 body is its result. A short read used to be discarded,
// which handed the caller half an answer as if it were the whole one.
package watcher

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// shortBody promises more bytes than it sends, then hangs up. io.ReadAll ends
// in ErrUnexpectedEOF with the partial body in hand.
func shortBody(t *testing.T, w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		t.Fatal("test server does not support hijacking")
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		t.Fatalf("hijack failed: %v", err)
	}
	writer := bufio.NewWriter(conn)
	writer.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 400\r\n\r\n")
	writer.WriteString(`{"result":"half an`)
	writer.Flush()
	conn.(*net.TCPConn).SetLinger(0)
	conn.Close()
}

func TestPromptGlyphShortReadIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		shortBody(t, w)
	}))
	defer server.Close()

	engine := NewEngine(nil, nil, server.URL, zap.NewNop().Sugar())

	body, err := engine.executeGlyphPrompt("GLYPH-1", "template", []byte(`{"id":"AS-1"}`))
	if err == nil {
		t.Fatalf("short read reported as success, body %q", string(body))
	}
	if !strings.Contains(err.Error(), "GLYPH-1") {
		t.Fatalf("error does not name the glyph: %v", err)
	}
}
