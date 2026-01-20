package server

// refreshGraphFromDatabase refreshes the graph visualization from recent attestations
// Called after operations that modify the database (e.g., changing graph limit)
func (s *QNTXServer) refreshGraphFromDatabase() {
	limit := int(s.graphLimit.Load())
	s.logger.Infow("Refreshing graph from database", "limit", limit)

	// Build graph from recent attestations (limit configurable from frontend)
	// Uses server's context for cancellation during shutdown
	g, err := s.builder.BuildFromRecentAttestations(s.ctx, limit)
	if err != nil {
		s.logger.Errorw("Failed to build graph from recent attestations", "error", err)
		return
	}

	// Broadcast updated graph to all connected clients
	s.broadcast <- g
}
