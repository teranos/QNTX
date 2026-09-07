package server

import (
	"net/http"

	"github.com/teranos/QNTX/server/openapi"
)

// HandleOpenAPI answers with the OpenAPI document for this build: every path
// server/reach's table names, who reaches it, and which Go function answers.
//
// ROOT's, like /api/config. The table already treats which paths exist as
// something a stranger does not learn — a caller who reaches nothing is told
// nothing about what is there — and a route list handed to anyone would say it
// all at once.
func (s *QNTXServer) HandleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET is the whole of it")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(openapi.Document()); err != nil {
		s.logger.Errorw("could not write the OpenAPI document", "error", err)
	}
}
