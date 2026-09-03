package server

import (
	"net/http"

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

// Unspoken is every handler this build carries that no line grants reach to.
// They are compiled and unreachable, which is what not being defined means.
func (s *QNTXServer) Unspoken() []string {
	return append([]string(nil), s.unreachable...)
}

// gate is the auth middleware, or nothing when the deployment runs without
// auth. am.toml requires auth whenever bind_address is not loopback.
func (s *QNTXServer) gate(reaching auth.Reach, handler http.HandlerFunc) http.HandlerFunc {
	if !s.authEnabled || s.authHandler == nil {
		return handler
	}
	return s.authHandler.Middleware(reaching, handler)
}
