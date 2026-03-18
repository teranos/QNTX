package grpc

import (
	"context"
	"sync"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// LLMServer is the core-side gRPC server that routes LLM requests to provider plugins.
// It implements protocol.LLMServiceServer and holds LLMServiceClient connections
// to provider plugins that registered a separate LLMService on their gRPC server.
type LLMServer struct {
	protocol.UnimplementedLLMServiceServer

	mu              sync.RWMutex
	providers       map[string]protocol.LLMServiceClient // provider name → client
	defaultProvider string
	logger          *zap.SugaredLogger
}

// NewLLMServer creates a new LLM routing server. Starts empty — providers register after init.
func NewLLMServer(logger *zap.SugaredLogger) *LLMServer {
	return &LLMServer{
		providers: make(map[string]protocol.LLMServiceClient),
		logger:    logger,
	}
}

// RegisterProvider adds a provider plugin's client connection.
// If this is the first provider, it becomes the default.
func (s *LLMServer) RegisterProvider(name string, client protocol.LLMServiceClient) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.providers[name] = client
	if s.defaultProvider == "" {
		s.defaultProvider = name
	}
	s.logger.Infow("LLM provider registered", "provider", name, "is_default", s.defaultProvider == name)
}

// Chat routes an LLM chat request to the appropriate provider plugin.
func (s *LLMServer) Chat(ctx context.Context, req *protocol.LLMChatRequest) (*protocol.LLMChatResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.providers) == 0 {
		return nil, errors.New("no LLM providers registered")
	}

	providerName := req.Provider
	if providerName == "" {
		providerName = s.defaultProvider
	}

	client, ok := s.providers[providerName]
	if !ok {
		return nil, errors.Newf("LLM provider %q not found (available: %v)", providerName, s.providerNames())
	}

	resp, err := client.Chat(ctx, req)
	if err != nil {
		return nil, errors.Wrapf(err, "LLM chat via provider %s failed", providerName)
	}

	return resp, nil
}

// providerNames returns registered provider names (must be called with lock held).
func (s *LLMServer) providerNames() []string {
	names := make([]string, 0, len(s.providers))
	for name := range s.providers {
		names = append(names, name)
	}
	return names
}
