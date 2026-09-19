package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/errors"

	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
)

// What this node can answer, by the path it would answer on.

// Offering is not serving. server/reach decides what is served, from the lines
// in its table, and this package never holds a mux to put anything on.
func (s *QNTXServer) answer(path string, handler http.HandlerFunc) {
	s.answering[path] = reach.Answering{Handler: handler}
}

// answerSocket is the same for a WebSocket upgrade.
func (s *QNTXServer) answerSocket(path string, handler http.HandlerFunc) {
	s.answering[path] = reach.Answering{Handler: handler, Socket: true}
}

// answerFromSigils is the same for a path sigils are bound to. What answers
// there puts each sigil behind the gate itself, with every line about that
// sigil (overHTTP), so the mux is told not to gate the path with its own line
// alone.
func (s *QNTXServer) answerFromSigils(path string, handler http.HandlerFunc) {
	s.answering[path] = reach.Answering{Handler: handler, Gates: true}
}

// wrapping is everything a request passes on the way in besides the gate.
func (s *QNTXServer) wrapping() reach.Wrapping {
	return reach.Wrapping{
		Gate: s.gate,
		Anyone: func(h http.HandlerFunc) http.HandlerFunc {
			return s.accessLog(s.rateLimitPublicMiddleware(s.corsMiddleware(h)))
		},
		Asked: func(h http.HandlerFunc) http.HandlerFunc {
			return s.accessLog(s.corsMiddleware(s.rateLimitMiddleware(h)))
		},
		Upgraded: func(h http.HandlerFunc) http.HandlerFunc {
			return s.accessLog(s.corsMiddleware(s.rateLimitWSMiddleware(h)))
		},
	}
}

// pluginRoute is whether a path is a loaded plugin's: /api/{name},
// /api/{name}/{path...}, /ws/{name}, or one literal path under /api/{name}/.
// The only paths a runtime line may open to a level.
func (s *QNTXServer) pluginRoute(path string) bool {
	var name string
	switch {
	case strings.HasPrefix(path, "/ws/"):
		name = strings.TrimPrefix(path, "/ws/")
		if strings.Contains(name, "/") {
			return false
		}
	case strings.HasPrefix(path, "/api/"):
		var under string
		name, under, _ = strings.Cut(strings.TrimPrefix(path, "/api/"), "/")
		if under != "{path...}" && strings.ContainsAny(under, "{} ") {
			return false
		}
	default:
		return false
	}
	if name == "" {
		return false
	}
	_, offered := s.pluginRoutes.Load(name)
	return offered
}

// pluginPathBody bounds what may be sent to a plugin path a line named. Such a
// path may be open to anybody, and open to anybody is not open to any size.
const pluginPathBody = 1 << 20

// answerLinedPluginPaths answers every plugin path a runtime line names and
// nothing answers yet, by handing it to the plugin like any plugin request.
// Without it a line naming one would stop the node from serving at all.
func (s *QNTXServer) answerLinedPluginPaths(runtime reach.Runtime) {
	for _, line := range runtime.Lines {
		for _, path := range line.Paths {
			if _, answered := s.answering[path]; answered || !s.pluginRoute(path) {
				continue
			}
			s.answer(path, s.boundedPluginRequest)
		}
	}
}

// openToStrangers is whether the lines opened a path to somebody the node
// does not otherwise know: anyone at all, or anybody who registered.
func (s *QNTXServer) openToStrangers(path string) bool {
	if s.served == nil {
		return false
	}
	reaching, anyone := s.served.Reaching(path)
	return anyone || slices.Contains(reaching.Beyond(), auth.LevelPublicRegistration)
}

// boundedPluginRequest holds a stranger to the floor, reads the body once,
// bounded, and hands it on.
func (s *QNTXServer) boundedPluginRequest(w http.ResponseWriter, r *http.Request) {
	// "yes, per caller": each caller has their own second on each path, so a
	// flood spends only the flooder's, and refusing it reads no body.
	if s.rlOpened != nil && s.openToStrangers(r.Pattern) && !s.rlOpened.allow(clientIP(r)+" "+r.Pattern) {
		measure.Count(measure.OpenedRefused, 1, measure.String(measure.AttrRoute, r.Pattern))
		denyRateLimit(w)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, pluginPathBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("%s takes at most %d bytes", r.URL.Path, pluginPathBody))
			return
		}
		writeError(w, http.StatusBadRequest, fmt.Sprintf("the body of %s did not read: %v", r.URL.Path, err))
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	s.handlePluginRequest(w, r)
}

// reopen serves again from the table and the store's lines, whole: every
// runtime reach line and every plugin coming or going arrives here.
func (s *QNTXServer) reopen() ([]string, error) {
	runtime := s.runtime()
	s.answerLinedPluginPaths(runtime)
	return s.served.Reopen(s.answering, s.wrapping(), runtime)
}

// Unspoken is every handler this build carries that no line grants reach to.
// They are ROOT's and nobody else's, which is what not being defined means.
func (s *QNTXServer) Unspoken() []string {
	return append([]string(nil), s.unnamed...)
}

// gate is the auth middleware, or nothing when the deployment runs without
// auth. am.toml requires auth whenever bind_address is not loopback.
func (s *QNTXServer) gate(path string, reaching auth.Reach, handler http.HandlerFunc) http.HandlerFunc {
	if !s.authEnabled || s.authHandler == nil {
		return handler
	}
	return s.authHandler.Middleware(path, reaching, handler)
}
