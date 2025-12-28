package server

import "context"

// refreshGraphFromDatabase refreshes the graph visualization from recent attestations
// Called after operations that modify the database (e.g., changing graph limit)
func (s *QNTXServer) refreshGraphFromDatabase() {
	limit := int(s.graphLimit.Load())
	s.logger.Infow("Refreshing graph from database", "limit", limit)

	// Build graph from recent attestations (limit configurable from frontend)
	g, err := s.builder.BuildFromRecentAttestations(context.Background(), limit)
	if err != nil {
		s.logger.Errorw("Failed to build graph from recent attestations", "error", err)
		return
	}

	// Broadcast updated graph to all connected clients
	s.broadcast <- g
}
