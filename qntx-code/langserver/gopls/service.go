package gopls

import (
	"context"
	"sync"

	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

// ErrServiceNotInitialized is returned when gopls operations are called before Initialize
var ErrServiceNotInitialized = errors.New("gopls service not initialized (call Initialize first)")

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
		return nil, errors.New("gopls service requires a logger")
	}

	client, err := NewStdioClient()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create gopls stdio client")
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
		return errors.Wrapf(err, "failed to initialize gopls for workspace %s", s.workspaceRoot)
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

	// Try graceful shutdown first
	shutdownErr := s.client.Shutdown(ctx)

	// If graceful shutdown failed or timed out, force kill the process
	if shutdownErr != nil {
		s.logger.Warnw("Graceful gopls shutdown failed, attempting force kill",
			"error", shutdownErr,
			"workspace", s.workspaceRoot,
		)

		// Try to force kill the process if it's a StdioClient
		if stdioClient, ok := s.client.(*StdioClient); ok {
			if killErr := stdioClient.ForceKill(); killErr != nil {
				s.logger.Errorw("Failed to force kill gopls process",
					"shutdown_error", shutdownErr,
					"kill_error", killErr,
				)
				return errors.Wrapf(shutdownErr, "gopls shutdown failed and force kill also failed (kill err: %v)", killErr)
			}
			s.logger.Infow("gopls process force killed after failed graceful shutdown")
		}

		// Mark as not initialized even if shutdown failed
		s.initialized = false
		return errors.Wrap(shutdownErr, "gopls required force kill after failed graceful shutdown")
	}

	s.initialized = false
	s.logger.Infow("gopls service shut down cleanly")
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
		return nil, ErrServiceNotInitialized
	}

	return s.client.GoToDefinition(ctx, uri, pos)
}

// FindReferences finds all references to a symbol
func (s *Service) FindReferences(ctx context.Context, uri string, pos Position, includeDeclaration bool) ([]Location, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, ErrServiceNotInitialized
	}

	return s.client.FindReferences(ctx, uri, pos, includeDeclaration)
}

// GetHover returns hover information for a position
func (s *Service) GetHover(ctx context.Context, uri string, pos Position) (*Hover, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, ErrServiceNotInitialized
	}

	return s.client.GetHover(ctx, uri, pos)
}

// GetDiagnostics returns diagnostics for a document
func (s *Service) GetDiagnostics(ctx context.Context, uri string) ([]Diagnostic, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, ErrServiceNotInitialized
	}

	return s.client.GetDiagnostics(ctx, uri)
}

// ListDocumentSymbols returns all symbols in a document
func (s *Service) ListDocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, ErrServiceNotInitialized
	}

	return s.client.ListDocumentSymbols(ctx, uri)
}

// FormatDocument returns formatting edits for a document
func (s *Service) FormatDocument(ctx context.Context, uri string) ([]TextEdit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, ErrServiceNotInitialized
	}

	return s.client.FormatDocument(ctx, uri)
}
