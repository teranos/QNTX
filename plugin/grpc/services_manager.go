package grpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// ServiceEndpoints holds the addresses of running service servers
type ServiceEndpoints struct {
	ATSStoreAddress string
	QueueAddress    string
	AuthToken       string
}

// ServicesManager manages gRPC service servers for plugin callbacks
type ServicesManager struct {
	atsStoreServer *grpc.Server
	queueServer    *grpc.Server
	endpoints      ServiceEndpoints
	logger         *zap.SugaredLogger
}

// NewServicesManager creates a new services manager
func NewServicesManager(logger *zap.SugaredLogger) *ServicesManager {
	return &ServicesManager{
		logger: logger,
	}
}

// Start starts the gRPC service servers with dynamic port allocation
func (m *ServicesManager) Start(ctx context.Context, store *storage.SQLStore, queue *async.Queue) (*ServiceEndpoints, error) {
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
		// Cleanup ATS store service if queue fails
		if m.atsStoreServer != nil {
			m.atsStoreServer.Stop()
		}
		return nil, errors.Wrap(err, "failed to start queue service")
	}

	m.endpoints = ServiceEndpoints{
		ATSStoreAddress: atsStoreAddr,
		QueueAddress:    queueAddr,
		AuthToken:       authToken,
	}

	return &m.endpoints, nil
}

// startATSStoreService starts the ATSStore gRPC service
func (m *ServicesManager) startATSStoreService(ctx context.Context, store *storage.SQLStore, authToken string) (string, error) {
	// Listen on dynamic port
	listener, err := net.Listen("tcp", "localhost:0")
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
	listener, err := net.Listen("tcp", "localhost:0")
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

// Shutdown gracefully stops all service servers
func (m *ServicesManager) Shutdown() {
	m.logger.Info("Shutting down plugin services")

	if m.atsStoreServer != nil {
		m.atsStoreServer.GracefulStop()
	}

	if m.queueServer != nil {
		m.queueServer.GracefulStop()
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
