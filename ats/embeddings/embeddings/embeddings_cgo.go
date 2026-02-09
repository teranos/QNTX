//go:build cgo && rustembeddings

package embeddings

/*
#cgo LDFLAGS: -L${SRCDIR}/../../../target/release -lqntx_embeddings -L/nix/store/m4wq7714cbksjnc2ga1l09gwk2ww7hrf-onnxruntime-1.22.2/lib -lonnxruntime
#cgo darwin LDFLAGS: -framework CoreFoundation -framework Security -lresolv
#cgo CFLAGS: -I${SRCDIR}/../include

#include <stdlib.h>
#include "embeddings.h"
*/
import "C"
import (
	"encoding/json"
	"sync"
	"unsafe"

	"github.com/teranos/QNTX/errors"
)

// CGOEmbeddingService implements EmbeddingService using Rust FFI
type CGOEmbeddingService struct {
	mu          sync.Mutex
	initialized bool
	modelInfo   *ModelInfo
}

// NewEmbeddingService creates a new embedding service
func NewEmbeddingService() EmbeddingService {
	return &CGOEmbeddingService{}
}

// Init initializes the embedding engine with a model
func (s *CGOEmbeddingService) Init(modelPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		return errors.New("embedding engine already initialized")
	}

	cPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cPath))

	ret := C.embedding_engine_init(cPath)
	if ret != 0 {
		return errors.Newf("failed to initialize embedding engine with model: %s", modelPath)
	}

	// Get model dimensions
	dims := int(C.embedding_engine_dimensions())
	if dims < 0 {
		C.embedding_engine_free()
		return errors.New("failed to get model dimensions")
	}

	s.modelInfo = &ModelInfo{
		Name:              "sentence-transformers",
		Dimensions:        dims,
		MaxSequenceLength: 512, // Standard for most models
	}

	s.initialized = true
	return nil
}

// ModelInfo returns information about the loaded model
func (s *CGOEmbeddingService) ModelInfo() (*ModelInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.initialized {
		return nil, errors.New("embedding engine not initialized")
	}

	return s.modelInfo, nil
}

// Embed embeds a single text
func (s *CGOEmbeddingService) Embed(text string) (*EmbeddingResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.initialized {
		return nil, errors.New("embedding engine not initialized")
	}

	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))

	// Get JSON result from Rust
	cJSON := C.embedding_engine_embed_json(cText)
	if cJSON == nil {
		return nil, errors.Newf("failed to embed text: %s", text)
	}
	defer C.embedding_free_string(cJSON)

	// Parse JSON result
	jsonStr := C.GoString(cJSON)
	var result EmbeddingResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, errors.Wrap(err, "failed to parse embedding result")
	}

	return &result, nil
}

// EmbedBatch embeds multiple texts
func (s *CGOEmbeddingService) EmbedBatch(texts []string) (*BatchEmbeddingResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.initialized {
		return nil, errors.New("embedding engine not initialized")
	}

	result := &BatchEmbeddingResult{
		Embeddings: make([]EmbeddingResult, 0, len(texts)),
	}

	// Process each text sequentially
	// TODO: Implement proper batching in Rust
	for _, text := range texts {
		s.mu.Unlock() // Release lock for embedding
		embedding, err := s.Embed(text)
		s.mu.Lock() // Re-acquire lock

		if err != nil {
			return nil, errors.Wrapf(err, "failed to embed text in batch")
		}

		result.Embeddings = append(result.Embeddings, *embedding)
		result.TotalTokens += embedding.Tokens
		result.TotalInferenceMS += embedding.InferenceMS
	}

	return result, nil
}

// Close cleans up resources
func (s *CGOEmbeddingService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		C.embedding_engine_free()
		s.initialized = false
		s.modelInfo = nil
	}

	return nil
}
