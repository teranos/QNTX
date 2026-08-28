package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/errors"
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
		if err := writeSSEEntry(w, entry); err != nil {
			s.logger.Warnw("Plugin log stream ended during history", "error", err)
			return
		}
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
			if err := writeSSEEntry(w, entry); err != nil {
				s.logger.Warnw("Plugin log stream ended", "error", err)
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEEntry returns why an entry did not reach the stream. A silent skip
// leaves a gap the reader cannot see, and a write that fails means the client
// is gone, which is the loop's signal to stop rather than spin.
func writeSSEEntry(w http.ResponseWriter, entry grpcplugin.LogEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return errors.Wrapf(err, "log entry from %s could not be encoded", entry.Source)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return errors.Wrap(err, "the log stream could not be written to")
	}
	return nil
}
