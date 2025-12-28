package server

import (
	"net/http"
)

// HandleDevMode returns whether the server is in dev mode (plain text: "true" or "false")
func (s *QNTXServer) HandleDevMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	if s.isDevMode() {
		w.Write([]byte("true"))
	} else {
		w.Write([]byte("false"))
	}
}
