package grpc

import (
	"context"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// RemoteATSStore is a gRPC client wrapper for the attestation store.
// It implements the subset of AttestationStore methods needed by plugins.
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
	conn, err := grpc.NewClient(endpoint,
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
func (r *RemoteATSStore) GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error) {
	// types.AsCommand says the node authors a command naming no actor, and now
	// for one naming no time; on the wire each is said. Its version it names.
	protoCmd := &protocol.AttestationCommand{
		Subjects:   cmd.Subjects,
		Predicates: cmd.Predicates,
		Contexts:   cmd.Contexts,
		Source:     cmd.Source,
		By:         &protocol.AttestationCommand_ActorsNamed{ActorsNamed: &protocol.ActorNames{Names: cmd.Actors}},
		When:       &protocol.AttestationCommand_Timestamp{Timestamp: cmd.Timestamp.UnixMilli()},
	}
	if err := cmd.NamesItsActor(); err != nil {
		protoCmd.By = &protocol.AttestationCommand_ByNode{ByNode: true}
	}
	if cmd.Timestamp.IsZero() {
		protoCmd.When = &protocol.AttestationCommand_Now{Now: true}
	}
	if err := sayVersion(cmd, protoCmd); err != nil {
		return nil, err
	}

	attrs, err := protocol.AttributesStruct(cmd.Attributes)
	if err != nil {
		return nil, errors.Wrapf(err, "the attestation of %v is not sent", cmd.Subjects)
	}
	protoCmd.Attributes = attrs

	req := &protocol.GenerateAttestationRequest{
		AuthToken: r.authToken,
		Command:   protoCmd,
	}

	resp, err := r.client.GenerateAndCreateAttestation(ctx, req)
	if err != nil {
		return nil, errors.Wrap(err, "gRPC GenerateAndCreateAttestation failed")
	}

	if !resp.Success {
		return nil, errors.Newf("failed to generate attestation: %s", resp.Error)
	}

	return resp.Attestation.ToTypes(), nil
}

// sayVersion says on the wire the version a command names: its own, or the
// registry's. A command naming neither is refused.
func sayVersion(cmd *types.AsCommand, protoCmd *protocol.AttestationCommand) error {
	switch version := cmd.SourceVersion.(type) {
	case types.VersionNamed:
		protoCmd.Version = &protocol.AttestationCommand_SourceVersion{SourceVersion: version.Version.String()}
		return nil
	case types.VersionFromRegistry:
		protoCmd.Version = &protocol.AttestationCommand_VersionFromRegistry{VersionFromRegistry: true}
		return nil
	}
	return errors.Newf("the attestation of %v from %s names no version: its own, or the registry's", cmd.Subjects, cmd.Source)
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
	// Each side of the time range is said on the wire as the filter names
	// it; a side naming neither is refused.
	start, starts, err := filter.TimeStart.At("start")
	if err != nil {
		return nil, err
	}
	end, ends, err := filter.TimeEnd.At("end")
	if err != nil {
		return nil, err
	}
	protoFilter := &protocol.AttestationFilter{
		Actors:     filter.Actors,
		Subjects:   filter.Subjects,
		Predicates: filter.Predicates,
		Contexts:   filter.Contexts,
		Limit:      int32(filter.Limit),
		From:       &protocol.AttestationFilter_UnboundedStart{UnboundedStart: true},
		To:         &protocol.AttestationFilter_UnboundedEnd{UnboundedEnd: true},
	}
	if starts {
		protoFilter.From = &protocol.AttestationFilter_TimeStart{TimeStart: start.UnixMilli()}
	}
	if ends {
		protoFilter.To = &protocol.AttestationFilter_TimeEnd{TimeEnd: end.UnixMilli()}
	}

	req := &protocol.GetAttestationsRequest{
		AuthToken: r.authToken,
		Filter:    protoFilter,
	}

	resp, err := r.client.GetAttestations(r.ctx, req)
	if err != nil {
		return nil, errors.Wrap(err, "gRPC GetAttestations failed")
	}
	if !resp.Success {
		return nil, errors.Newf("the store did not answer the query: %s", resp.Error)
	}

	attestations := make([]*types.As, len(resp.Attestations))
	for i, protoAtt := range resp.Attestations {
		attestations[i] = protoAtt.ToTypes()
	}

	return attestations, nil
}

// CreateAttestation creates an attestation with a pre-generated ID via gRPC.
func (r *RemoteATSStore) CreateAttestation(a *types.As) error {
	protoAtt, err := protocol.AttestationFromTypes(a)
	if err != nil {
		return errors.Wrapf(err, "failed to convert attestation %s to proto", a.ID)
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

	return nil
}

// CreateAttestationInbound sends a synced attestation to the remote store.
// For remote stores, this currently maps to CreateAttestation - the remote
// server is responsible for deciding whether to sign based on its context.
func (r *RemoteATSStore) CreateAttestationInbound(a *types.As) error {
	return r.CreateAttestation(a)
}
