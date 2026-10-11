package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/teranos/QNTX/internal/measure"
	errors "github.com/teranos/sacred-error"

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

// answerSigils answers every path a sigil is bound to now, and no longer the
// ones a sigil was bound to before and is not: a plugin that restarts may
// hand different signa.
func (s *QNTXServer) answerSigils() {
	for path := range s.sigilPaths {
		delete(s.answering, path)
	}
	s.sigilPaths = map[string]bool{}
	for path, answered := range s.answeredFromSigils() {
		s.answerFromSigils(path, answered)
		s.sigilPaths[path] = true
	}
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

// pluginRoute is whether a name is a loaded plugin's, the only names a
// runtime line may open to a level: a path, or a sigil by its reach name.
func (s *QNTXServer) pluginRoute(path string) bool {
	var name string
	switch {
	// {name} or {name}:{sigil}, either after http: or mcp:.
	case !strings.HasPrefix(path, "/"):
		named := path
		for _, surface := range []string{reach.OverHTTP + ":", reach.OverMCP + ":"} {
			named = strings.TrimPrefix(named, surface)
		}
		name, _, _ = strings.Cut(named, ":")
		if strings.ContainsAny(name, "/{} ") {
			return false
		}
	// /api/{name}, /api/{name}/{path...}, /ws/{name}, or one literal path under /api/{name}/.
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
	s.opening.Lock()
	defer s.opening.Unlock()
	return s.reopenHeld()
}

// reopenHeld is reopen for a caller already holding s.opening, because it
// changed what the node answers first.
func (s *QNTXServer) reopenHeld() ([]string, error) {
	runtime := s.runtime()
	s.answerLinedPluginPaths(runtime)
	unnamed, err := s.served.Reopen(s.answering, s.wrapping(), runtime)
	if err != nil {
		return nil, err
	}
	s.sayUnanswered()
	return unnamed, nil
}

// Unspoken is every handler this build carries that no line grants reach to.
// They are ROOT's and nobody else's, which is what not being defined means.
func (s *QNTXServer) Unspoken() []string {
	return append([]string(nil), s.unnamed...)
}

// gate is the auth middleware. A node running without auth has one caller,
// and the gate says that caller is ROOT rather than saying nothing. am.toml
// requires auth whenever bind_address is not loopback.
func (s *QNTXServer) gate(path string, reaching auth.Reach, handler http.HandlerFunc) http.HandlerFunc {
	if !s.authEnabled || s.authHandler == nil {
		return func(w http.ResponseWriter, r *http.Request) {
			handler(w, r.WithContext(auth.WithAdmission(r.Context(), auth.Admitted(auth.LevelRoot))))
		}
	}
	return s.authHandler.Middleware(path, reaching, handler)
}
