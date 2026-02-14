package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/ats/storage"
	syncPkg "github.com/teranos/QNTX/sync"
)

// gorillaSyncConn wraps gorilla/websocket.Conn to implement sync.Conn.
type gorillaSyncConn struct {
	conn *websocket.Conn
}

func (c *gorillaSyncConn) ReadJSON(v interface{}) error  { return c.conn.ReadJSON(v) }
func (c *gorillaSyncConn) WriteJSON(v interface{}) error { return c.conn.WriteJSON(v) }
func (c *gorillaSyncConn) Close() error                  { return c.conn.Close() }

// HandleSyncWebSocket handles incoming sync peer connections.
// The remote peer connects via WebSocket and both sides run the symmetric
// reconciliation protocol. This is the "accept incoming sync" side.
func (s *QNTXServer) HandleSyncWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.syncTree == nil || s.syncObserver == nil {
		http.Error(w, "Sync not available (WASM engine not loaded)", http.StatusServiceUnavailable)
		return
	}

	upgrader := getAxUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Errorw("Sync WebSocket upgrade failed", "error", err)
		return
	}

	store := storage.NewSQLStore(s.db, s.logger)
	wsConn := &gorillaSyncConn{conn: conn}
	peer := syncPkg.NewPeer(wsConn, s.syncTree, store, s.logger)

	sent, received, err := peer.Reconcile(r.Context())
	if err != nil {
		s.logger.Warnw("Sync reconciliation failed",
			"remote_addr", r.RemoteAddr,
			"sent", sent,
			"received", received,
			"error", err,
		)
		return
	}

	s.logger.Infow("Sync reconciliation complete",
		"remote_addr", r.RemoteAddr,
		"sent", sent,
		"received", received,
	)
}

// syncRequest is the JSON body for POST /api/sync.
type syncRequest struct {
	Peer string `json:"peer"` // e.g., "https://phone.local:877"
}

// syncResponse is the JSON response from POST /api/sync.
type syncResponse struct {
	Sent     int    `json:"sent"`
	Received int    `json:"received"`
	Error    string `json:"error,omitempty"`
}

// HandleSync initiates outbound sync with a peer.
// POST /api/sync {"peer":"https://phone.local:877"}
func (s *QNTXServer) HandleSync(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if s.syncTree == nil || s.syncObserver == nil {
		writeJSON(w, http.StatusServiceUnavailable, syncResponse{
			Error: "Sync not available (WASM engine not loaded)",
		})
		return
	}

	var req syncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
		return
	}

	if req.Peer == "" {
		writeError(w, http.StatusBadRequest, "Missing 'peer' field")
		return
	}

	// Convert HTTP(S) URL to WebSocket URL
	wsURL := httpToWS(req.Peer) + "/ws/sync"

	s.logger.Infow("Initiating sync with peer", "peer", wsURL)

	// Dial the remote peer's sync WebSocket
	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(r.Context(), wsURL, nil)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, syncResponse{
			Error: fmt.Sprintf("Failed to connect to peer %s: %v", req.Peer, err),
		})
		return
	}
	defer conn.Close()

	store := storage.NewSQLStore(s.db, s.logger)
	wsConn := &gorillaSyncConn{conn: conn}
	peer := syncPkg.NewPeer(wsConn, s.syncTree, store, s.logger)

	sent, received, err := peer.Reconcile(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, syncResponse{
			Sent:     sent,
			Received: received,
			Error:    fmt.Sprintf("Reconciliation failed: %v", err),
		})
		return
	}

	writeJSON(w, http.StatusOK, syncResponse{
		Sent:     sent,
		Received: received,
	})
}

// HandleSyncStatus returns the current sync tree state.
// GET /api/sync/status
func (s *QNTXServer) HandleSyncStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if s.syncTree == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"reason":    "WASM engine not loaded",
		})
		return
	}

	root, err := s.syncTree.Root()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"available": true,
			"error":     err.Error(),
		})
		return
	}

	groups, err := s.syncTree.GroupHashes()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"available": true,
			"error":     err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"available": true,
		"root":      root,
		"groups":    len(groups),
	})
}

// httpToWS converts http(s) URLs to ws(s) URLs.
func httpToWS(url string) string {
	if len(url) >= 8 && url[:8] == "https://" {
		return "wss://" + url[8:]
	}
	if len(url) >= 7 && url[:7] == "http://" {
		return "ws://" + url[7:]
	}
	return url
}
