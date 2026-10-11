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
	"github.com/teranos/QNTX/internal/named"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// startATSStoreServer starts an ATSStoreService gRPC server for testing
func startATSStoreServer(t *testing.T, store ats.AttestationStore, authToken string) (string, func()) {
	t.Helper()
	logger := zaptest.NewLogger(t).Sugar()
	server := services.NewATSStoreServer(store, authToken, "did:key:znode", logger)

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
		Attributes: map[string]any{"key": "value"},
		CreatedAt:  time.Now(),
	}

	err = client.CreateAttestation(attestation)
	require.NoError(t, err)

	// Verify it exists
	exists := client.AttestationExists("test-id-123")
	assert.True(t, exists)
}

// "nil is nil": a plugin's command names the version it is stamped with, its
// own or the registry's; one naming neither is not sent.
func TestRemoteATSStore_ACommandNamesItsVersion(t *testing.T) {
	store, _ := qntxtest.CreateTestStore(t)
	addr, cleanup := startATSStoreServer(t, store, "test-token")
	defer cleanup()
	client, err := NewRemoteATSStore(context.Background(), addr, "test-token", zaptest.NewLogger(t).Sugar())
	require.NoError(t, err)
	defer client.Close()

	version, err := named.TextOf("version", "1.2.3")
	require.NoError(t, err)
	as, err := client.GenerateAndCreateAttestation(context.Background(), &types.AsCommand{
		Subjects: []string{"alice"}, By: types.ActorsNamed{Actors: named.ItemOf("test@user")}, When: types.Now{}, Source: "test",
		SourceVersion: types.VersionNamed{Version: version},
	})
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", as.Attributes["source_version"])

	_, err = client.GenerateAndCreateAttestation(context.Background(), &types.AsCommand{
		Subjects: []string{"alice"}, By: types.ActorsNamed{Actors: named.ItemOf("test@user")}, When: types.Now{}, Source: "test",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no version")
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
	assert.Contains(t, resp.Error, "source of the command")
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
			By:         &protocol.AttestationCommand_ActorsNamed{ActorsNamed: &protocol.ActorNames{Names: []string{"test-actor"}}},
			When:       &protocol.AttestationCommand_Now{Now: true},
			Source:     "test",
			Version:    &protocol.AttestationCommand_SourceVersion{SourceVersion: "1.0.0"},
		},
	})
	require.NoError(t, err, "RPC should not return transport error")
	require.True(t, resp.Success, "expected success, got error: %s", resp.Error)
	require.NotNil(t, resp.Attestation)
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
