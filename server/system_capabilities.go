package server

import (
	"time"

	"github.com/teranos/QNTX/ats/ax"
)

// sendSystemCapabilitiesToClient sends system capability information to a newly connected client.
// This informs the frontend about available optimizations (e.g., Rust fuzzy matching).
func (s *QNTXServer) sendSystemCapabilitiesToClient(client *Client) {
	// Small delay to ensure client is fully registered
	select {
	case <-time.After(50 * time.Millisecond):
	case <-s.ctx.Done():
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
	select {
	case client.sendMsg <- msg:
		s.logger.Debugw("Sent system capabilities to client",
			"client_id", client.id,
			"fuzzy_backend", fuzzyBackend,
			"fuzzy_optimized", fuzzyOptimized,
		)
	case <-s.ctx.Done():
		return
	case <-time.After(2 * time.Second):
		s.logger.Warnw("Timeout sending system capabilities to client",
			"client_id", client.id,
		)
	}
}
