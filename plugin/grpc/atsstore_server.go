package grpc

import (
	"context"
	"fmt"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// ATSStoreServer implements the ATSStoreService gRPC server
type ATSStoreServer struct {
	protocol.UnimplementedATSStoreServiceServer
	store     *storage.SQLStore
	authToken string
	logger    *zap.SugaredLogger
}

// NewATSStoreServer creates a new ATS store gRPC server
func NewATSStoreServer(store *storage.SQLStore, authToken string, logger *zap.SugaredLogger) *ATSStoreServer {
	return &ATSStoreServer{
		store:     store,
		authToken: authToken,
		logger:    logger,
	}
}


// CreateAttestation creates a new attestation
func (s *ATSStoreServer) CreateAttestation(ctx context.Context, req *protocol.CreateAttestationRequest) (*protocol.CreateAttestationResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.CreateAttestationResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	as := req.Attestation.ToTypes()

	if err := s.store.CreateAttestation(as); err != nil {
		return &protocol.CreateAttestationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create attestation: %v", err),
		}, nil
	}

	s.logger.Infow("Attestation created via gRPC", "id", as.ID, "subjects", as.Subjects)

	return &protocol.CreateAttestationResponse{
		Success: true,
	}, nil
}

// AttestationExists checks if an attestation exists
func (s *ATSStoreServer) AttestationExists(ctx context.Context, req *protocol.AttestationExistsRequest) (*protocol.AttestationExistsResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.AttestationExistsResponse{
			Exists: false,
		}, nil
	}

	exists := s.store.AttestationExists(req.Id)

	return &protocol.AttestationExistsResponse{
		Exists: exists,
	}, nil
}

// GenerateAndCreateAttestation generates an ID and creates an attestation
func (s *ATSStoreServer) GenerateAndCreateAttestation(ctx context.Context, req *protocol.GenerateAttestationRequest) (*protocol.GenerateAttestationResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.GenerateAttestationResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Convert protobuf command to types.AsCommand
	cmd, err := protoToCommand(req.Command)
	if err != nil {
		return &protocol.GenerateAttestationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to convert command: %v", err),
		}, nil
	}

	// Generate and create the attestation
	as, err := s.store.GenerateAndCreateAttestation(cmd)
	if err != nil {
		return &protocol.GenerateAttestationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to generate attestation: %v", err),
		}, nil
	}

	s.logger.Infow("Attestation generated and created via gRPC", "id", as.ID, "subjects", as.Subjects)

	return &protocol.GenerateAttestationResponse{
		Success:     true,
		Attestation: protocol.AttestationFromTypes(as),
	}, nil
}

// GetAttestations queries attestations with filters
func (s *ATSStoreServer) GetAttestations(ctx context.Context, req *protocol.GetAttestationsRequest) (*protocol.GetAttestationsResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.GetAttestationsResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Convert protobuf filter to ats.AttestationFilter
	filter := protoToFilter(req.Filter)

	// Query attestations
	attestations, err := s.store.GetAttestations(filter)
	if err != nil {
		return &protocol.GetAttestationsResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to query attestations: %v", err),
		}, nil
	}

	protoAttestations := make([]*protocol.Attestation, len(attestations))
	for i, as := range attestations {
		protoAttestations[i] = protocol.AttestationFromTypes(as)
	}

	return &protocol.GetAttestationsResponse{
		Success:      true,
		Attestations: protoAttestations,
	}, nil
}

func protoToCommand(proto *protocol.AttestationCommand) (*types.AsCommand, error) {
	attributes, err := attributesFromJSON(proto.AttributesJson)
	if err != nil {
		return nil, err
	}

	timestamp := time.Now()
	if proto.Timestamp != nil && *proto.Timestamp != 0 {
		timestamp = time.UnixMilli(*proto.Timestamp)
	}

	return &types.AsCommand{
		Subjects:   proto.Subjects,
		Predicates: proto.Predicates,
		Contexts:   proto.Contexts,
		Actors:     proto.Actors,
		Timestamp:  timestamp,
		Attributes: attributes,
	}, nil
}

func protoToFilter(proto *protocol.AttestationFilter) ats.AttestationFilter {
	limit := 0
	if proto.Limit != nil {
		limit = int(*proto.Limit)
	}
	filter := ats.AttestationFilter{
		Limit:      limit,
		Actors:     proto.Actors,
		Subjects:   proto.Subjects,
		Predicates: proto.Predicates,
		Contexts:   proto.Contexts,
	}

	if proto.TimeStart != 0 {
		t := time.UnixMilli(proto.TimeStart)
		filter.TimeStart = &t
	}

	if proto.TimeEnd != 0 {
		t := time.UnixMilli(proto.TimeEnd)
		filter.TimeEnd = &t
	}

	return filter
}
