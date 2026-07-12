package server

import (
	"encoding/json"
	"net/http"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// TODO: capability-based routing — replace this with a generic provider resolution layer
// where the frontend discovers endpoints dynamically (e.g., GET /api/capabilities/python
// returns the provider name, and pluginFetch routes accordingly). This handler is the
// stopgap: it gives the frontend a stable /api/python/execute endpoint that delegates
// to whichever plugin declared python_provider=true via gRPC PythonService.

// HandlePythonExecute handles POST /api/python/execute by delegating to the
// python provider's gRPC PythonService, regardless of plugin name.
func (s *QNTXServer) HandlePythonExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.pythonClient == nil {
		http.Error(w, "No python provider available", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Content     string `json:"content"`
		CaptureVars bool   `json:"capture_variables"`
		GlyphID     string `json:"glyph_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	resp, err := s.pythonClient.Execute(r.Context(), &protocol.PythonExecuteRequest{
		Code:    req.Content,
		GlyphId: req.GlyphID,
	})
	if err != nil {
		s.logger.Errorw("Python execution failed", "glyph_id", req.GlyphID, "error", err)
		writeJSON(w, http.StatusOK, map[string]any{
			"success":     false,
			"stdout":      "",
			"stderr":      "",
			"result":      nil,
			"error":       err.Error(),
			"duration_ms": 0,
		})
		return
	}

	// Forward the gRPC response as the JSON the frontend expects (ExecutionResult shape)
	result := map[string]any{
		"success":     resp.Success,
		"stdout":      resp.Output,
		"stderr":      "",
		"error":       resp.Error,
		"duration_ms": 0,
	}

	// Parse result bytes if present (JSON from the plugin)
	if len(resp.Result) > 0 {
		var parsed any
		if err := json.Unmarshal(resp.Result, &parsed); err == nil {
			result["result"] = parsed
		}
	}

	writeJSON(w, http.StatusOK, result)
}
