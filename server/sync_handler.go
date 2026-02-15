package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

	cfg, _ := appcfg.Load()
	store := storage.NewSQLStore(s.db, s.logger)
	wsConn := &gorillaSyncConn{conn: conn}
	peer := syncPkg.NewPeer(wsConn, s.syncTree, store, s.budgetTracker, s.logger)
	if cfg != nil {
		peer.LocalName = cfg.Sync.Name
	}

	_, _, err = peer.Reconcile(r.Context())
	if err != nil {
		s.logger.Warnw("Sync reconciliation failed",
			"peer", peer.Name,
			"remote_addr", r.RemoteAddr,
			"error", err,
		)
	}
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

	cfg, _ := appcfg.Load()

	if isSelfPeer(req.Peer, appcfg.GetServerPort()) {
		writeJSON(w, http.StatusOK, syncResponse{})
		return
	}

	// Resolve peer name from config (reverse lookup URL → name)
	peerName := req.Peer
	if cfg != nil {
		for name, u := range cfg.Sync.Peers {
			if u == req.Peer {
				peerName = name
				break
			}
		}
	}

	// Convert HTTP(S) URL to WebSocket URL
	wsURL := httpToWS(req.Peer) + "/ws/sync"

	s.logger.Infow("Initiating sync with peer", "peer", peerName)

	// Dial the remote peer's sync WebSocket
	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(r.Context(), wsURL, nil)
	if err != nil {
		s.logger.Warnw("Failed to connect to sync peer", "peer", peerName, "error", err)
		writeJSON(w, http.StatusBadGateway, syncResponse{
			Error: fmt.Sprintf("Failed to connect to peer %s: %v", req.Peer, err),
		})
		return
	}
	defer conn.Close()

	store := storage.NewSQLStore(s.db, s.logger)
	wsConn := &gorillaSyncConn{conn: conn}
	peer := syncPkg.NewPeer(wsConn, s.syncTree, store, s.budgetTracker, s.logger)
	peer.Name = peerName
	if cfg != nil {
		peer.LocalName = cfg.Sync.Name
	}

	sent, received, err := peer.Reconcile(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, syncResponse{
			Sent:     sent,
			Received: received,
			Error:    fmt.Sprintf("Reconciliation failed: %v", err),
		})
		return
	}

	if peer.RemoteName != "" {
		s.syncPeerRemoteName.Store(peerName, peer.RemoteName)
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

// isSelfPeer checks if a peer URL points to this node by comparing ports.
// Shared cluster rosters naturally include self — this silently filters it.
func isSelfPeer(peerURL string, serverPort int) bool {
	parsed, err := url.Parse(peerURL)
	if err != nil {
		return false
	}
	port := parsed.Port()
	if port == "" {
		return false
	}
	return port == fmt.Sprintf("%d", serverPort)
}

// shortError extracts a compact reason from an error (e.g. "connection refused").
func shortError(err error) string {
	s := err.Error()
	// Dial errors: "dial tcp [::1]:8777: connect: connection refused"
	if i := strings.LastIndex(s, ": "); i >= 0 {
		return s[i+2:]
	}
	return s
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

	state := &syncTickState{
		failCounts:  map[string]int{},
		nextAttempt: map[string]time.Time{},
		interval:    interval,
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncAllPeers(ctx, state)
		}
	}
}

// syncTickState tracks per-peer failure counts and backoff timing.
type syncTickState struct {
	failCounts  map[string]int
	nextAttempt map[string]time.Time // skip peer until this time
	interval    time.Duration
}

// backoffMultiplier returns how many intervals to wait before retrying.
// Failures 1-3: every tick. 4-6: 10× interval. 7+: 100× interval.
func (st *syncTickState) backoffMultiplier(failures int) int {
	switch {
	case failures <= 3:
		return 1
	case failures <= 6:
		return 10
	default:
		return 100
	}
}

// syncAllPeers reconciles with every configured peer. Emits one summary log
// per tick. First failure per peer gets a separate WARN; after that, failures
// are only reported in the summary. Unreachable peers are retried with backoff.
func (s *QNTXServer) syncAllPeers(ctx context.Context, st *syncTickState) {
	cfg, _ := appcfg.Load()
	if cfg == nil || len(cfg.Sync.Peers) == 0 {
		return
	}

	serverPort := appcfg.GetServerPort()

	var syncedNames []string
	var transferred []string
	// Group unreachable peers by reason for compact summary
	failedByReason := map[string][]string{} // short reason → peer names

	now := time.Now()
	var backedOff []string

	for name, peerURL := range cfg.Sync.Peers {
		// Shared cluster rosters include self — skip silently
		if isSelfPeer(peerURL, serverPort) {
			s.syncPeerStatus.Store(name, "self")
			continue
		}

		// Backoff: skip peers that aren't due for a retry yet
		if next, ok := st.nextAttempt[name]; ok && now.Before(next) {
			backedOff = append(backedOff, name)
			continue
		}

		peerCtx, cancel := context.WithTimeout(ctx, 30*time.Second)

		wsURL := httpToWS(peerURL) + "/ws/sync"
		dialer := websocket.Dialer{}
		conn, _, err := dialer.DialContext(peerCtx, wsURL, nil)
		if err != nil {
			cancel()
			st.failCounts[name]++
			if st.failCounts[name] == 1 {
				s.logger.Warnw("Sync peer unreachable", "peer", name, "error", err)
			}
			backoff := time.Duration(st.backoffMultiplier(st.failCounts[name])) * st.interval
			st.nextAttempt[name] = now.Add(backoff)
			reason := shortError(err)
			failedByReason[reason] = append(failedByReason[reason], name)
			s.syncPeerStatus.Store(name, "unreachable")
			continue
		}

		store := storage.NewSQLStore(s.db, s.logger)
		wsConn := &gorillaSyncConn{conn: conn}
		peer := syncPkg.NewPeer(wsConn, s.syncTree, store, s.budgetTracker, s.logger)
		peer.Name = name
		peer.LocalName = cfg.Sync.Name

		sent, received, err := peer.Reconcile(peerCtx)
		conn.Close()
		cancel()

		if err != nil {
			st.failCounts[name]++
			if st.failCounts[name] == 1 {
				s.logger.Warnw("Sync reconciliation failed", "peer", name, "error", err)
			}
			backoff := time.Duration(st.backoffMultiplier(st.failCounts[name])) * st.interval
			st.nextAttempt[name] = now.Add(backoff)
			reason := shortError(err)
			failedByReason[reason] = append(failedByReason[reason], name)
			s.syncPeerStatus.Store(name, "unreachable")
			continue
		}

		// Success — reset failure tracking
		st.failCounts[name] = 0
		delete(st.nextAttempt, name)
		s.syncPeerStatus.Store(name, "ok")
		if peer.RemoteName != "" {
			s.syncPeerRemoteName.Store(name, peer.RemoteName)
		}
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
		syncedNames = append(syncedNames, name)

		if sent > 0 || received > 0 {
			transferred = append(transferred, fmt.Sprintf("%s ↑%d↓%d", name, sent, received))
		}
	}

	// Push updated status to connected browsers (peer reachability + tree state)
	s.broadcastSyncStatus()

	// One summary line per tick (only when something noteworthy happened)
	if len(syncedNames) > 0 || len(failedByReason) > 0 || len(backedOff) > 0 {
		fields := []interface{}{}
		if len(syncedNames) > 0 {
			fields = append(fields, "synced", strings.Join(syncedNames, ","))
		}
		if len(transferred) > 0 {
			fields = append(fields, "transferred", strings.Join(transferred, ", "))
		}
		if len(failedByReason) > 0 {
			var groups []string
			for reason, names := range failedByReason {
				groups = append(groups, fmt.Sprintf("%s (%s)", strings.Join(names, ","), reason))
			}
			fields = append(fields, "unreachable", strings.Join(groups, ", "))
		}
		if len(backedOff) > 0 {
			fields = append(fields, "backed_off", strings.Join(backedOff, ","))
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
			entry := map[string]string{
				"name":   name,
				"url":    url,
				"status": status,
			}
			if v, ok := s.syncPeerRemoteName.Load(name); ok {
				entry["advertised_name"] = v.(string)
			}
			peers = append(peers, entry)
		}
	}
	return peers
}
