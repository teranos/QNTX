package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// VersionResolver maps a source name to its running version.
// Returns "" if the source is unknown.
type VersionResolver func(source string) string

// CallStores is the store of the caller a plugin is answering, by the token the
// node handed the plugin for that one call. False is no call open under it.
type CallStores func(token string) (ats.AttestationStore, bool)

// PluginStores is the store of the namespace a plugin stands in, by the token
// the node handed that plugin at Initialize (ADR-046). False is no plugin
// standing under it.
type PluginStores func(token string) (ats.AttestationStore, bool)

// ATSStoreServer implements the ATSStoreService gRPC server
type ATSStoreServer struct {
	protocol.UnimplementedATSStoreServiceServer
	store           ats.AttestationStore
	authToken       string
	node            string // The node's DID: who wrote an attestation a plugin named no actor for.
	logger          *zap.SugaredLogger
	versionResolver VersionResolver
	// Set after the service is serving, while plugins may already be calling.
	calls   atomic.Pointer[CallStores]
	plugins atomic.Pointer[PluginStores]

	// streamMu protects streamCtx/streamCancel
	streamMu     sync.Mutex
	streamCtx    context.Context
	streamCancel context.CancelFunc
}

// NewATSStoreServer creates a new ATS store gRPC server. node is the node's DID.
func NewATSStoreServer(store ats.AttestationStore, authToken, node string, logger *zap.SugaredLogger) *ATSStoreServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &ATSStoreServer{
		store:        store,
		authToken:    authToken,
		node:         node,
		logger:       logger,
		streamCtx:    ctx,
		streamCancel: cancel,
	}
}

// SetVersionResolver sets the function used to resolve plugin versions from source names.
func (s *ATSStoreServer) SetVersionResolver(resolver VersionResolver) {
	s.versionResolver = resolver
}

// SetCallStores hands the server the stores of the calls plugins are answering.
func (s *ATSStoreServer) SetCallStores(calls CallStores) {
	s.calls.Store(&calls)
}

// SetPluginStores hands the server the stores of the namespaces plugins stand in.
func (s *ATSStoreServer) SetPluginStores(plugins PluginStores) {
	s.plugins.Store(&plugins)
}

// storeFor is the store a token reaches: the served one for the shared token,
// the caller's for a call's token, the namespace a plugin stands in for that
// plugin's own token, and none for anything else.
func (s *ATSStoreServer) storeFor(token string) (ats.AttestationStore, error) {
	if ValidateToken(token, s.authToken) == nil {
		return s.store, nil
	}
	if calls := s.calls.Load(); calls != nil {
		if store, open := (*calls)(token); open {
			return store, nil
		}
	}
	if plugins := s.plugins.Load(); plugins != nil {
		if store, standing := (*plugins)(token); standing {
			return store, nil
		}
	}
	return nil, errors.New("invalid authentication token")
}

// CancelStreams cancels all active streams and resets the context for new ones.
// Called during plugin restart to free the database mutex before launching the new process.
func (s *ATSStoreServer) CancelStreams() {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	s.streamCancel()
	s.streamCtx, s.streamCancel = context.WithCancel(context.Background())
	s.logger.Infow("Cancelled active ATSStore streams for plugin restart")
}

// getStreamCtx returns the current stream context (safe for concurrent use).
func (s *ATSStoreServer) getStreamCtx() context.Context {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	return s.streamCtx
}

// CreateAttestation creates a new attestation
func (s *ATSStoreServer) CreateAttestation(ctx context.Context, req *protocol.CreateAttestationRequest) (*protocol.CreateAttestationResponse, error) {
	store, err := s.storeFor(req.AuthToken)
	if err != nil {
		return &protocol.CreateAttestationResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	as := req.Attestation.ToTypes()

	if err := store.CreateAttestation(as); err != nil {
		return &protocol.CreateAttestationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create attestation: %v", err),
		}, nil
	}

	return &protocol.CreateAttestationResponse{
		Success: true,
	}, nil
}

// AttestationExists checks if an attestation exists
func (s *ATSStoreServer) AttestationExists(ctx context.Context, req *protocol.AttestationExistsRequest) (*protocol.AttestationExistsResponse, error) {
	// This response has no error field, so the transport error is the only
	// honest channel — Exists: false would be indistinguishable from truth.
	store, err := s.storeFor(req.AuthToken)
	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "attestation exists check refused: %v", err)
	}

	exists := store.AttestationExists(req.Id)

	return &protocol.AttestationExistsResponse{
		Exists: exists,
	}, nil
}

// GenerateAndCreateAttestation generates an ID and creates an attestation
func (s *ATSStoreServer) GenerateAndCreateAttestation(ctx context.Context, req *protocol.GenerateAttestationRequest) (*protocol.GenerateAttestationResponse, error) {
	store, err := s.storeFor(req.AuthToken)
	if err != nil {
		return &protocol.GenerateAttestationResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	if req.Command == nil {
		return &protocol.GenerateAttestationResponse{
			Success: false,
			Error:   "command is nil",
		}, nil
	}

	// Convert protobuf command to types.AsCommand
	cmd, err := s.protoToCommand(req.Command)
	if err != nil {
		return &protocol.GenerateAttestationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to convert command: %v", err),
		}, nil
	}

	// Generate and create the attestation
	as, err := store.GenerateAndCreateAttestation(ctx, cmd)
	if err != nil {
		s.logger.Errorw("GenerateAndCreateAttestation failed", "source", req.Command.Source, "error", err)
		return &protocol.GenerateAttestationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to generate attestation: %v", err),
		}, nil
	}

	protoAtt, err := protocol.AttestationFromTypes(as)
	if err != nil {
		return &protocol.GenerateAttestationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to convert attestation to proto: %v", err),
		}, nil
	}

	return &protocol.GenerateAttestationResponse{
		Success:     true,
		Attestation: protoAtt,
	}, nil
}

// BatchGenerateAndCreateAttestations generates IDs and creates multiple attestations in one write transaction
func (s *ATSStoreServer) BatchGenerateAndCreateAttestations(ctx context.Context, req *protocol.BatchGenerateAttestationRequest) (*protocol.BatchGenerateAttestationResponse, error) {
	store, err := s.storeFor(req.AuthToken)
	if err != nil {
		return &protocol.BatchGenerateAttestationResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	if len(req.Commands) == 0 {
		return &protocol.BatchGenerateAttestationResponse{
			Success: true,
			Created: 0,
		}, nil
	}

	cmds := make([]*types.AsCommand, 0, len(req.Commands))
	for i, proto := range req.Commands {
		cmd, err := s.protoToCommand(proto)
		if err != nil {
			return &protocol.BatchGenerateAttestationResponse{
				Success: false,
				Error:   fmt.Sprintf("failed to convert command %d: %v", i, err),
			}, nil
		}
		cmds = append(cmds, cmd)
	}

	// RustBackedStore implements this; other stores fall back to individual writes
	type batchCreator interface {
		BatchGenerateAndCreateAttestations(ctx context.Context, cmds []*types.AsCommand) (int, error)
	}
	if bs, ok := store.(batchCreator); ok {
		created, err := bs.BatchGenerateAndCreateAttestations(ctx, cmds)
		if err != nil {
			s.logger.Errorw("BatchGenerateAndCreateAttestations failed", "count", len(cmds), "created", created, "error", err)
			return &protocol.BatchGenerateAttestationResponse{
				Success: false,
				Error:   fmt.Sprintf("batch write failed after %d/%d: %v", created, len(cmds), err),
				Created: int32(created),
			}, nil
		}
		return &protocol.BatchGenerateAttestationResponse{
			Success: true,
			Created: int32(created),
		}, nil
	}

	// Fallback: individual writes
	var created int32
	for _, cmd := range cmds {
		if _, err := store.GenerateAndCreateAttestation(ctx, cmd); err != nil {
			s.logger.Errorw("BatchGenerateAndCreateAttestations fallback failed", "created", created, "total", len(cmds), "error", err)
			return &protocol.BatchGenerateAttestationResponse{
				Success: false,
				Error:   fmt.Sprintf("individual write failed at %d/%d: %v", created, len(cmds), err),
				Created: created,
			}, nil
		}
		created++
	}
	return &protocol.BatchGenerateAttestationResponse{
		Success: true,
		Created: created,
	}, nil
}

// GetAttestations queries attestations with filters
func (s *ATSStoreServer) GetAttestations(ctx context.Context, req *protocol.GetAttestationsRequest) (*protocol.GetAttestationsResponse, error) {
	store, err := s.storeFor(req.AuthToken)
	if err != nil {
		return &protocol.GetAttestationsResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	filter, err := protoToFilter(req.Filter)
	if err != nil {
		return &protocol.GetAttestationsResponse{ //nolint:nilerr // the refusal travels in the response payload
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Query attestations
	attestations, err := store.GetAttestations(filter)
	if err != nil {
		return &protocol.GetAttestationsResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to query attestations: %v", err),
		}, nil
	}

	s.logger.Debugw("GetAttestations",
		"results", len(attestations),
		"predicates", filter.Predicates,
		"subjects", filter.Subjects,
		"contexts", filter.Contexts,
		"actors", filter.Actors,
		"limit", filter.Limit)

	protoAttestations := make([]*protocol.Attestation, len(attestations))
	for i, as := range attestations {
		protoAtt, err := protocol.AttestationFromTypes(as)
		if err != nil {
			s.logger.Errorw("GetAttestations proto conversion failed", "index", i, "id", as.ID, "error", err)
			return &protocol.GetAttestationsResponse{
				Success: false,
				Error:   fmt.Sprintf("failed to convert attestation %s to proto: %v", as.ID, err),
			}, nil
		}
		protoAttestations[i] = protoAtt
	}

	s.logger.Debugw("GetAttestations returning", "count", len(protoAttestations))

	return &protocol.GetAttestationsResponse{
		Success:      true,
		Attestations: protoAttestations,
	}, nil
}

// GetAttestationsStream queries attestations and streams them individually.
func (s *ATSStoreServer) GetAttestationsStream(req *protocol.GetAttestationsRequest, stream protocol.ATSStoreService_GetAttestationsStreamServer) error {
	store, err := s.storeFor(req.AuthToken)
	if err != nil {
		return err
	}

	// Check if streams were cancelled (plugin restart in progress)
	ctx := s.getStreamCtx()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	filter, err := protoToFilter(req.Filter)
	if err != nil {
		return err
	}

	attestations, err := store.GetAttestations(filter)
	if err != nil {
		return errors.Wrapf(err, "failed to query attestations")
	}

	// Check again after query — plugin may have been killed while we held the mutex
	if ctx.Err() != nil {
		s.logger.Debugw("Stream cancelled after query, discarding results",
			"results", len(attestations))
		return ctx.Err()
	}

	s.logger.Debugw("GetAttestationsStream",
		"results", len(attestations),
		"predicates", filter.Predicates,
		"subjects", filter.Subjects)

	for _, as := range attestations {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		protoAtt, err := protocol.AttestationFromTypes(as)
		if err != nil {
			return errors.Wrapf(err, "failed to convert attestation %s to proto", as.ID)
		}
		if err := stream.Send(protoAtt); err != nil {
			return err
		}
	}

	return nil
}

func (s *ATSStoreServer) protoToCommand(proto *protocol.AttestationCommand) (*types.AsCommand, error) {
	attributes := make(map[string]any)
	if proto.Attributes != nil {
		attributes = proto.Attributes.AsMap()
	}

	timestamp := time.Now()
	if proto.Timestamp != nil && *proto.Timestamp != 0 {
		timestamp = time.UnixMilli(*proto.Timestamp)
	}

	// Source is how an attestation was made: a plugin that does not say is
	// refused rather than written as some plugin.
	source := proto.Source
	if source == "" {
		return nil, errors.Newf("the command about %v names no source; set source to the plugin's name", proto.Subjects)
	}

	// Stamp source_version: prefer explicit value from proto, fall back to registry lookup
	if proto.SourceVersion != "" {
		attributes["source_version"] = proto.SourceVersion
	} else if s.versionResolver != nil {
		if v := s.versionResolver(source); v != "" {
			attributes["source_version"] = v
		}
	}

	// "the node"
	//
	// authors what a plugin named no actor for: the plugin wrote through it.
	actors := proto.Actors
	if len(actors) == 0 {
		actors = []string{s.node}
	}

	return &types.AsCommand{
		Subjects:   proto.Subjects,
		Predicates: proto.Predicates,
		Contexts:   proto.Contexts,
		Actors:     actors,
		Timestamp:  timestamp,
		Attributes: attributes,
		Source:     source,
	}, nil
}

// protoToFilter is a plugin's filter as the store reads it. A query names how
// many rows it wants, every row being ats.EveryRow; one naming no limit is
// refused rather than handed every row or none.
func protoToFilter(proto *protocol.AttestationFilter) (ats.AttestationFilter, error) {
	if proto.Limit == nil {
		return ats.AttestationFilter{}, errors.New("a query names how many rows it wants, and this one named no limit; every row is ats.EveryRow")
	}
	filter := ats.AttestationFilter{
		Limit:      int(*proto.Limit),
		Actors:     proto.Actors,
		Subjects:   proto.Subjects,
		Predicates: proto.Predicates,
		Contexts:   proto.Contexts,
	}

	if proto.TimeStart != nil {
		t := time.UnixMilli(*proto.TimeStart)
		filter.TimeStart = &t
	}

	if proto.TimeEnd != nil {
		t := time.UnixMilli(*proto.TimeEnd)
		filter.TimeEnd = &t
	}

	return filter, nil
}
