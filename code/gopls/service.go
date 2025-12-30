package gopls

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// Service provides gopls language intelligence for Go code
// Wraps the gopls client with lifecycle management and LSP-oriented API
type Service struct {
	client        Client
	workspaceRoot string
	logger        *zap.SugaredLogger
	mu            sync.RWMutex
	initialized   bool
}

// Config holds gopls service configuration
type Config struct {
	WorkspaceRoot string
	Logger        *zap.SugaredLogger
}

// NewService creates a new gopls service
func NewService(cfg Config) (*Service, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	client, err := NewStdioClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create gopls client: %w", err)
	}

	return &Service{
		client:        client,
		workspaceRoot: cfg.WorkspaceRoot,
		logger:        cfg.Logger,
	}, nil
}

// Initialize starts the gopls service with the configured workspace
func (s *Service) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		return nil
	}

	if err := s.client.Initialize(ctx, s.workspaceRoot); err != nil {
		return fmt.Errorf("failed to initialize gopls: %w", err)
	}

	s.initialized = true
	s.logger.Infow("gopls service initialized", "workspace", s.workspaceRoot)
	return nil
}

// Shutdown stops the gopls service
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.initialized {
		return nil
	}

	if err := s.client.Shutdown(ctx); err != nil {
		s.logger.Warnw("gopls shutdown error", "error", err)
		return err
	}

	s.initialized = false
	s.logger.Infow("gopls service shut down")
	return nil
}

// IsInitialized returns whether the service has been initialized
func (s *Service) IsInitialized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.initialized
}

// GoToDefinition finds the definition of a symbol
func (s *Service) GoToDefinition(ctx context.Context, uri string, pos Position) ([]Location, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, fmt.Errorf("service not initialized")
	}

	return s.client.GoToDefinition(ctx, uri, pos)
}

// FindReferences finds all references to a symbol
func (s *Service) FindReferences(ctx context.Context, uri string, pos Position, includeDeclaration bool) ([]Location, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, fmt.Errorf("service not initialized")
	}

	return s.client.FindReferences(ctx, uri, pos, includeDeclaration)
}

// GetHover returns hover information for a position
func (s *Service) GetHover(ctx context.Context, uri string, pos Position) (*Hover, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, fmt.Errorf("service not initialized")
	}

	return s.client.GetHover(ctx, uri, pos)
}

// GetDiagnostics returns diagnostics for a document
func (s *Service) GetDiagnostics(ctx context.Context, uri string) ([]Diagnostic, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, fmt.Errorf("service not initialized")
	}

	return s.client.GetDiagnostics(ctx, uri)
}

// ListDocumentSymbols returns all symbols in a document
func (s *Service) ListDocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, fmt.Errorf("service not initialized")
	}

	return s.client.ListDocumentSymbols(ctx, uri)
}

// FormatDocument returns formatting edits for a document
func (s *Service) FormatDocument(ctx context.Context, uri string) ([]TextEdit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, fmt.Errorf("service not initialized")
	}

	return s.client.FormatDocument(ctx, uri)
}
