package services

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/budget"
	"github.com/teranos/errors"
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
	store           ats.AttestationStore // nil = weave creation disabled
	weaveCount      atomic.Int64         // accumulated weave count, drained by ticker
	logger          *zap.SugaredLogger
}

// NewLLMServer creates a new LLM routing server. Starts empty — providers register after init.
// store may be nil to disable weave attestation creation.
func NewLLMServer(cfg config.LLMConfig, store ats.AttestationStore, logger *zap.SugaredLogger) *LLMServer {
	return &LLMServer{
		providers: make(map[string]protocol.LLMServiceClient),
		queue:     newLLMQueue(cfg.MaxConcurrent, cfg.MaxQueueDepth, time.Duration(cfg.CooldownSeconds)*time.Second),
		store:     store,
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
	s.logger.Debugw("LLM provider registered", "provider", name, "is_default", s.defaultProvider == name)
}

// ClearProviders removes all providers. Called during server shutdown
// to prevent routing to dead gRPC connections.
func (s *LLMServer) ClearProviders() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.providers = make(map[string]protocol.LLMServiceClient)
	s.defaultProvider = ""
}

// UnregisterProvider removes a provider plugin's client connection.
// Called when an LLM provider plugin is disabled via hot-swap.
func (s *LLMServer) UnregisterProvider(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.providers, name)
	if s.defaultProvider == name {
		s.defaultProvider = ""
		// Promote another provider if available
		for k := range s.providers {
			s.defaultProvider = k
			break
		}
	}
	s.logger.Debugw("LLM provider unregistered", "provider", name, "new_default", s.defaultProvider)
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

	if err := s.gate(ctx, req.Priority); err != nil {
		return nil, err
	}
	defer s.queue.Release()

	resp, err := client.Chat(ctx, req)
	if err != nil {
		return nil, errors.Wrapf(err, "LLM chat via provider %s failed", providerName)
	}

	s.createWeave(ctx, req, providerName, resp.GetModel(), resp.GetContent(),
		int(resp.GetTotalTokens()), nil)

	return resp, nil
}

// StreamChat implements protocol.LLMServiceServer — routes a streaming LLM request
// to the provider plugin and forwards chunks back to the calling plugin.
// Accumulates token signals from each chunk and creates a weave attestation
// when the stream completes, so every provider gets observability for free.
func (s *LLMServer) StreamChat(req *protocol.LLMChatRequest, srv protocol.LLMService_StreamChatServer) error {
	_, providerName, err := s.resolveProvider(req.Provider)
	if err != nil {
		return err
	}

	clientStream, release, err := s.StreamChatClient(srv.Context(), req)
	if err != nil {
		return err
	}
	defer release()

	var (
		text      strings.Builder
		model     string
		totalToks int
		signals   []*protocol.TokenSignalProto
	)

	for {
		chunk, err := clientStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Accumulate for weave
		text.WriteString(chunk.GetToken())
		if chunk.GetModel() != "" {
			model = chunk.GetModel()
		}
		if chunk.GetDone() {
			totalToks = int(chunk.GetTotalTokens())
		}
		if chunk.GetSignal() != nil {
			signals = append(signals, chunk.GetSignal())
		}

		if err := srv.Send(chunk); err != nil {
			return err
		}
	}

	s.createWeave(srv.Context(), req, providerName, model, text.String(), totalToks, signals)

	return nil
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

	if err := s.gate(ctx, req.Priority); err != nil {
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
		// Stale config or typo — fall back to default provider instead of failing
		if name != s.defaultProvider && s.defaultProvider != "" {
			client, ok = s.providers[s.defaultProvider]
			if ok {
				return client, s.defaultProvider, nil
			}
		}
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

// DrainWeaveCounts atomically reads and resets the accumulated weave counter.
func (s *LLMServer) DrainWeaveCounts() int {
	return int(s.weaveCount.Swap(0))
}

// providerNames returns registered provider names (must be called with lock held).
func (s *LLMServer) providerNames() []string {
	names := make([]string, 0, len(s.providers))
	for name := range s.providers {
		names = append(names, name)
	}
	return names
}

// createWeave creates a Weave attestation capturing an LLM interaction.
// Runs asynchronously — failures are logged, never propagated to the caller.
func (s *LLMServer) createWeave(ctx context.Context, req *protocol.LLMChatRequest, provider, model, responseText string, totalTokens int, signals []*protocol.TokenSignalProto) {
	if s.store == nil {
		return
	}

	prompt := lastUserMessage(req)
	if prompt == "" && responseText == "" {
		return
	}

	attrs := map[string]interface{}{
		"prompt":       prompt,
		"text":         responseText,
		"model":        model,
		"token_count":  totalTokens,
		"weave_source": provider,
	}

	// Aggregate signal statistics
	if len(signals) > 0 {
		var confSum, entSum float64
		for _, sig := range signals {
			confSum += float64(sig.Confidence)
			entSum += float64(sig.Entropy)
		}
		n := float64(len(signals))
		attrs["mean_confidence"] = confSum / n
		attrs["mean_entropy"] = entSum / n

		// Pack per-token signal data
		tokens := make([]interface{}, 0, len(signals))
		for i, sig := range signals {
			tok := map[string]interface{}{
				"position":   i,
				"confidence": sig.Confidence,
				"entropy":    sig.Entropy,
				"top_gap":    sig.TopGap,
			}
			if len(sig.TopK) > 0 {
				topK := make([]interface{}, 0, len(sig.TopK))
				for _, c := range sig.TopK {
					topK = append(topK, map[string]interface{}{
						"text": c.Text,
						"prob": c.Prob,
					})
				}
				tok["top_k"] = topK
			}
			tokens = append(tokens, tok)
		}
		attrs["tokens"] = tokens
	}

	contextID := fmt.Sprintf("llm:%d", time.Now().UnixNano())

	cmd := &types.AsCommand{
		Subjects:   []string{"model:" + model},
		Predicates: []string{"Weave"},
		Contexts:   []string{contextID},
		Actors:     []string{provider},
		Source:     "llm",
		Attributes: attrs,
	}

	go func() {
		if _, err := s.store.GenerateAndCreateAttestation(ctx, cmd); err != nil {
			s.logger.Warnw("Failed to create weave attestation", "provider", provider, "model", model, "error", err)
		} else {
			s.weaveCount.Add(1)
		}
	}()
}

// lastUserMessage extracts the last user message from the request for the weave prompt.
func lastUserMessage(req *protocol.LLMChatRequest) string {
	// Prefer multi-turn messages field
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return req.Messages[i].Content
		}
	}
	// Fall back to deprecated single-turn field
	return req.UserPrompt
}
