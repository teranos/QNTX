package nodedid

import "net/http"

// HandleDIDDocument serves the node's DID document at /.well-known/did.json.
// A peer that cannot read this cannot verify anything this node ever signed, so
// the failure belongs in the log rather than only in the peer's connection.
func (h *Handler) HandleDIDDocument(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/did+json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(h.didDocument); err != nil && h.logger != nil {
		h.logger.Warnw("DID document not delivered", "did", h.DID, "peer", r.RemoteAddr, "error", err)
	}
}
