package grpc

import (
	"context"
	"io"
	"sync"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/budget"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LLMServer is the core-side gRPC server that routes LLM requests to provider plugins.
// It implements protocol.LLMServiceServer and holds LLMServiceClient connections
// to provider plugins that registered a separate LLMService on their gRPC server.
//
// Queuing: a priority-aware concurrency semaphore limits how many calls reach
// the provider simultaneously. Callers that don't get a slot block until one
// opens, served in priority order (lower value = higher priority).
// Rate limiting reuses Pulse's budget.Limiter (sliding window, calls/minute).
type LLMServer struct {
	protocol.UnimplementedLLMServiceServer

	mu              sync.RWMutex
	providers       map[string]protocol.LLMServiceClient // provider name → client
	defaultProvider string
	queue           *llmQueue
	limiter         *budget.Limiter
	logger          *zap.SugaredLogger
}

// NewLLMServer creates a new LLM routing server. Starts empty — providers register after init.
func NewLLMServer(cfg am.LLMConfig, logger *zap.SugaredLogger) *LLMServer {
	return &LLMServer{
		providers: make(map[string]protocol.LLMServiceClient),
		queue:     newLLMQueue(cfg.MaxConcurrent, cfg.MaxQueueDepth),
		limiter:   budget.NewLimiter(cfg.MaxCallsPerMinute),
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

	if err := s.gate(ctx, req.GetPriority()); err != nil {
		return nil, err
	}
	defer s.queue.Release()

	resp, err := client.Chat(ctx, req)
	if err != nil {
		return nil, errors.Wrapf(err, "LLM chat via provider %s failed", providerName)
	}

	return resp, nil
}

// StreamChat implements protocol.LLMServiceServer — routes a streaming LLM request
// to the provider plugin and forwards chunks back to the calling plugin.
func (s *LLMServer) StreamChat(req *protocol.LLMChatRequest, srv protocol.LLMService_StreamChatServer) error {
	clientStream, release, err := s.StreamChatClient(srv.Context(), req)
	if err != nil {
		return err
	}
	defer release()
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
// Returns a client-side stream and a release function. The caller MUST call release
// when the stream is fully consumed to return the concurrency slot.
func (s *LLMServer) StreamChatClient(ctx context.Context, req *protocol.LLMChatRequest) (protocol.LLMService_StreamChatClient, func(), error) {
	// Look up provider under lock, then release before streaming —
	// inference can run for seconds and must not block provider registration.
	client, providerName, err := s.resolveProvider(req.Provider)
	if err != nil {
		return nil, nil, err
	}

	if err := s.gate(ctx, req.GetPriority()); err != nil {
		return nil, nil, err
	}

	stream, err := client.StreamChat(ctx, req)
	if err != nil {
		s.queue.Release()
		return nil, nil, errors.Wrapf(err, "LLM stream chat via provider %s failed", providerName)
	}

	return stream, s.queue.Release, nil
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

// gate checks rate limit then acquires a concurrency slot (priority-ordered).
// Returns gRPC RESOURCE_EXHAUSTED for rate limit and queue-full rejections
// so callers can distinguish "retry later" from "provider error".
func (s *LLMServer) gate(ctx context.Context, priority int32) error {
	if err := s.limiter.Wait(ctx); err != nil {
		return status.Errorf(codes.ResourceExhausted, "LLM rate limit: %v", err)
	}
	active, queued := s.queue.Stats()
	if queued > 0 {
		s.logger.Infow("LLM request queued", "priority", priority, "active", active, "queued", queued)
	}
	if err := s.queue.Acquire(ctx, priority); err != nil {
		return status.Errorf(codes.ResourceExhausted, "LLM queue: %v", err)
	}
	return nil
}

// providerNames returns registered provider names (must be called with lock held).
func (s *LLMServer) providerNames() []string {
	names := make([]string, 0, len(s.providers))
	for name := range s.providers {
		names = append(names, name)
	}
	return names
}
