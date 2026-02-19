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
	ctx       context.Context // Parent context for cancellation
}

// NewRemoteATSStore creates a gRPC client connection to the ATSStore service.
// The provided context is used for all gRPC operations and enables cancellation.
func NewRemoteATSStore(ctx context.Context, endpoint string, authToken string, logger *zap.SugaredLogger) (*RemoteATSStore, error) {
	conn, err := grpc.Dial(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to ATSStore gRPC endpoint")
	}

	client := protocol.NewATSStoreServiceClient(conn)

	return &RemoteATSStore{
		client:    client,
		conn:      conn,
		authToken: authToken,
		logger:    logger,
		ctx:       ctx,
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
			return nil, errors.Wrap(err, "failed to marshal attributes")
		}
		protoCmd.AttributesJson = attributesJSON
	}

	if !cmd.Timestamp.IsZero() {
		ts := cmd.Timestamp.UnixMilli()
		protoCmd.Timestamp = &ts
	}

	req := &protocol.GenerateAttestationRequest{
		AuthToken: r.authToken,
		Command:   protoCmd,
	}

	resp, err := r.client.GenerateAndCreateAttestation(r.ctx, req)
	if err != nil {
		return nil, errors.Wrap(err, "gRPC GenerateAndCreateAttestation failed")
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
		Timestamp:  time.UnixMilli(resp.Attestation.Timestamp),
		Source:     resp.Attestation.Source,
		Attributes: make(map[string]interface{}),
		CreatedAt:  time.UnixMilli(resp.Attestation.CreatedAt),
	}

	// Unmarshal attributes from JSON
	if resp.Attestation.Attributes != "" {
		attributes, err := attributesFromJSON(resp.Attestation.Attributes)
		if err != nil {
			// Surface attribute parsing error to caller via special key
			r.logger.Warnw("Failed to unmarshal attributes", "error", err, "json", resp.Attestation.Attributes)
			attestation.Attributes["_attribute_parse_error"] = err.Error()
			attestation.Attributes["_attribute_parse_json"] = resp.Attestation.Attributes
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

	resp, err := r.client.AttestationExists(r.ctx, req)
	if err != nil {
		r.logger.Warnw("Failed to check attestation existence", "error", err)
		return false
	}

	return resp.Exists
}

// GetAttestations retrieves attestations via gRPC.
func (r *RemoteATSStore) GetAttestations(filter ats.AttestationFilter) ([]*types.As, error) {
	limit := int32(filter.Limit)
	protoFilter := &protocol.AttestationFilter{
		Actors:     filter.Actors,
		Subjects:   filter.Subjects,
		Predicates: filter.Predicates,
		Contexts:   filter.Contexts,
		Limit:      &limit,
	}
	if filter.TimeStart != nil {
		protoFilter.TimeStart = filter.TimeStart.UnixMilli()
	}
	if filter.TimeEnd != nil {
		protoFilter.TimeEnd = filter.TimeEnd.UnixMilli()
	}

	req := &protocol.GetAttestationsRequest{
		AuthToken: r.authToken,
		Filter:    protoFilter,
	}

	resp, err := r.client.GetAttestations(r.ctx, req)
	if err != nil {
		return nil, errors.Wrap(err, "gRPC GetAttestations failed")
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
			Timestamp:  time.UnixMilli(protoAtt.Timestamp),
			Source:     protoAtt.Source,
			Attributes: make(map[string]interface{}),
			CreatedAt:  time.UnixMilli(protoAtt.CreatedAt),
		}

		// Unmarshal attributes from JSON
		if protoAtt.Attributes != "" {
			attributes, err := attributesFromJSON(protoAtt.Attributes)
			if err != nil {
				// Surface attribute parsing error to caller via special key
				r.logger.Warnw("Failed to unmarshal attributes", "error", err, "id", protoAtt.Id)
				attestations[i].Attributes["_attribute_parse_error"] = err.Error()
				attestations[i].Attributes["_attribute_parse_json"] = protoAtt.Attributes
			} else {
				attestations[i].Attributes = attributes
			}
		}
	}

	return attestations, nil
}

// CreateAttestation creates an attestation with a pre-generated ID via gRPC.
func (r *RemoteATSStore) CreateAttestation(a *types.As) error {
	// Marshal attributes to JSON string
	attributesJSON := ""
	if a.Attributes != nil {
		json, err := attributesToJSON(a.Attributes)
		if err != nil {
			return errors.Wrap(err, "failed to marshal attributes")
		}
		attributesJSON = json
	}

	protoAtt := &protocol.Attestation{
		Id:         a.ID,
		Subjects:   a.Subjects,
		Predicates: a.Predicates,
		Contexts:   a.Contexts,
		Actors:     a.Actors,
		Timestamp:  a.Timestamp.UnixMilli(),
		Source:     a.Source,
		Attributes: attributesJSON,
		CreatedAt:  a.CreatedAt.UnixMilli(),
	}

	req := &protocol.CreateAttestationRequest{
		AuthToken:   r.authToken,
		Attestation: protoAtt,
	}

	resp, err := r.client.CreateAttestation(r.ctx, req)
	if err != nil {
		return errors.Wrap(err, "gRPC CreateAttestation failed")
	}

	if !resp.Success {
		return errors.Newf("failed to create attestation: %s", resp.Error)
	}

	r.logger.Infow("Attestation created via gRPC", "id", a.ID)
	return nil
}
