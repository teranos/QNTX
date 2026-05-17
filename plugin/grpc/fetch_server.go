package grpc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// FetchServer implements the FetchService gRPC server.
// Performs HTTP GET requests on behalf of plugins and attests the response.
type FetchServer struct {
	protocol.UnimplementedFetchServiceServer
	store     ats.AttestationStore
	authToken string
	client    *http.Client
	logger    *zap.SugaredLogger
}

func NewFetchServer(store ats.AttestationStore, authToken string, logger *zap.SugaredLogger) *FetchServer {
	return &FetchServer{
		store:     store,
		authToken: authToken,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

func (s *FetchServer) Fetch(ctx context.Context, req *protocol.FetchRequest) (*protocol.FetchResponse, error) {
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.FetchResponse{Success: false, Error: err.Error()}, nil
	}

	if req.Url == "" {
		return &protocol.FetchResponse{Success: false, Error: "url is required"}, nil
	}

	// HTTP GET
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.Url, nil)
	if err != nil {
		return &protocol.FetchResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to create request for %s: %v", req.Url, err),
		}, nil
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return &protocol.FetchResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to fetch %s: %v", req.Url, err),
		}, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &protocol.FetchResponse{
			Success:    false,
			Error:      fmt.Sprintf("failed to read response body from %s: %v", req.Url, err),
			StatusCode: int32(resp.StatusCode),
		}, nil
	}

	s.logger.Infow("Fetch completed",
		"url", req.Url,
		"status", resp.StatusCode,
		"bytes", len(body),
		"subjects", req.Subjects,
	)

	// Attest the result
	attestID := ""
	if len(req.Subjects) > 0 && req.Predicate != "" {
		attCtx := req.Context
		if attCtx == "" {
			attCtx = req.Url
		}

		cmd := &types.AsCommand{
			Subjects:   req.Subjects,
			Predicates: []string{req.Predicate},
			Contexts:   []string{attCtx},
			Actors:     []string{"voor:pipeline"},
			Source:     "fetch-service",
			Attributes: map[string]interface{}{
				"response":    string(body),
				"status_code": resp.StatusCode,
			},
		}

		att, err := s.store.GenerateAndCreateAttestation(ctx, cmd)
		if err != nil {
			s.logger.Warnw("Fetch succeeded but attestation failed",
				"url", req.Url,
				"error", err,
			)
		} else {
			attestID = att.ID
		}
	}

	return &protocol.FetchResponse{
		Success:       true,
		Body:          string(body),
		StatusCode:    int32(resp.StatusCode),
		AttestationId: attestID,
	}, nil
}
