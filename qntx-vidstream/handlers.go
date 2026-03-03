package qntxvidstream

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/teranos/QNTX/ats/vidstream/vidstream"
	"github.com/teranos/QNTX/plugin/httputil"
)

// registerHTTPHandlers registers all HTTP handlers for the vidstream plugin.
func (p *Plugin) registerHTTPHandlers(mux *http.ServeMux) error {
	// Glyph UI module
	mux.HandleFunc("GET /vidstream-glyph-module.js", p.handleGlyphModule)

	// Video engine operations
	mux.HandleFunc("POST /init", p.handleInit)
	mux.HandleFunc("POST /frame", p.handleFrame)
	mux.HandleFunc("GET /status", p.handleStatus)

	return nil
}

// handleGlyphModule serves the embedded glyph module.
func (p *Plugin) handleGlyphModule(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(vidstreamGlyphModuleJS)
}

// initRequest is the JSON body for POST /init.
type initRequest struct {
	ModelPath           string  `json:"model_path"`
	ConfidenceThreshold float32 `json:"confidence_threshold"`
	NMSThreshold        float32 `json:"nms_threshold"`
}

// handleInit initializes the ONNX video engine.
func (p *Plugin) handleInit(w http.ResponseWriter, r *http.Request) {
	var req initRequest
	if err := httputil.ReadJSON(w, r, &req); err != nil {
		return
	}

	if req.ModelPath == "" {
		httputil.WriteError(w, http.StatusBadRequest, "model_path is required")
		return
	}

	// Apply defaults
	if req.ConfidenceThreshold <= 0 {
		req.ConfidenceThreshold = 0.5
	}
	if req.NMSThreshold <= 0 {
		req.NMSThreshold = 0.45
	}

	p.engineMu.Lock()
	err := p.initEngine(req.ModelPath, req.ConfidenceThreshold, req.NMSThreshold)
	if err != nil {
		p.engineMu.Unlock()
		p.Services().Logger("vidstream").Errorw("Engine init failed",
			"model_path", req.ModelPath, "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("Engine init failed: %v", err))
		return
	}

	width, height := p.engine.InputDimensions()
	ready := p.engine.IsReady()
	p.engineMu.Unlock()

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status": "initialized",
		"width":  width,
		"height": height,
		"ready":  ready,
	})
}

// frameRequest is the JSON body for POST /frame.
type frameRequest struct {
	FrameData []byte `json:"frame_data"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Format    string `json:"format"`
}

// handleFrame processes a video frame through ONNX inference.
func (p *Plugin) handleFrame(w http.ResponseWriter, r *http.Request) {
	// Check content type — support both JSON and binary
	contentType := r.Header.Get("Content-Type")

	var frameData []byte
	var width, height int
	var format string

	if contentType == "application/octet-stream" {
		// Binary mode: frame metadata in headers, raw bytes in body
		width = headerInt(r, "X-Frame-Width")
		height = headerInt(r, "X-Frame-Height")
		format = r.Header.Get("X-Frame-Format")

		var err error
		frameData, err = io.ReadAll(io.LimitReader(r.Body, 16*1024*1024)) // 16MB limit
		if err != nil {
			httputil.WriteError(w, http.StatusBadRequest, fmt.Sprintf("Failed to read frame body: %v", err))
			return
		}
	} else {
		// JSON mode (backward compat)
		var req frameRequest
		if err := httputil.ReadJSON(w, r, &req); err != nil {
			return
		}
		frameData = req.FrameData
		width = req.Width
		height = req.Height
		format = req.Format
	}

	// Validate dimensions
	if width <= 0 || width > 4096 || height <= 0 || height > 4096 {
		httputil.WriteError(w, http.StatusBadRequest,
			fmt.Sprintf("Invalid frame dimensions: %dx%d (must be 1-4096)", width, height))
		return
	}

	// Parse format
	var frameFormat vidstream.FrameFormat
	switch format {
	case "rgba8":
		frameFormat = vidstream.FormatRGBA8
	case "rgb8":
		frameFormat = vidstream.FormatRGB8
	default:
		httputil.WriteError(w, http.StatusBadRequest,
			fmt.Sprintf("Unsupported format: %s (use rgba8 or rgb8)", format))
		return
	}

	p.engineMu.Lock()
	if p.engine == nil {
		p.engineMu.Unlock()
		httputil.WriteError(w, http.StatusPreconditionFailed, "Engine not initialized. Call /init first.")
		return
	}

	result, err := p.engine.ProcessFrame(
		frameData,
		uint32(width),
		uint32(height),
		frameFormat,
		uint64(time.Now().UnixMicro()),
	)
	p.engineMu.Unlock()

	if err != nil {
		p.Services().Logger("vidstream").Warnw("Frame processing failed", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("Frame processing failed: %v", err))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"detections": result.Detections,
		"stats": map[string]interface{}{
			"preprocess_us":    result.Stats.PreprocessUs,
			"inference_us":     result.Stats.InferenceUs,
			"postprocess_us":   result.Stats.PostprocessUs,
			"total_us":         result.Stats.TotalUs,
			"detections_raw":   result.Stats.DetectionsRaw,
			"detections_final": result.Stats.DetectionsFinal,
		},
	})
}

// handleStatus returns the current engine status.
func (p *Plugin) handleStatus(w http.ResponseWriter, r *http.Request) {
	p.engineMu.Lock()
	var status map[string]interface{}
	if p.engine != nil {
		width, height := p.engine.InputDimensions()
		status = map[string]interface{}{
			"engine_ready":     p.engine.IsReady(),
			"input_dimensions": fmt.Sprintf("%dx%d", width, height),
			"version":          vidstream.Version(),
		}
	} else {
		status = map[string]interface{}{
			"engine_ready": false,
			"version":      vidstream.Version(),
		}
	}
	p.engineMu.Unlock()

	httputil.WriteJSON(w, http.StatusOK, status)
}

// headerInt reads an integer from a request header. Returns 0 if missing or invalid.
func headerInt(r *http.Request, name string) int {
	v := r.Header.Get(name)
	if v == "" {
		return 0
	}
	var n int
	if err := json.Unmarshal([]byte(v), &n); err != nil {
		return 0
	}
	return n
}
