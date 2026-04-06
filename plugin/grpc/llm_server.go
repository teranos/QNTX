package grpc

import (
	"context"
	"io"
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

// HasProvider returns true if the named provider is registered.
func (s *LLMServer) HasProvider(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.providers[name]
	return ok
}

// Chat routes an LLM chat request to the appropriate provider plugin.
func (s *LLMServer) Chat(ctx context.Context, req *protocol.LLMChatRequest) (*protocol.LLMChatResponse, error) {
	client, providerName, err := s.resolveProvider(req.Provider)
	if err != nil {
		return nil, err
	}

	resp, err := client.Chat(ctx, req)
	if err != nil {
		return nil, errors.Wrapf(err, "LLM chat via provider %s failed", providerName)
	}

	return resp, nil
}

// StreamChat implements protocol.LLMServiceServer — routes a streaming LLM request
// to the provider plugin and forwards chunks back to the calling plugin.
func (s *LLMServer) StreamChat(req *protocol.LLMChatRequest, srv protocol.LLMService_StreamChatServer) error {
	clientStream, err := s.StreamChatClient(srv.Context(), req)
	if err != nil {
		return err
	}
	for {
		chunk, err := clientStream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := srv.Send(chunk); err != nil {
			return err
		}
	}
}

// StreamChatClient routes a streaming LLM request to the appropriate provider plugin.
// Returns a client-side stream of LLMChatChunk messages (one per token).
// Named StreamChatClient to avoid conflicting with the server-side StreamChat method.
func (s *LLMServer) StreamChatClient(ctx context.Context, req *protocol.LLMChatRequest) (protocol.LLMService_StreamChatClient, error) {
	// Look up provider under lock, then release before streaming —
	// inference can run for seconds and must not block provider registration.
	client, providerName, err := s.resolveProvider(req.Provider)
	if err != nil {
		return nil, err
	}

	stream, err := client.StreamChat(ctx, req)
	if err != nil {
		return nil, errors.Wrapf(err, "LLM stream chat via provider %s failed", providerName)
	}

	return stream, nil
}

// resolveProvider returns the LLM client for the given provider name (or default).
func (s *LLMServer) resolveProvider(name string) (protocol.LLMServiceClient, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.providers) == 0 {
		return nil, "", errors.New("no LLM providers registered")
	}

	if name == "" {
		name = s.defaultProvider
	}

	client, ok := s.providers[name]
	if !ok {
		return nil, "", errors.Newf("LLM provider %q not found (available: %v)", name, s.providerNames())
	}

	return client, name, nil
}

// providerNames returns registered provider names (must be called with lock held).
func (s *LLMServer) providerNames() []string {
	names := make([]string, 0, len(s.providers))
	for name := range s.providers {
		names = append(names, name)
	}
	return names
}
