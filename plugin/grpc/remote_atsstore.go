package grpc

import (
	"context"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// RemoteATSStore is a gRPC client wrapper for storage.SQLStore.
// It implements the subset of SQLStore methods needed by plugins.
type RemoteATSStore struct {
	client    protocol.ATSStoreServiceClient
	conn      *grpc.ClientConn
	authToken string
	logger    *zap.SugaredLogger
}

// NewRemoteATSStore creates a gRPC client connection to the ATSStore service.
func NewRemoteATSStore(endpoint string, authToken string, logger *zap.SugaredLogger) (*RemoteATSStore, error) {
	conn, err := grpc.Dial(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	client := protocol.NewATSStoreServiceClient(conn)

	return &RemoteATSStore{
		client:    client,
		conn:      conn,
		authToken: authToken,
		logger:    logger,
	}, nil
}

// Close closes the gRPC connection.
func (r *RemoteATSStore) Close() error {
	return r.conn.Close()
}

// GenerateAndCreateAttestation creates an attestation via gRPC.
func (r *RemoteATSStore) GenerateAndCreateAttestation(cmd *types.AsCommand) (*types.As, error) {
	protoCmd := &protocol.AttestationCommand{
		Subjects:   cmd.Subjects,
		Predicates: cmd.Predicates,
		Contexts:   cmd.Contexts,
		Actors:     cmd.Actors,
	}

	// Marshal attributes to JSON string
	if cmd.Attributes != nil {
		attributesJSON, err := attributesToJSON(cmd.Attributes)
		if err != nil {
			return nil, err
		}
		protoCmd.AttributesJson = attributesJSON
	}

	if !cmd.Timestamp.IsZero() {
		protoCmd.Timestamp = cmd.Timestamp.Unix()
	}

	req := &protocol.GenerateAttestationRequest{
		AuthToken: r.authToken,
		Command:   protoCmd,
	}

	resp, err := r.client.GenerateAndCreateAttestation(context.Background(), req)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, errors.Newf("failed to generate attestation: %s", resp.Error)
	}

	// Convert response to types.As
	attestation := &types.As{
		ID:         resp.Attestation.Id,
		Subjects:   resp.Attestation.Subjects,
		Predicates: resp.Attestation.Predicates,
		Contexts:   resp.Attestation.Contexts,
		Actors:     resp.Attestation.Actors,
		Timestamp:  time.Unix(resp.Attestation.Timestamp, 0),
		Source:     resp.Attestation.Source,
		Attributes: make(map[string]interface{}),
		CreatedAt:  time.Unix(resp.Attestation.CreatedAt, 0),
	}

	// Unmarshal attributes from JSON
	if resp.Attestation.AttributesJson != "" {
		attributes, err := attributesFromJSON(resp.Attestation.AttributesJson)
		if err != nil {
			// Log warning but don't fail
			r.logger.Warnw("Failed to unmarshal attributes", "error", err, "json", resp.Attestation.AttributesJson)
		} else {
			attestation.Attributes = attributes
		}
	}

	return attestation, nil
}

// AttestationExists checks if an attestation exists via gRPC.
func (r *RemoteATSStore) AttestationExists(asid string) bool {
	req := &protocol.AttestationExistsRequest{
		AuthToken: r.authToken,
		Id:        asid,
	}

	resp, err := r.client.AttestationExists(context.Background(), req)
	if err != nil {
		r.logger.Warnw("Failed to check attestation existence", "error", err)
		return false
	}

	return resp.Exists
}

// GetAttestations retrieves attestations via gRPC.
func (r *RemoteATSStore) GetAttestations(filter ats.AttestationFilter) ([]*types.As, error) {
	protoFilter := &protocol.AttestationFilter{
		Actors:     filter.Actors,
		Subjects:   filter.Subjects,
		Predicates: filter.Predicates,
		Contexts:   filter.Contexts,
		Limit:      int32(filter.Limit),
	}
	if filter.TimeStart != nil {
		protoFilter.TimeStart = filter.TimeStart.Unix()
	}
	if filter.TimeEnd != nil {
		protoFilter.TimeEnd = filter.TimeEnd.Unix()
	}

	req := &protocol.GetAttestationsRequest{
		AuthToken: r.authToken,
		Filter:    protoFilter,
	}

	resp, err := r.client.GetAttestations(context.Background(), req)
	if err != nil {
		return nil, err
	}

	// Convert response attestations
	attestations := make([]*types.As, len(resp.Attestations))
	for i, protoAtt := range resp.Attestations {
		attestations[i] = &types.As{
			ID:         protoAtt.Id,
			Subjects:   protoAtt.Subjects,
			Predicates: protoAtt.Predicates,
			Contexts:   protoAtt.Contexts,
			Actors:     protoAtt.Actors,
			Timestamp:  time.Unix(protoAtt.Timestamp, 0),
			Source:     protoAtt.Source,
			Attributes: make(map[string]interface{}),
			CreatedAt:  time.Unix(protoAtt.CreatedAt, 0),
		}

		// Unmarshal attributes from JSON
		if protoAtt.AttributesJson != "" {
			attributes, err := attributesFromJSON(protoAtt.AttributesJson)
			if err != nil {
				// Log warning but don't fail
				r.logger.Warnw("Failed to unmarshal attributes", "error", err, "id", protoAtt.Id)
			} else {
				attestations[i].Attributes = attributes
			}
		}
	}

	return attestations, nil
}

// CreateAttestation is not implemented for remote plugins.
// Use GenerateAndCreateAttestation instead.
func (r *RemoteATSStore) CreateAttestation(a *types.As) error {
	r.logger.Warn("CreateAttestation not supported for remote plugins - use GenerateAndCreateAttestation")
	return errors.New("CreateAttestation not supported for remote plugins")
}

// GetAttestation is not implemented for remote plugins.
func (r *RemoteATSStore) GetAttestation(asid string) (*types.As, error) {
	r.logger.Warn("GetAttestation not supported for remote plugins yet")
	return nil, errors.New("GetAttestation not supported for remote plugins yet")
}

// DeleteAttestation is not implemented for remote plugins.
func (r *RemoteATSStore) DeleteAttestation(asid string) error {
	r.logger.Warn("DeleteAttestation not supported for remote plugins")
	return errors.New("DeleteAttestation not supported for remote plugins")
}
