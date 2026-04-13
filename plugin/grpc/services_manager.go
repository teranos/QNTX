package grpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// ServiceEndpoints holds the addresses of running service servers
type ServiceEndpoints struct {
	ATSStoreAddress     string
	QueueAddress        string
	ScheduleAddress     string
	FileServiceAddress  string
	LLMAddress          string
	EmbeddingAddress    string
	VectorSearchAddress string
	GroundAddress       string
	SearchAddress       string
	AuthToken           string
}

// ServicesManager manages gRPC service servers for plugin callbacks
type ServicesManager struct {
	atsStoreServer     *grpc.Server
	queueServer        *grpc.Server
	scheduleServer     *grpc.Server
	fileServiceServer  *grpc.Server
	llmServer          *grpc.Server
	llmRouter          *LLMServer // Exposed for provider registration after plugin init
	llmConfig          am.LLMConfig
	embeddingServer    *grpc.Server
	embeddingRouter    *EmbeddingServer // Exposed for late backend registration
	vectorSearchServer *grpc.Server
	vectorSearchRouter *VectorSearchServer // Exposed for provider registration after plugin init
	groundServer       *grpc.Server
	groundDBPath       string
	searchServer       *grpc.Server
	searchRouter       *SearchServer // Exposed for provider registration after plugin init
	endpoints          ServiceEndpoints
	logger             *zap.SugaredLogger
}

// NewServicesManager creates a new services manager
func NewServicesManager(llmCfg am.LLMConfig, logger *zap.SugaredLogger) *ServicesManager {
	return &ServicesManager{
		llmConfig: llmCfg,
		logger:    logger,
	}
}

// Start starts the gRPC service servers with dynamic port allocation.
// filesDir is the path to stored files (for the FileService).
// groundDBPath is the path to Ground's SQLite database (empty = Ground service disabled).
func (m *ServicesManager) Start(ctx context.Context, store ats.AttestationStore, queue *async.Queue, scheduleStore *schedule.Store, filesDir string, groundDBPath string) (*ServiceEndpoints, error) {
	m.groundDBPath = groundDBPath
	// Generate authentication token
	authToken, err := generateAuthToken()
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate auth token for plugin services")
	}

	// Start ATSStore service
	atsStoreAddr, err := m.startATSStoreService(ctx, store, authToken)
	if err != nil {
		return nil, errors.Wrap(err, "failed to start ATS store service")
	}

	// Start Queue service
	queueAddr, err := m.startQueueService(ctx, queue, authToken)
	if err != nil {
		if m.atsStoreServer != nil {
			m.atsStoreServer.Stop()
		}
		return nil, errors.Wrap(err, "failed to start queue service")
	}

	// Start Schedule service
	scheduleAddr, err := m.startScheduleService(ctx, scheduleStore, authToken)
	if err != nil {
		if m.atsStoreServer != nil {
			m.atsStoreServer.Stop()
		}
		if m.queueServer != nil {
			m.queueServer.Stop()
		}
		return nil, errors.Wrap(err, "failed to start schedule service")
	}

	// Start File service
	fileServiceAddr, err := m.startFileService(ctx, filesDir, authToken)
	if err != nil {
		if m.atsStoreServer != nil {
			m.atsStoreServer.Stop()
		}
		if m.queueServer != nil {
			m.queueServer.Stop()
		}
		if m.scheduleServer != nil {
			m.scheduleServer.Stop()
		}
		return nil, errors.Wrap(err, "failed to start file service")
	}

	// Start LLM service (starts empty, providers register after plugin init)
	llmAddr, err := m.startLLMService(ctx, store)
	if err != nil {
		m.logger.Warnw("Failed to start LLM service, plugins will not have LLM access", "error", err)
		llmAddr = ""
	}

	// Start Embedding service (starts empty, backend registers after embedding engine init)
	embeddingAddr, err := m.startEmbeddingService(ctx, authToken)
	if err != nil {
		m.logger.Warnw("Failed to start Embedding service, plugins will not have embedding access", "error", err)
		embeddingAddr = ""
	}

	// Start VectorSearch service (starts empty, provider registers after plugin init)
	vectorSearchAddr, err := m.startVectorSearchService(ctx, authToken)
	if err != nil {
		m.logger.Warnw("Failed to start VectorSearch service, plugins will not have vector search access", "error", err)
		vectorSearchAddr = ""
	}

	// Start Ground service (only if ground_db_path is configured)
	groundAddr := ""
	if groundDBPath != "" {
		var groundErr error
		groundAddr, groundErr = m.startGroundService(ctx, groundDBPath, authToken)
		if groundErr != nil {
			m.logger.Warnw("Failed to start Ground service, plugins will not have Ground access", "error", groundErr)
			groundAddr = ""
		}
	}

	// Start Search service (starts empty, provider registers after plugin init)
	searchAddr, err := m.startSearchService(ctx)
	if err != nil {
		m.logger.Warnw("Failed to start Search service, plugins will not have search access", "error", err)
		searchAddr = ""
	}

	m.endpoints = ServiceEndpoints{
		ATSStoreAddress:     atsStoreAddr,
		QueueAddress:        queueAddr,
		ScheduleAddress:     scheduleAddr,
		FileServiceAddress:  fileServiceAddr,
		LLMAddress:          llmAddr,
		EmbeddingAddress:    embeddingAddr,
		VectorSearchAddress: vectorSearchAddr,
		GroundAddress:       groundAddr,
		SearchAddress:       searchAddr,
		AuthToken:           authToken,
	}

	return &m.endpoints, nil
}

// startATSStoreService starts the ATSStore gRPC service
func (m *ServicesManager) startATSStoreService(ctx context.Context, store ats.AttestationStore, authToken string) (string, error) {
	// Listen on dynamic port
	// Use explicit IPv4 127.0.0.1 instead of "localhost" to avoid IPv6 [::1] resolution
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	// Create gRPC server
	m.atsStoreServer = grpc.NewServer()
	atsServer := NewATSStoreServer(store, authToken, m.logger)
	protocol.RegisterATSStoreServiceServer(m.atsStoreServer, atsServer)

	// Handle context cancellation for graceful shutdown
	go func() {
		<-ctx.Done()
		m.logger.Debug("Context cancelled, stopping ATSStore service")
		m.atsStoreServer.GracefulStop()
	}()

	// Start serving in background
	go func() {
		if err := m.atsStoreServer.Serve(listener); err != nil {
			m.logger.Errorw("ATSStore service error", "error", err)
		}
	}()

	addr := listener.Addr().String()
	m.logger.Infow("ATSStore service started", "address", addr)

	return addr, nil
}

// startQueueService starts the Queue gRPC service
func (m *ServicesManager) startQueueService(ctx context.Context, queue *async.Queue, authToken string) (string, error) {
	// Listen on dynamic port
	// Use explicit IPv4 127.0.0.1 instead of "localhost" to avoid IPv6 [::1] resolution
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	// Create gRPC server
	m.queueServer = grpc.NewServer()
	queueServer := NewQueueServer(queue, authToken, m.logger)
	protocol.RegisterQueueServiceServer(m.queueServer, queueServer)

	// Handle context cancellation for graceful shutdown
	go func() {
		<-ctx.Done()
		m.logger.Debug("Context cancelled, stopping Queue service")
		m.queueServer.GracefulStop()
	}()

	// Start serving in background
	go func() {
		if err := m.queueServer.Serve(listener); err != nil {
			m.logger.Errorw("Queue service error", "error", err)
		}
	}()

	addr := listener.Addr().String()
	m.logger.Infow("Queue service started", "address", addr)

	return addr, nil
}

// startScheduleService starts the Schedule gRPC service
func (m *ServicesManager) startScheduleService(ctx context.Context, store *schedule.Store, authToken string) (string, error) {
	// Listen on dynamic port
	// Use explicit IPv4 127.0.0.1 instead of "localhost" to avoid IPv6 [::1] resolution
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	// Create gRPC server
	m.scheduleServer = grpc.NewServer()
	schedServer := NewScheduleServer(store, authToken, m.logger)
	protocol.RegisterScheduleServiceServer(m.scheduleServer, schedServer)

	// Handle context cancellation for graceful shutdown
	go func() {
		<-ctx.Done()
		m.logger.Debug("Context cancelled, stopping Schedule service")
		m.scheduleServer.GracefulStop()
	}()

	// Start serving in background
	go func() {
		if err := m.scheduleServer.Serve(listener); err != nil {
			m.logger.Errorw("Schedule service error", "error", err)
		}
	}()

	addr := listener.Addr().String()
	m.logger.Infow("Schedule service started", "address", addr)

	return addr, nil
}

// startFileService starts the File gRPC service
func (m *ServicesManager) startFileService(ctx context.Context, filesDir string, authToken string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	m.fileServiceServer = grpc.NewServer()
	fileServer := NewFileServiceServer(filesDir, authToken, m.logger)
	protocol.RegisterFileServiceServer(m.fileServiceServer, fileServer)

	go func() {
		<-ctx.Done()
		m.logger.Debug("Context cancelled, stopping File service")
		m.fileServiceServer.GracefulStop()
	}()

	go func() {
		if err := m.fileServiceServer.Serve(listener); err != nil {
			m.logger.Errorw("File service error", "error", err)
		}
	}()

	addr := listener.Addr().String()
	m.logger.Infow("File service started", "address", addr)

	return addr, nil
}

// startLLMService starts the LLM routing gRPC service.
// The server starts empty — providers register after their own initialization completes.
func (m *ServicesManager) startLLMService(ctx context.Context, store ats.AttestationStore) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	m.llmRouter = NewLLMServer(m.llmConfig, store, m.logger)
	m.llmServer = grpc.NewServer()
	protocol.RegisterLLMServiceServer(m.llmServer, m.llmRouter)

	go func() {
		<-ctx.Done()
		m.logger.Debug("Context cancelled, stopping LLM service")
		m.llmServer.GracefulStop()
	}()

	go func() {
		if err := m.llmServer.Serve(listener); err != nil {
			m.logger.Errorw("LLM service error", "error", err)
		}
	}()

	addr := listener.Addr().String()
	m.logger.Infow("LLM service started", "address", addr)

	return addr, nil
}

// startEmbeddingService starts the Embedding gRPC service.
// The server starts empty — the backend registers after the embedding engine initializes.
func (m *ServicesManager) startEmbeddingService(ctx context.Context, authToken string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	m.embeddingRouter = NewEmbeddingServer(authToken, m.logger)
	m.embeddingServer = grpc.NewServer()
	protocol.RegisterEmbeddingServiceServer(m.embeddingServer, m.embeddingRouter)

	go func() {
		<-ctx.Done()
		m.logger.Debug("Context cancelled, stopping Embedding service")
		m.embeddingServer.GracefulStop()
	}()

	go func() {
		if err := m.embeddingServer.Serve(listener); err != nil {
			m.logger.Errorw("Embedding service error", "error", err)
		}
	}()

	addr := listener.Addr().String()
	m.logger.Infow("Embedding service started", "address", addr)

	return addr, nil
}

// startVectorSearchService starts the VectorSearch routing gRPC service.
// The server starts empty — the provider registers after its own initialization completes.
func (m *ServicesManager) startVectorSearchService(ctx context.Context, authToken string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	m.vectorSearchRouter = NewVectorSearchServer(authToken, m.logger)
	m.vectorSearchServer = grpc.NewServer()
	protocol.RegisterVectorSearchServiceServer(m.vectorSearchServer, m.vectorSearchRouter)

	go func() {
		<-ctx.Done()
		m.logger.Debug("Context cancelled, stopping VectorSearch service")
		m.vectorSearchServer.GracefulStop()
	}()

	go func() {
		if err := m.vectorSearchServer.Serve(listener); err != nil {
			m.logger.Errorw("VectorSearch service error", "error", err)
		}
	}()

	addr := listener.Addr().String()
	m.logger.Infow("VectorSearch service started", "address", addr)

	return addr, nil
}

// startGroundService starts the Ground gRPC service
func (m *ServicesManager) startGroundService(ctx context.Context, dbPath string, authToken string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	m.groundServer = grpc.NewServer()
	groundServer := NewGroundServer(dbPath, authToken, m.logger)
	protocol.RegisterGroundServiceServer(m.groundServer, groundServer)

	go func() {
		<-ctx.Done()
		m.logger.Debug("Context cancelled, stopping Ground service")
		m.groundServer.GracefulStop()
	}()

	go func() {
		if err := m.groundServer.Serve(listener); err != nil {
			m.logger.Errorw("Ground service error", "error", err)
		}
	}()

	addr := listener.Addr().String()
	m.logger.Infow("Ground service started", "address", addr)

	return addr, nil
}

// startSearchService starts the Search routing gRPC service.
// The server starts empty — the provider registers after its own initialization completes.
func (m *ServicesManager) startSearchService(ctx context.Context) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	m.searchRouter = NewSearchServer(m.logger)
	m.searchServer = grpc.NewServer()
	protocol.RegisterSearchServiceServer(m.searchServer, m.searchRouter)

	go func() {
		<-ctx.Done()
		m.logger.Debug("Context cancelled, stopping Search service")
		m.searchServer.GracefulStop()
	}()

	go func() {
		if err := m.searchServer.Serve(listener); err != nil {
			m.logger.Errorw("Search service error", "error", err)
		}
	}()

	addr := listener.Addr().String()
	m.logger.Infow("Search service started", "address", addr)

	return addr, nil
}

// GetSearchRouter returns the search router for provider registration.
// Returns nil if the search service is not running.
func (m *ServicesManager) GetSearchRouter() *SearchServer {
	return m.searchRouter
}

// GetLLMRouter returns the LLM router for provider registration.
// Returns nil if the LLM service is not running.
func (m *ServicesManager) GetLLMRouter() *LLMServer {
	return m.llmRouter
}

// GetEmbeddingRouter returns the embedding router for backend registration.
// Returns nil if the embedding service is not running.
func (m *ServicesManager) GetEmbeddingRouter() *EmbeddingServer {
	return m.embeddingRouter
}

// GetVectorSearchRouter returns the vector search router for provider registration.
// Returns nil if the vector search service is not running.
func (m *ServicesManager) GetVectorSearchRouter() *VectorSearchServer {
	return m.vectorSearchRouter
}

// Shutdown gracefully stops all service servers
func (m *ServicesManager) Shutdown() {
	m.logger.Info("Shutting down plugin services")

	if m.atsStoreServer != nil {
		m.atsStoreServer.GracefulStop()
	}

	if m.queueServer != nil {
		m.queueServer.GracefulStop()
	}

	if m.scheduleServer != nil {
		m.scheduleServer.GracefulStop()
	}

	if m.fileServiceServer != nil {
		m.fileServiceServer.GracefulStop()
	}

	if m.llmServer != nil {
		m.llmServer.GracefulStop()
	}

	if m.embeddingServer != nil {
		m.embeddingServer.GracefulStop()
	}

	if m.vectorSearchServer != nil {
		m.vectorSearchServer.GracefulStop()
	}

	if m.groundServer != nil {
		m.groundServer.GracefulStop()
	}

	if m.searchServer != nil {
		m.searchServer.GracefulStop()
	}

	m.logger.Info("Plugin services stopped")
}

// GetEndpoints returns the service endpoints
func (m *ServicesManager) GetEndpoints() *ServiceEndpoints {
	return &m.endpoints
}

// generateAuthToken generates a random authentication token
func generateAuthToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
