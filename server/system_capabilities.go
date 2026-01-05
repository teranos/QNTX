package server

import (
	"github.com/teranos/QNTX/ats/ax"
)

// sendSystemCapabilitiesToClient sends system capability information to a newly connected client.
// This informs the frontend about available optimizations (e.g., Rust fuzzy matching).
func (s *QNTXServer) sendSystemCapabilitiesToClient(client *Client) {
	// Get fuzzy backend from the AxGraphBuilder
	fuzzyBackend := s.builder.FuzzyBackend()
	fuzzyOptimized := (fuzzyBackend == ax.MatcherBackendRust)

	// Create system capabilities message
	msg := SystemCapabilitiesMessage{
		Type:           "system_capabilities",
		FuzzyBackend:   string(fuzzyBackend),
		FuzzyOptimized: fuzzyOptimized,
	}

	// Send to client via generic message channel
	// Use non-blocking send to handle case where client disconnects before we send
	select {
	case client.sendMsg <- msg:
		s.logger.Debugw("Sent system capabilities to client",
			"client_id", client.id,
			"fuzzy_backend", fuzzyBackend,
			"fuzzy_optimized", fuzzyOptimized,
		)
	case <-s.ctx.Done():
		return
	default:
		// Client disconnected or channel full
		// This is expected during rapid connect/disconnect scenarios
		s.logger.Debugw("Client channel unavailable for system capabilities",
			"client_id", client.id,
		)
	}
}
