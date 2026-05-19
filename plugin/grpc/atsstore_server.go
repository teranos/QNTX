package grpc

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// VersionResolver maps a source name to its running version.
// Returns "" if the source is unknown.
type VersionResolver func(source string) string

// ATSStoreServer implements the ATSStoreService gRPC server
type ATSStoreServer struct {
	protocol.UnimplementedATSStoreServiceServer
	store           ats.AttestationStore
	authToken       string
	logger          *zap.SugaredLogger
	versionResolver VersionResolver

	// streamMu protects streamCtx/streamCancel
	streamMu     sync.Mutex
	streamCtx    context.Context
	streamCancel context.CancelFunc
}

// NewATSStoreServer creates a new ATS store gRPC server
func NewATSStoreServer(store ats.AttestationStore, authToken string, logger *zap.SugaredLogger) *ATSStoreServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &ATSStoreServer{
		store:        store,
		authToken:    authToken,
		logger:       logger,
		streamCtx:    ctx,
		streamCancel: cancel,
	}
}

// SetVersionResolver sets the function used to resolve plugin versions from source names.
func (s *ATSStoreServer) SetVersionResolver(resolver VersionResolver) {
	s.versionResolver = resolver
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
	as, err := s.store.GenerateAndCreateAttestation(ctx, cmd)
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
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.BatchGenerateAttestationResponse{
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
	if bs, ok := s.store.(batchCreator); ok {
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
		if _, err := s.store.GenerateAndCreateAttestation(ctx, cmd); err != nil {
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
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return err
	}

	// Check if streams were cancelled (plugin restart in progress)
	ctx := s.getStreamCtx()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	filter := protoToFilter(req.Filter)

	attestations, err := s.store.GetAttestations(filter)
	if err != nil {
		return fmt.Errorf("failed to query attestations: %w", err)
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
			return fmt.Errorf("failed to convert attestation %s to proto: %w", as.ID, err)
		}
		if err := stream.Send(protoAtt); err != nil {
			return err
		}
	}

	return nil
}

func (s *ATSStoreServer) protoToCommand(proto *protocol.AttestationCommand) (*types.AsCommand, error) {
	attributes := make(map[string]interface{})
	if proto.Attributes != nil {
		attributes = proto.Attributes.AsMap()
	}

	timestamp := time.Now()
	if proto.Timestamp != nil && *proto.Timestamp != 0 {
		timestamp = time.UnixMilli(*proto.Timestamp)
	}

	source := proto.Source
	if source == "" {
		logger.Logger.Warnw("AttestationCommand.source not set by plugin, falling back to 'plugin'", "hint", "set source to your plugin name")
		source = "plugin"
	}

	// Stamp source_version: prefer explicit value from proto, fall back to registry lookup
	if proto.SourceVersion != "" {
		attributes["source_version"] = proto.SourceVersion
	} else if s.versionResolver != nil {
		if v := s.versionResolver(source); v != "" {
			attributes["source_version"] = v
		}
	}

	return &types.AsCommand{
		Subjects:   proto.Subjects,
		Predicates: proto.Predicates,
		Contexts:   proto.Contexts,
		Actors:     proto.Actors,
		Timestamp:  timestamp,
		Attributes: attributes,
		Source:     source,
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

	if proto.TimeStart != nil {
		t := time.UnixMilli(*proto.TimeStart)
		filter.TimeStart = &t
	}

	if proto.TimeEnd != nil {
		t := time.UnixMilli(*proto.TimeEnd)
		filter.TimeEnd = &t
	}

	return filter
}
