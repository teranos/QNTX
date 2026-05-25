package server

import (
	"net/http"
)

// HandleDevMode returns whether the server is in dev mode (plain text: "true" or "false")
func (s *QNTXServer) HandleDevMode(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	if s.isDevMode() {
		w.Write([]byte("true"))
	} else {
		w.Write([]byte("false"))
	}
}
