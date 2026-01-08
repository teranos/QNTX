package server

import (
	"github.com/teranos/QNTX/ats/ax"
)

// sendSystemCapabilitiesToClient sends system capability information to a newly connected client.
// This informs the frontend about available optimizations (e.g., Rust fuzzy matching).
func (s *QNTXServer) sendSystemCapabilitiesToClient(client *Client) {
	// Check if client is closed before sending to prevent panic.
	// This can happen if client disconnects between registration and this send.
	if client.IsClosed() {
		s.logger.Debugw("Client disconnected before system capabilities could be sent",
			"client_id", client.id,
		)
		return
	}

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
	// Use non-blocking send to handle case where channel is full
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
		// Channel full - skip sending
		s.logger.Debugw("Client channel full, skipping system capabilities",
			"client_id", client.id,
		)
	}
}
