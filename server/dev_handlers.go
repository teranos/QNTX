package server

import (
	"net/http"

	appcfg "github.com/teranos/QNTX/internal/config"
)

// isDevMode checks if the server is running in development mode
func (s *QNTXServer) isDevMode() bool {
	return appcfg.IsDevMode()
}

// HandleDevMode returns whether the server is in dev mode (plain text: "true" or "false")
func (s *QNTXServer) HandleDevMode(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	answer := "false"
	if s.isDevMode() {
		answer = "true"
	}
	deliver(w, s.logger, []byte(answer), "dev mode")
}
