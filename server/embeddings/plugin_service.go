package embeddings

import (
	"context"
	"math"
	"unsafe"

	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// PluginEmbeddingService implements the Service interface by calling a plugin's
// EmbeddingService gRPC. Pure Go — no build tags, no CGO.
type PluginEmbeddingService struct {
	client    protocol.EmbeddingServiceClient
	conn      *grpc.ClientConn
	authToken string
	logger    *zap.SugaredLogger
}

// NewPluginEmbeddingService creates a gRPC client connection to a plugin's EmbeddingService.
func NewPluginEmbeddingService(endpoint string, authToken string, logger *zap.SugaredLogger) (*PluginEmbeddingService, error) {
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to plugin embedding service at %s", endpoint)
	}

	return &PluginEmbeddingService{
		client:    protocol.NewEmbeddingServiceClient(conn),
		conn:      conn,
		authToken: authToken,
		logger:    logger,
	}, nil
}

// NewPluginEmbeddingServiceFromClient creates a PluginEmbeddingService using an existing
// gRPC client (e.g., from ExternalDomainProxy.EmbeddingServiceClient()). The caller
// owns the underlying connection — Close() is a no-op.
func NewPluginEmbeddingServiceFromClient(client protocol.EmbeddingServiceClient, logger *zap.SugaredLogger) *PluginEmbeddingService {
	return &PluginEmbeddingService{
		client: client,
		logger: logger,
	}
}

// GenerateEmbedding creates an embedding for the given text via plugin gRPC.
func (s *PluginEmbeddingService) GenerateEmbedding(text string) (*embeddings.EmbeddingResult, error) {
	resp, err := s.client.Embed(context.Background(), &protocol.EmbedRequest{
		AuthToken: s.authToken,
		Text:      text,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "plugin Embed RPC failed for text (%d chars)", len(text))
	}

	return &embeddings.EmbeddingResult{
		Text:      text,
		Embedding: resp.Vector,
		Tokens:    int(resp.Tokens),
	}, nil
}

// GenerateBatchEmbeddings creates embeddings for multiple texts via plugin gRPC.
func (s *PluginEmbeddingService) GenerateBatchEmbeddings(texts []string) (*embeddings.BatchEmbeddingResult, error) {
	resp, err := s.client.BatchEmbed(context.Background(), &protocol.BatchEmbedRequest{
		AuthToken: s.authToken,
		Texts:     texts,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "plugin BatchEmbed RPC failed for %d texts", len(texts))
	}

	results := make([]embeddings.EmbeddingResult, len(resp.Results))
	for i, vec := range resp.Results {
		text := ""
		if i < len(texts) {
			text = texts[i]
		}
		results[i] = embeddings.EmbeddingResult{
			Text:      text,
			Embedding: vec.Vector,
			Tokens:    int(vec.Tokens),
		}
	}

	return &embeddings.BatchEmbeddingResult{
		Embeddings:  results,
		TotalTokens: int(resp.TotalTokens),
	}, nil
}

// GetModelInfo returns metadata about the loaded model via plugin gRPC.
func (s *PluginEmbeddingService) GetModelInfo() (*embeddings.ModelInfo, error) {
	resp, err := s.client.ModelInfo(context.Background(), &protocol.ModelInfoRequest{
		AuthToken: s.authToken,
	})
	if err != nil {
		return nil, errors.Wrap(err, "plugin ModelInfo RPC failed")
	}

	return &embeddings.ModelInfo{
		Name:       resp.Name,
		Dimensions: int(resp.Dimensions),
	}, nil
}

// SerializeEmbedding converts an embedding to FLOAT32_BLOB format for sqlite-vec.
// Pure Go — no RPC needed.
func (s *PluginEmbeddingService) SerializeEmbedding(embedding []float32) ([]byte, error) {
	if len(embedding) == 0 {
		return nil, errors.New("embedding cannot be empty")
	}

	buf := make([]byte, len(embedding)*4)
	for i, val := range embedding {
		bits := *(*uint32)(unsafe.Pointer(&val))
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}

	return buf, nil
}

// DeserializeEmbedding converts a FLOAT32_BLOB back to []float32.
// Pure Go — no RPC needed.
func (s *PluginEmbeddingService) DeserializeEmbedding(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, errors.Newf("invalid embedding data length: %d", len(data))
	}

	embedding := make([]float32, len(data)/4)
	for i := range embedding {
		bits := uint32(data[i*4]) |
			uint32(data[i*4+1])<<8 |
			uint32(data[i*4+2])<<16 |
			uint32(data[i*4+3])<<24
		embedding[i] = *(*float32)(unsafe.Pointer(&bits))
	}

	return embedding, nil
}

// ComputeSimilarity calculates cosine similarity between two embeddings.
// Pure Go — no RPC needed.
func (s *PluginEmbeddingService) ComputeSimilarity(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, errors.Newf("embedding dimension mismatch: %d vs %d", len(a), len(b))
	}

	if len(a) == 0 {
		return 0, errors.New("embeddings cannot be empty")
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0, nil
	}

	similarity := dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
	return similarity, nil
}

// ClusterHDBSCAN runs HDBSCAN clustering via the plugin's Cluster RPC.
func (s *PluginEmbeddingService) ClusterHDBSCAN(flat []float32, nPoints, dims, minClusterSize int) (*embeddings.ClusterResult, error) {
	resp, err := s.client.Cluster(context.Background(), &protocol.ClusterRequest{
		AuthToken:      s.authToken,
		Data:           flat,
		NPoints:        int32(nPoints),
		Dimensions:     int32(dims),
		MinClusterSize: int32(minClusterSize),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "plugin Cluster RPC failed (n_points=%d, dims=%d, min_cluster_size=%d)", nPoints, dims, minClusterSize)
	}

	centroids := make([][]float32, len(resp.Centroids))
	for i, c := range resp.Centroids {
		centroids[i] = c.Vector
	}

	nNoise := 0
	for _, l := range resp.Labels {
		if l < 0 {
			nNoise++
		}
	}

	return &embeddings.ClusterResult{
		Labels:        resp.Labels,
		Probabilities: resp.Probabilities,
		NClusters:     int(resp.NClusters),
		NPoints:       nPoints,
		NNoise:        nNoise,
		Centroids:     centroids,
	}, nil
}

// Close closes the gRPC connection if owned by this service.
// No-op when created via NewPluginEmbeddingServiceFromClient (caller owns connection).
func (s *PluginEmbeddingService) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}
