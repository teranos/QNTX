package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// startATSStoreServer starts an ATSStoreService gRPC server for testing
func startATSStoreServer(t *testing.T, store ats.AttestationStore, authToken string) (string, func()) {
	t.Helper()
	logger := zaptest.NewLogger(t).Sugar()
	server := NewATSStoreServer(store, authToken, logger)

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	protocol.RegisterATSStoreServiceServer(grpcServer, server)

	go func() {
		grpcServer.Serve(listener)
	}()

	time.Sleep(50 * time.Millisecond)

	cleanup := func() {
		grpcServer.Stop()
		listener.Close()
	}

	return listener.Addr().String(), cleanup
}

func TestRemoteATSStore_CreateAttestation(t *testing.T) {
	store, _ := qntxtest.CreateTestStore(t)
	logger := zaptest.NewLogger(t).Sugar()

	authToken := "test-token"
	addr, cleanup := startATSStoreServer(t, store, authToken)
	defer cleanup()

	ctx := context.Background()
	client, err := NewRemoteATSStore(ctx, addr, authToken, logger)
	require.NoError(t, err)
	defer client.Close()

	// Create an attestation
	attestation := &types.As{
		ID:         "test-id-123",
		Subjects:   []string{"alice"},
		Predicates: []string{"knows"},
		Contexts:   []string{"TEST"},
		Actors:     []string{"test@user"},
		Timestamp:  time.Now(),
		Source:     "test",
		Attributes: map[string]interface{}{"key": "value"},
		CreatedAt:  time.Now(),
	}

	err = client.CreateAttestation(attestation)
	require.NoError(t, err)

	// Verify it exists
	exists := client.AttestationExists("test-id-123")
	assert.True(t, exists)
}

func TestATSStoreServer_GenerateAndCreate_NilCommand(t *testing.T) {
	store, _ := qntxtest.CreateTestStore(t)

	authToken := "test-token"
	addr, cleanup := startATSStoreServer(t, store, authToken)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := protocol.NewATSStoreServiceClient(conn)

	// Send request with nil command — must return error, not panic/hang
	resp, err := client.GenerateAndCreateAttestation(ctx, &protocol.GenerateAttestationRequest{
		AuthToken: authToken,
		Command:   nil,
	})
	require.NoError(t, err, "RPC should not return transport error")
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "nil")
}

func TestATSStoreServer_GenerateAndCreate_ValidCommand(t *testing.T) {
	store, _ := qntxtest.CreateTestStore(t)

	authToken := "test-token"
	addr, cleanup := startATSStoreServer(t, store, authToken)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	client := protocol.NewATSStoreServiceClient(conn)

	resp, err := client.GenerateAndCreateAttestation(ctx, &protocol.GenerateAttestationRequest{
		AuthToken: authToken,
		Command: &protocol.AttestationCommand{
			Subjects:   []string{"test-subject"},
			Predicates: []string{"test-predicate"},
			Contexts:   []string{"test-context"},
			Actors:     []string{"test-actor"},
			Source:     "test",
		},
	})
	require.NoError(t, err, "RPC should not return transport error")
	assert.True(t, resp.Success, "expected success, got error: %s", resp.Error)
	assert.NotEmpty(t, resp.Attestation.Id)
}

func TestRemoteATSStore_CreateAttestation_InvalidToken(t *testing.T) {
	store, _ := qntxtest.CreateTestStore(t)
	logger := zaptest.NewLogger(t).Sugar()

	authToken := "correct-token"
	addr, cleanup := startATSStoreServer(t, store, authToken)
	defer cleanup()

	ctx := context.Background()
	client, err := NewRemoteATSStore(ctx, addr, "wrong-token", logger)
	require.NoError(t, err)
	defer client.Close()

	attestation := &types.As{
		ID:         "test-id-456",
		Subjects:   []string{"bob"},
		Predicates: []string{"likes"},
		Contexts:   []string{"TEST"},
		Actors:     []string{"test@user"},
		Timestamp:  time.Now(),
		Source:     "test",
		CreatedAt:  time.Now(),
	}

	err = client.CreateAttestation(attestation)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid authentication token")
}
