package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// VersionResolver maps a source name to its running version. False is a
// source the node runs no plugin by.
type VersionResolver func(source string) (version string, known bool)

// noVersions is the node before it hands its plugin registry over: no source's
// version is known yet.
func noVersions(string) (string, bool) { return "", false }

// CallStores is the store of the caller a plugin is answering, by the token the
// node handed the plugin for that one call. False is no call open under it.
type CallStores func(token string) (ats.AttestationStore, bool)

// PluginStores is the store of the namespace a plugin stands in, by the token
// the node handed that plugin at Initialize (ADR-046). False is no plugin
// standing under it.
type PluginStores func(token string) (ats.AttestationStore, bool)

// noCallOpen is the node before it hands its open calls over: it has handed no
// plugin a call's token yet, so no call is open.
func noCallOpen(string) (ats.AttestationStore, bool) { return nil, false }

// noPluginStanding is the node before it hands its standing plugins over: it
// has handed no plugin a token of its own yet, so none stands.
func noPluginStanding(string) (ats.AttestationStore, bool) { return nil, false }

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
	s := &ATSStoreServer{
		store:           store,
		authToken:       authToken,
		node:            node,
		logger:          logger,
		versionResolver: noVersions,
		streamCtx:       ctx,
		streamCancel:    cancel,
	}
	s.SetCallStores(noCallOpen)
	s.SetPluginStores(noPluginStanding)
	return s
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
	if sameToken(token, s.authToken) {
		return s.store, nil
	}
	if store, open := (*s.calls.Load())(token); open {
		return store, nil
	}
	if store, standing := (*s.plugins.Load())(token); standing {
		return store, nil
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

	// A request carrying no command names no source, and is refused for it.
	cmd, err := s.protoToCommand(req.GetCommand())
	if err != nil {
		return &protocol.GenerateAttestationResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to convert command: %v", err),
		}, nil
	}

	// Generate and create the attestation
	as, err := store.GenerateAndCreateAttestation(ctx, cmd)
	if err != nil {
		s.logger.Errorw("GenerateAndCreateAttestation failed", "source", cmd.Source, "error", err)
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

	// Fallback: individual writes, each attestation named as it is made
	made := make([]string, 0, len(cmds))
	for _, cmd := range cmds {
		as, err := store.GenerateAndCreateAttestation(ctx, cmd)
		if err != nil {
			s.logger.Errorw("BatchGenerateAndCreateAttestations fallback failed", "created", made, "total", len(cmds), "error", err)
			return &protocol.BatchGenerateAttestationResponse{
				Success: false,
				Error:   fmt.Sprintf("individual write failed at %d/%d, after %v: %v", len(made), len(cmds), made, err),
				Created: int32(len(made)),
			}, nil
		}
		made = append(made, as.ID)
	}
	s.logger.Debugw("Batch written one attestation at a time", "attestations", made)
	created := int32(len(made))
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
	if err := ctx.Err(); err != nil {
		return err
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
	if err := ctx.Err(); err != nil {
		s.logger.Debugw("Stream cancelled after query, discarding results",
			"results", len(attestations))
		return err
	}

	s.logger.Debugw("GetAttestationsStream",
		"results", len(attestations),
		"predicates", filter.Predicates,
		"subjects", filter.Subjects)

	for _, as := range attestations {
		if err := ctx.Err(); err != nil {
			return err
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
	// Source is how an attestation was made: a plugin that does not say is
	// refused rather than written as some plugin. A request that carries no
	// command says no source either.
	source, err := named.TextOf(fmt.Sprintf("the source of the command about %v (the plugin's name)", proto.GetSubjects()), proto.GetSource())
	if err != nil {
		return nil, err
	}

	// No attributes sent is a command with none: AsMap reads an absent Struct
	// as no fields.
	attributes := proto.GetAttributes().AsMap()

	when, err := commandTime(proto)
	if err != nil {
		return nil, err
	}
	if err := s.stampVersion(proto, source.String(), attributes); err != nil {
		return nil, err
	}
	by, err := s.commandActors(proto)
	if err != nil {
		return nil, err
	}

	return &types.AsCommand{
		Subjects:   proto.Subjects,
		Predicates: proto.Predicates,
		Contexts:   proto.Contexts,
		By:         by,
		When:       when,
		Attributes: attributes,
		Source:     source.String(),
	}, nil
}

// commandTime is when a command says it was made: the time it names, or now.
// A timestamp of 0 is the epoch, as said.
func commandTime(proto *protocol.AttestationCommand) (types.Moment, error) {
	switch when := proto.GetWhen().(type) {
	case *protocol.AttestationCommand_Timestamp:
		return types.At{Time: time.UnixMilli(when.Timestamp)}, nil
	case *protocol.AttestationCommand_Now:
		if !when.Now {
			return nil, errors.Newf("the command about %v says now is false: it names a timestamp, or now", proto.GetSubjects())
		}
		return types.Now{}, nil
	}
	return nil, errors.Newf("the command about %v names no time: a timestamp, or now", proto.GetSubjects())
}

// stampVersion stamps source_version: as the plugin says it, or as the
// registry has it for the source.
func (s *ATSStoreServer) stampVersion(proto *protocol.AttestationCommand, source string, attributes map[string]any) error {
	switch version := proto.GetVersion().(type) {
	case *protocol.AttestationCommand_SourceVersion:
		said, err := named.TextOf("source_version", version.SourceVersion)
		if err != nil {
			return err
		}
		attributes["source_version"] = said.String()
		return nil
	case *protocol.AttestationCommand_VersionFromRegistry:
		if !version.VersionFromRegistry {
			return errors.Newf("the command from %s says version_from_registry is false: it names its source_version, or the registry's", source)
		}
		registered, known := s.versionResolver(source)
		if !known {
			return errors.Newf("the node runs no plugin %s, so the registry has no version of it", source)
		}
		attributes["source_version"] = registered
		return nil
	}
	return errors.Newf("the command from %s names no version: its source_version, or version_from_registry", source)
}

// commandActors are who a command says made it: the actors it names, or the
// node.
func (s *ATSStoreServer) commandActors(proto *protocol.AttestationCommand) (types.Authors, error) {
	switch by := proto.GetBy().(type) {
	case *protocol.AttestationCommand_ActorsNamed:
		actors, err := named.ItemsOf(fmt.Sprintf("the actors of the command about %v", proto.GetSubjects()), by.ActorsNamed.GetNames())
		if err != nil {
			return nil, err
		}
		return types.ActorsNamed{Actors: actors}, nil
	case *protocol.AttestationCommand_ByNode:
		// "the node"
		//
		// authors what a plugin asks it to: the plugin wrote through it.
		if !by.ByNode {
			return nil, errors.Newf("the command about %v says by_node is false: it names its actors, or the node", proto.GetSubjects())
		}
		node, err := named.TextOf(fmt.Sprintf("the node's DID, who writes the command about %v", proto.GetSubjects()), s.node)
		if err != nil {
			return nil, err
		}
		return types.ActorNamed(node), nil
	}
	return nil, errors.Newf("the command about %v names who made it: its actors, or by_node", proto.GetSubjects())
}

// protoToFilter is a plugin's filter as the store reads it. A query names how
// many rows it wants, 0 being none and every row ats.EveryRow, and where its
// time range starts and ends, or that it is unbounded there.
func protoToFilter(proto *protocol.AttestationFilter) (ats.AttestationFilter, error) {
	filter := ats.AttestationFilter{
		Limit:      int(proto.GetLimit()),
		Actors:     proto.GetActors(),
		Subjects:   proto.GetSubjects(),
		Predicates: proto.GetPredicates(),
		Contexts:   proto.GetContexts(),
	}

	start, err := filterStart(proto)
	if err != nil {
		return ats.AttestationFilter{}, err
	}
	end, err := filterEnd(proto)
	if err != nil {
		return ats.AttestationFilter{}, err
	}
	// ats.AttestationFilter names each side the same way.
	filter.TimeStart, filter.TimeEnd = start, end
	return filter, nil
}

// filterStart is where a query's time range starts, or that it is unbounded.
func filterStart(proto *protocol.AttestationFilter) (types.TimeBound, error) {
	switch from := proto.GetFrom().(type) {
	case *protocol.AttestationFilter_TimeStart:
		return types.BoundAt(time.UnixMilli(from.TimeStart)), nil
	case *protocol.AttestationFilter_UnboundedStart:
		if !from.UnboundedStart {
			return types.TimeBound{}, errors.New("unbounded_start is false: a query names where its time range starts, or that it is unbounded")
		}
		return types.Unbounded(), nil
	}
	return types.TimeBound{}, errors.New("a query names where its time range starts: time_start, or unbounded_start")
}

// filterEnd is where a query's time range ends, or that it is unbounded.
func filterEnd(proto *protocol.AttestationFilter) (types.TimeBound, error) {
	switch to := proto.GetTo().(type) {
	case *protocol.AttestationFilter_TimeEnd:
		return types.BoundAt(time.UnixMilli(to.TimeEnd)), nil
	case *protocol.AttestationFilter_UnboundedEnd:
		if !to.UnboundedEnd {
			return types.TimeBound{}, errors.New("unbounded_end is false: a query names where its time range ends, or that it is unbounded")
		}
		return types.Unbounded(), nil
	}
	return types.TimeBound{}, errors.New("a query names where its time range ends: time_end, or unbounded_end")
}
