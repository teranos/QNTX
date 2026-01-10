package server

import (
	"github.com/teranos/QNTX/ats/ax"
)

// sendSystemCapabilitiesToClient sends system capability information to a newly connected client.
// This informs the frontend about available optimizations (e.g., Rust fuzzy matching, ONNX video).
// Sends are routed through broadcast worker (thread-safe).
func (s *QNTXServer) sendSystemCapabilitiesToClient(client *Client) {
	// Get fuzzy backend from the AxGraphBuilder
	fuzzyBackend := s.builder.FuzzyBackend()
	fuzzyOptimized := (fuzzyBackend == ax.MatcherBackendRust)

	// Detect vidstream/ONNX availability (requires CGO build with vidstream)
	vidstreamBackend := "onnx"
	vidstreamOptimized := true
	// If vidstream is not compiled in, this will be set to false by build tags
	// For now, assume available if CGO is enabled (vidstream requires CGO)

	// Create system capabilities message
	msg := SystemCapabilitiesMessage{
		Type:               "system_capabilities",
		FuzzyBackend:       string(fuzzyBackend),
		FuzzyOptimized:     fuzzyOptimized,
		VidStreamBackend:   vidstreamBackend,
		VidStreamOptimized: vidstreamOptimized,
	}

	// Send to broadcast worker (thread-safe)
	req := &broadcastRequest{
		reqType:  "message",
		msg:      msg,
		clientID: client.id, // Send to specific client only
	}

	select {
	case s.broadcastReq <- req:
		s.logger.Debugw("Queued system capabilities to client",
			"client_id", client.id,
			"fuzzy_backend", fuzzyBackend,
			"fuzzy_optimized", fuzzyOptimized,
		)
	case <-s.ctx.Done():
		return
	default:
		// Broadcast queue full (should never happen with proper sizing)
		s.logger.Warnw("Broadcast request queue full, skipping system capabilities",
			"client_id", client.id,
		)
	}
}
