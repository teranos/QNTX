package grpc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
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

// validateAuth checks the authentication token
func (s *ATSStoreServer) validateAuth(token string) error {
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) != 1 {
		return fmt.Errorf("invalid authentication token")
	}
	return nil
}

// CreateAttestation creates a new attestation
func (s *ATSStoreServer) CreateAttestation(ctx context.Context, req *protocol.CreateAttestationRequest) (*protocol.CreateAttestationResponse, error) {
	if err := s.validateAuth(req.AuthToken); err != nil {
		return &protocol.CreateAttestationResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Convert protobuf attestation to types.As
	as, err := protoToAttestation(req.Attestation)
	if err != nil {
		return &protocol.CreateAttestationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to convert attestation: %v", err),
		}, nil
	}

	// Create the attestation
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
	if err := s.validateAuth(req.AuthToken); err != nil {
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
	if err := s.validateAuth(req.AuthToken); err != nil {
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

	// Convert back to protobuf
	protoAs, err := attestationToProto(as)
	if err != nil {
		return &protocol.GenerateAttestationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to convert attestation to proto: %v", err),
		}, nil
	}

	return &protocol.GenerateAttestationResponse{
		Success:     true,
		Attestation: protoAs,
	}, nil
}

// GetAttestations queries attestations with filters
func (s *ATSStoreServer) GetAttestations(ctx context.Context, req *protocol.GetAttestationsRequest) (*protocol.GetAttestationsResponse, error) {
	if err := s.validateAuth(req.AuthToken); err != nil {
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

	// Convert results to protobuf
	protoAttestations := make([]*protocol.Attestation, len(attestations))
	for i, as := range attestations {
		protoAs, err := attestationToProto(as)
		if err != nil {
			return &protocol.GetAttestationsResponse{
				Success: false,
				Error:   fmt.Sprintf("failed to convert attestation: %v", err),
			}, nil
		}
		protoAttestations[i] = protoAs
	}

	return &protocol.GetAttestationsResponse{
		Success:      true,
		Attestations: protoAttestations,
	}, nil
}

// Helper functions for conversion

func protoToAttestation(proto *protocol.Attestation) (*types.As, error) {
	var attributes map[string]interface{}
	if proto.AttributesJson != "" {
		if err := json.Unmarshal([]byte(proto.AttributesJson), &attributes); err != nil {
			return nil, fmt.Errorf("failed to unmarshal attributes: %w", err)
		}
	}

	return &types.As{
		ID:         proto.Id,
		Subjects:   proto.Subjects,
		Predicates: proto.Predicates,
		Contexts:   proto.Contexts,
		Actors:     proto.Actors,
		Timestamp:  time.Unix(proto.Timestamp, 0),
		Source:     proto.Source,
		Attributes: attributes,
		CreatedAt:  time.Unix(proto.CreatedAt, 0),
	}, nil
}

func attestationToProto(as *types.As) (*protocol.Attestation, error) {
	var attributesJSON string
	if len(as.Attributes) > 0 {
		bytes, err := json.Marshal(as.Attributes)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal attributes: %w", err)
		}
		attributesJSON = string(bytes)
	}

	return &protocol.Attestation{
		Id:             as.ID,
		Subjects:       as.Subjects,
		Predicates:     as.Predicates,
		Contexts:       as.Contexts,
		Actors:         as.Actors,
		Timestamp:      as.Timestamp.Unix(),
		Source:         as.Source,
		AttributesJson: attributesJSON,
		CreatedAt:      as.CreatedAt.Unix(),
	}, nil
}

func protoToCommand(proto *protocol.AttestationCommand) (*types.AsCommand, error) {
	var attributes map[string]interface{}
	if proto.AttributesJson != "" {
		if err := json.Unmarshal([]byte(proto.AttributesJson), &attributes); err != nil {
			return nil, fmt.Errorf("failed to unmarshal attributes: %w", err)
		}
	}

	timestamp := time.Now()
	if proto.Timestamp != 0 {
		timestamp = time.Unix(proto.Timestamp, 0)
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
	filter := ats.AttestationFilter{
		Limit:      int(proto.Limit),
		Actors:     proto.Actors,
		Subjects:   proto.Subjects,
		Predicates: proto.Predicates,
		Contexts:   proto.Contexts,
	}

	if proto.TimeStart != 0 {
		t := time.Unix(proto.TimeStart, 0)
		filter.TimeStart = &t
	}

	if proto.TimeEnd != 0 {
		t := time.Unix(proto.TimeEnd, 0)
		filter.TimeEnd = &t
	}

	return filter
}
