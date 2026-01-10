package server

import (
	"fmt"
	"time"

	"github.com/teranos/QNTX/ats/vidstream/vidstream"
)

// handleVidStreamInit initializes the ONNX video engine with model configuration
// Runs asynchronously to avoid blocking the WebSocket message handler
func (c *Client) handleVidStreamInit(msg QueryMessage) {
	c.server.logger.Infow("VidStream init request",
		"client_id", c.id,
		"model_path", msg.ModelPath,
	)

	// Run initialization in background to avoid blocking WebSocket
	go func() {
		c.server.vidstreamMu.Lock()
		defer c.server.vidstreamMu.Unlock()

		// Close existing engine if any
		if c.server.vidstreamEngine != nil {
			c.server.vidstreamEngine.Close()
			c.server.vidstreamEngine = nil
		}

		// Create new engine with config
		config := vidstream.Config{
			ModelPath:           msg.ModelPath,
			ConfidenceThreshold: msg.ConfidenceThreshold,
			NMSThreshold:        msg.NMSThreshold,
			InputWidth:          640, // Default YOLOv11 input size
			InputHeight:         480,
			NumThreads:          0,     // Auto-detect
			UseGPU:              false, // CPU inference for now
			Labels:              "",    // Use default COCO labels
		}

		engine, err := vidstream.NewVideoEngineWithConfig(config)
		if err != nil {
			c.server.logger.Errorw("VidStream init failed",
				"error", err,
				"client_id", c.id,
			)
			c.sendMsg <- map[string]interface{}{
				"type":  "vidstream_init_error",
				"error": err.Error(),
			}
			return
		}

		c.server.vidstreamEngine = engine
		width, height := engine.InputDimensions()
		ready := engine.IsReady()

		c.server.logger.Infow("VidStream engine initialized",
			"client_id", c.id,
			"input_dimensions", fmt.Sprintf("%dx%d", width, height),
			"ready", ready,
		)

		c.sendMsg <- map[string]interface{}{
			"type":   "vidstream_init_success",
			"width":  width,
			"height": height,
			"ready":  ready,
		}
	}()
}

// handleVidStreamFrame processes a video frame through ONNX inference
func (c *Client) handleVidStreamFrame(msg QueryMessage) {
	c.server.vidstreamMu.Lock()
	defer c.server.vidstreamMu.Unlock()

	if c.server.vidstreamEngine == nil {
		c.sendMsg <- map[string]interface{}{
			"type":  "vidstream_frame_error",
			"error": "Engine not initialized. Call vidstream_init first.",
		}
		return
	}

	// Parse format
	var format vidstream.FrameFormat
	switch msg.Format {
	case "rgba8":
		format = vidstream.FormatRGBA8
	case "rgb8":
		format = vidstream.FormatRGB8
	default:
		c.sendMsg <- map[string]interface{}{
			"type":  "vidstream_frame_error",
			"error": fmt.Sprintf("Unsupported format: %s", msg.Format),
		}
		return
	}

	// Process frame
	result, err := c.server.vidstreamEngine.ProcessFrame(
		msg.FrameData,
		uint32(msg.Width),
		uint32(msg.Height),
		format,
		uint64(time.Now().UnixMicro()),
	)
	if err != nil {
		c.server.logger.Warnw("VidStream frame processing failed",
			"error", err,
			"client_id", c.id,
		)
		c.sendMsg <- map[string]interface{}{
			"type":  "vidstream_frame_error",
			"error": err.Error(),
		}
		return
	}

	// Send detections back to client
	c.sendMsg <- map[string]interface{}{
		"type":       "vidstream_detections",
		"detections": result.Detections,
		"stats": map[string]interface{}{
			"preprocess_us":    result.Stats.PreprocessUs,
			"inference_us":     result.Stats.InferenceUs,
			"postprocess_us":   result.Stats.PostprocessUs,
			"total_us":         result.Stats.TotalUs,
			"detections_raw":   result.Stats.DetectionsRaw,
			"detections_final": result.Stats.DetectionsFinal,
		},
	}
}
