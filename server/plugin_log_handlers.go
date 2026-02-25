package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
)

// HandlePluginLogs streams plugin log entries via Server-Sent Events.
// GET /api/plugins/{name}/logs
func (s *QNTXServer) HandlePluginLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Parse plugin name from path: /api/plugins/{name}/logs
	path := strings.TrimPrefix(r.URL.Path, "/api/plugins/")
	pluginName := strings.TrimSuffix(path, "/logs")

	if pluginName == "" {
		writeError(w, http.StatusBadRequest, "plugin name required in URL path")
		return
	}

	pm := s.getPluginManager()
	if pm == nil {
		writeError(w, http.StatusServiceUnavailable, "plugin manager not available")
		return
	}

	buf := pm.GetLogBuffer(pluginName)
	if buf == nil {
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("no log buffer for plugin '%s' (remote plugins don't have local logs)", pluginName))
		return
	}

	// SSE headers
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send recent history as initial batch
	history := buf.Recent(200)
	for _, entry := range history {
		writeSSEEntry(w, entry)
	}
	flusher.Flush()

	// Subscribe for new entries
	ch := buf.Subscribe()
	defer buf.Unsubscribe(ch)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-ch:
			if !ok {
				return
			}
			writeSSEEntry(w, entry)
			flusher.Flush()
		}
	}
}

func writeSSEEntry(w http.ResponseWriter, entry grpcplugin.LogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}
