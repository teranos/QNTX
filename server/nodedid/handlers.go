package nodedid

import "net/http"

// HandleDIDDocument serves the node's DID document at /.well-known/did.json.
func (h *Handler) HandleDIDDocument(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/did+json")
	w.WriteHeader(http.StatusOK)
	w.Write(h.didDocument)
}
