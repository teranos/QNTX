// Package qntxvidstream provides a video inference plugin for QNTX.
//
// The vidstream plugin wraps the Rust ONNX video engine via CGO, exposing
// real-time video frame processing as HTTP endpoints. It registers a canvas
// glyph with window manifestation for the frontend UI.
//
// Build with:
//
//	CGO_ENABLED=1 go build -tags rustvideo ./qntx-vidstream/cmd/qntx-vidstream-plugin
package qntxvidstream

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"sync"

	"github.com/teranos/QNTX/ats/vidstream/vidstream"
	"github.com/teranos/QNTX/plugin"
)

//go:embed web/vidstream-glyph-module.js
var vidstreamGlyphModuleJS []byte

// Plugin is the vidstream plugin implementation.
type Plugin struct {
	plugin.Base

	engine   *vidstream.VideoEngine
	engineMu sync.Mutex
}

// NewPlugin creates a new vidstream plugin.
func NewPlugin() *Plugin {
	return &Plugin{
		Base: plugin.NewBase(plugin.Metadata{
			Name:        "vidstream",
			Version:     "0.1.0",
			QNTXVersion: ">= 0.1.0",
			Description: "Real-time video inference via ONNX Runtime (Rust CGO)",
			Author:      "QNTX Team",
			License:     "MIT",
		}),
	}
}

// Initialize initializes the vidstream plugin.
func (p *Plugin) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	p.Init(services)
	services.Logger("vidstream").Info("vidstream plugin initialized")
	return nil
}

// Shutdown shuts down the vidstream plugin, closing the engine if active.
func (p *Plugin) Shutdown(ctx context.Context) error {
	p.engineMu.Lock()
	defer p.engineMu.Unlock()

	if p.engine != nil {
		p.engine.Close()
		p.engine = nil
	}

	p.Services().Logger("vidstream").Info("vidstream plugin shut down")
	return nil
}

// RegisterHTTP registers HTTP handlers for the vidstream plugin.
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
	return p.registerHTTPHandlers(mux)
}

// Health returns the health status of the vidstream plugin.
func (p *Plugin) Health(ctx context.Context) plugin.HealthStatus {
	p.engineMu.Lock()
	ready := p.engine != nil && p.engine.IsReady()
	p.engineMu.Unlock()

	paused := p.IsPaused()
	message := "vidstream plugin operational"
	if paused {
		message = "vidstream plugin paused"
	}

	return plugin.HealthStatus{
		Healthy: true,
		Paused:  paused,
		Message: message,
		Details: map[string]any{
			"engine_ready": ready,
		},
	}
}

// ConfigSchema returns the configuration schema for UI-based configuration.
func (p *Plugin) ConfigSchema() map[string]plugin.ConfigField {
	return map[string]plugin.ConfigField{
		"model_path": {
			Type:         "string",
			Description:  "Path to ONNX model file",
			DefaultValue: "ats/vidstream/models/yolo11n.onnx",
			Required:     false,
		},
		"confidence_threshold": {
			Type:         "number",
			Description:  "Detection confidence threshold (0.0-1.0)",
			DefaultValue: "0.5",
			Required:     false,
			MinValue:     "0.0",
			MaxValue:     "1.0",
		},
		"nms_threshold": {
			Type:         "number",
			Description:  "NMS IoU threshold (0.0-1.0)",
			DefaultValue: "0.45",
			Required:     false,
			MinValue:     "0.0",
			MaxValue:     "1.0",
		},
	}
}

// RegisterGlyphs returns custom glyph type definitions provided by this plugin.
func (p *Plugin) RegisterGlyphs() []plugin.GlyphDef {
	return []plugin.GlyphDef{
		{
			Symbol:        "⮀",
			Title:         "VidStream",
			Label:         "vidstream",
			ModulePath:    "/vidstream-glyph-module.js",
			DefaultWidth:  680,
			DefaultHeight: 620,
		},
	}
}

// initEngine initializes the ONNX engine with the given config.
// Caller must hold engineMu.
func (p *Plugin) initEngine(modelPath string, confThreshold, nmsThreshold float32) error {
	logger := p.Services().Logger("vidstream")

	// Close existing engine if any
	if p.engine != nil {
		p.engine.Close()
		p.engine = nil
	}

	config := vidstream.Config{
		ModelPath:           modelPath,
		ConfidenceThreshold: confThreshold,
		NMSThreshold:        nmsThreshold,
		InputWidth:          640,
		InputHeight:         480,
		NumThreads:          0,
		UseGPU:              false,
		Labels:              "",
	}

	engine, err := vidstream.NewVideoEngineWithConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create video engine with model %s: %w", modelPath, err)
	}

	p.engine = engine
	width, height := engine.InputDimensions()
	logger.Infow("ONNX engine initialized",
		"model_path", modelPath,
		"input_dimensions", fmt.Sprintf("%dx%d", width, height),
		"ready", engine.IsReady(),
	)

	return nil
}

// Verify Plugin implements all optional interfaces at compile time.
var _ plugin.ConfigurablePlugin = (*Plugin)(nil)
var _ plugin.PausablePlugin = (*Plugin)(nil)
var _ plugin.UIPlugin = (*Plugin)(nil)
