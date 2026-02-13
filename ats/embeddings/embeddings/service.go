//go:build cgo && rustembeddings

package embeddings

import (
	"math"
	"sync"
	"unsafe"

	"github.com/teranos/QNTX/errors"
)

// ManagedEmbeddingService manages the embedding engine lifecycle and provides
// methods for generating embeddings from text
type ManagedEmbeddingService struct {
	mu          sync.RWMutex
	engine      EmbeddingService
	modelPath   string
	initialized bool
}

// NewManagedEmbeddingService creates a new managed embedding service instance
func NewManagedEmbeddingService(modelPath string) (*ManagedEmbeddingService, error) {
	if modelPath == "" {
		return nil, errors.New("model path is required")
	}

	return &ManagedEmbeddingService{
		modelPath: modelPath,
	}, nil
}

// Initialize loads the model and prepares the service for use
func (s *ManagedEmbeddingService) Initialize() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		return nil // Already initialized
	}

	// Create the underlying embedding engine
	s.engine = NewEmbeddingService()

	// Initialize with the model
	if err := s.engine.Init(s.modelPath); err != nil {
		return errors.Wrap(err, "failed to initialize embedding engine")
	}

	s.initialized = true
	return nil
}

// GetModelInfo returns information about the loaded model
func (s *ManagedEmbeddingService) GetModelInfo() (*ModelInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, errors.New("service not initialized")
	}

	return s.engine.ModelInfo()
}

// GenerateEmbedding creates an embedding for the given text
func (s *ManagedEmbeddingService) GenerateEmbedding(text string) (*EmbeddingResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, errors.New("service not initialized")
	}

	if text == "" {
		return nil, errors.New("text cannot be empty")
	}

	result, err := s.engine.Embed(text)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to generate embedding for text")
	}

	return result, nil
}

// GenerateBatchEmbeddings creates embeddings for multiple texts
func (s *ManagedEmbeddingService) GenerateBatchEmbeddings(texts []string) (*BatchEmbeddingResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.initialized {
		return nil, errors.New("service not initialized")
	}

	if len(texts) == 0 {
		return nil, errors.New("no texts provided")
	}

	// Check for empty texts
	for i, text := range texts {
		if text == "" {
			return nil, errors.Newf("text at index %d is empty", i)
		}
	}

	result, err := s.engine.EmbedBatch(texts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate batch embeddings")
	}

	return result, nil
}

// SerializeEmbedding converts an embedding to FLOAT32_BLOB format for sqlite-vec
func (s *ManagedEmbeddingService) SerializeEmbedding(embedding []float32) ([]byte, error) {
	if len(embedding) == 0 {
		return nil, errors.New("embedding cannot be empty")
	}

	// sqlite-vec expects little-endian float32 array
	buf := make([]byte, len(embedding)*4)
	for i, val := range embedding {
		// Convert float32 to bytes (little-endian)
		bits := *(*uint32)(unsafe.Pointer(&val))
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}

	return buf, nil
}

// DeserializeEmbedding converts a FLOAT32_BLOB back to []float32
func (s *ManagedEmbeddingService) DeserializeEmbedding(data []byte) ([]float32, error) {
	if len(data)%4 != 0 {
		return nil, errors.Newf("invalid embedding data length: %d", len(data))
	}

	embedding := make([]float32, len(data)/4)
	for i := range embedding {
		// Convert bytes back to float32 (little-endian)
		bits := uint32(data[i*4]) |
			uint32(data[i*4+1])<<8 |
			uint32(data[i*4+2])<<16 |
			uint32(data[i*4+3])<<24
		embedding[i] = *(*float32)(unsafe.Pointer(&bits))
	}

	return embedding, nil
}

// ComputeSimilarity calculates cosine similarity between two embeddings
func (s *ManagedEmbeddingService) ComputeSimilarity(a, b []float32) (float32, error) {
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
		return 0, nil // Zero vector
	}

	// Calculate cosine similarity using math.Sqrt for accuracy
	similarity := dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))

	return similarity, nil
}

// Close releases resources
func (s *ManagedEmbeddingService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized && s.engine != nil {
		if err := s.engine.Close(); err != nil {
			return errors.Wrap(err, "failed to close embedding engine")
		}
		s.initialized = false
		s.engine = nil
	}

	return nil
}

// EmbeddingRequest represents a request to generate embeddings
type EmbeddingRequest struct {
	Text  string   `json:"text,omitempty"`
	Texts []string `json:"texts,omitempty"`
}

// EmbeddingResponse represents the response from embedding generation
type EmbeddingResponse struct {
	Embeddings  [][]float32 `json:"embeddings"`
	Tokens      int         `json:"tokens"`
	InferenceMS float64     `json:"inference_ms"`
	Dimensions  int         `json:"dimensions"`
}

// HandleEmbeddingRequest processes an embedding request and returns a response
func (s *ManagedEmbeddingService) HandleEmbeddingRequest(req *EmbeddingRequest) (*EmbeddingResponse, error) {
	if req.Text == "" && len(req.Texts) == 0 {
		return nil, errors.New("either text or texts must be provided")
	}

	if req.Text != "" && len(req.Texts) > 0 {
		return nil, errors.New("provide either text or texts, not both")
	}

	// Get model info for dimensions
	info, err := s.GetModelInfo()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get model info")
	}

	resp := &EmbeddingResponse{
		Dimensions: info.Dimensions,
	}

	if req.Text != "" {
		// Single embedding
		result, err := s.GenerateEmbedding(req.Text)
		if err != nil {
			return nil, err
		}
		resp.Embeddings = [][]float32{result.Embedding}
		resp.Tokens = result.Tokens
		resp.InferenceMS = result.InferenceMS
	} else {
		// Batch embeddings
		result, err := s.GenerateBatchEmbeddings(req.Texts)
		if err != nil {
			return nil, err
		}
		resp.Embeddings = make([][]float32, len(result.Embeddings))
		for i, emb := range result.Embeddings {
			resp.Embeddings[i] = emb.Embedding
		}
		resp.Tokens = result.TotalTokens
		resp.InferenceMS = result.TotalInferenceMS
	}

	return resp, nil
}
