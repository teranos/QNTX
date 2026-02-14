package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	appcfg "github.com/teranos/QNTX/am"
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
	peer := syncPkg.NewPeer(wsConn, s.syncTree, store, s.budgetTracker, s.logger)

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
		s.logger.Warnw("Failed to connect to sync peer", "peer", wsURL, "error", err)
		writeJSON(w, http.StatusBadGateway, syncResponse{
			Error: fmt.Sprintf("Failed to connect to peer %s: %v", req.Peer, err),
		})
		return
	}
	defer conn.Close()

	store := storage.NewSQLStore(s.db, s.logger)
	wsConn := &gorillaSyncConn{conn: conn}
	peer := syncPkg.NewPeer(wsConn, s.syncTree, store, s.budgetTracker, s.logger)

	sent, received, err := peer.Reconcile(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, syncResponse{
			Sent:     sent,
			Received: received,
			Error:    fmt.Sprintf("Reconciliation failed: %v", err),
		})
		return
	}

	if peer.RemoteBudget != nil {
		s.budgetTracker.SetPeerSpend(req.Peer,
			peer.RemoteBudget.DailyUSD,
			peer.RemoteBudget.WeeklyUSD,
			peer.RemoteBudget.MonthlyUSD,
			peer.RemoteBudget.ClusterDailyLimitUSD,
			peer.RemoteBudget.ClusterWeeklyLimitUSD,
			peer.RemoteBudget.ClusterMonthlyLimitUSD,
		)
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

// startSyncTicker runs periodic sync with all configured peers.
func (s *QNTXServer) startSyncTicker(ctx context.Context, interval time.Duration) {
	s.logger.Infow("Sync ticker started", "interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Per-peer failure tracking for log suppression
	failCounts := map[string]int{}
	lastWarned := map[string]time.Time{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncAllPeers(ctx, failCounts, lastWarned)
		}
	}
}

const syncWarnInitialAttempts = 5 // warn individually for first N failures per peer

// syncAllPeers reconciles with every configured peer. Emits one summary log
// per tick. Individual failure warnings are suppressed after 5 consecutive
// failures per peer, then re-emitted hourly.
func (s *QNTXServer) syncAllPeers(ctx context.Context, failCounts map[string]int, lastWarned map[string]time.Time) {
	cfg, _ := appcfg.Load()
	if cfg == nil || len(cfg.Sync.Peers) == 0 {
		return
	}

	var synced int
	var transferred []string
	var unreachable []string

	for name, peerURL := range cfg.Sync.Peers {
		peerCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

		wsURL := httpToWS(peerURL) + "/ws/sync"
		dialer := websocket.Dialer{}
		conn, _, err := dialer.DialContext(peerCtx, wsURL, nil)
		if err != nil {
			cancel()
			failCounts[name]++
			unreachable = append(unreachable, name)
			s.syncPeerStatus.Store(name, "unreachable")

			if failCounts[name] <= syncWarnInitialAttempts || time.Since(lastWarned[name]) > time.Hour {
				s.logger.Warnw("Scheduled sync: failed to connect",
					"peer", name, "url", peerURL, "error", err,
					"consecutive_failures", failCounts[name],
				)
				lastWarned[name] = time.Now()
			}
			continue
		}

		store := storage.NewSQLStore(s.db, s.logger)
		wsConn := &gorillaSyncConn{conn: conn}
		peer := syncPkg.NewPeer(wsConn, s.syncTree, store, s.budgetTracker, s.logger)

		sent, received, err := peer.Reconcile(peerCtx)
		conn.Close()
		cancel()

		if err != nil {
			failCounts[name]++
			unreachable = append(unreachable, name)
			s.syncPeerStatus.Store(name, "unreachable")

			if failCounts[name] <= syncWarnInitialAttempts || time.Since(lastWarned[name]) > time.Hour {
				s.logger.Warnw("Scheduled sync: reconciliation failed",
					"peer", name, "sent", sent, "received", received, "error", err,
				)
				lastWarned[name] = time.Now()
			}
			continue
		}

		// Success — reset failure tracking
		failCounts[name] = 0
		s.syncPeerStatus.Store(name, "ok")
		if peer.RemoteBudget != nil {
			s.budgetTracker.SetPeerSpend(name,
				peer.RemoteBudget.DailyUSD,
				peer.RemoteBudget.WeeklyUSD,
				peer.RemoteBudget.MonthlyUSD,
				peer.RemoteBudget.ClusterDailyLimitUSD,
				peer.RemoteBudget.ClusterWeeklyLimitUSD,
				peer.RemoteBudget.ClusterMonthlyLimitUSD,
			)
		}
		synced++

		if sent > 0 || received > 0 {
			transferred = append(transferred, fmt.Sprintf("%s ↑%d↓%d", name, sent, received))
		}
	}

	// Push updated status to connected browsers (peer reachability + tree state)
	s.broadcastSyncStatus()

	// One summary line per tick (only when something noteworthy happened)
	if len(transferred) > 0 || len(unreachable) > 0 {
		fields := []interface{}{}
		if synced > 0 {
			fields = append(fields, "synced", synced)
		}
		if len(transferred) > 0 {
			fields = append(fields, "transferred", strings.Join(transferred, ", "))
		}
		if len(unreachable) > 0 {
			fields = append(fields, "unreachable", len(unreachable))
		}
		s.logger.Infow("Sync tick", fields...)
	}
}

// broadcastSyncStatus pushes the current tree state to all connected browsers.
func (s *QNTXServer) broadcastSyncStatus() {
	if s.syncTree == nil {
		return
	}

	root, err := s.syncTree.Root()
	if err != nil {
		return
	}

	groups, err := s.syncTree.GroupHashes()
	if err != nil {
		return
	}

	s.sendMessageToClients(map[string]interface{}{
		"type":      "sync_status",
		"available": true,
		"root":      root,
		"groups":    len(groups),
		"peers":     s.buildPeerList(),
	}, "")
}

// buildPeerList returns configured peers with their reachability status.
func (s *QNTXServer) buildPeerList() []map[string]string {
	cfg, _ := appcfg.Load()
	peers := []map[string]string{}
	if cfg != nil {
		for name, url := range cfg.Sync.Peers {
			status := ""
			if v, ok := s.syncPeerStatus.Load(name); ok {
				status = v.(string)
			}
			peers = append(peers, map[string]string{
				"name":   name,
				"url":    url,
				"status": status,
			})
		}
	}
	return peers
}
