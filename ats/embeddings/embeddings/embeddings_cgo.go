//go:build cgo && rustembeddings

package embeddings

/*
#cgo LDFLAGS: -L${SRCDIR}/../../../target/release -lqntx_embeddings
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

// CGOEmbeddingService implements EmbeddingService using Rust FFI.
// Go owns the engine pointer and manages synchronization via mu.
// Rust has no global state — all FFI calls take the engine pointer.
type CGOEmbeddingService struct {
	mu          sync.Mutex
	engine      *C.EmbeddingEngine
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

	engine := C.embedding_engine_init(cPath)
	if engine == nil {
		return errors.Newf("failed to initialize embedding engine with model: %s", modelPath)
	}

	dims := int(C.embedding_engine_dimensions(engine))
	if dims < 0 {
		C.embedding_engine_free(engine)
		return errors.New("failed to get model dimensions")
	}

	s.engine = engine
	s.modelInfo = &ModelInfo{
		Name:              "sentence-transformers",
		Dimensions:        dims,
		MaxSequenceLength: 512,
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

	return s.embedLocked(text)
}

// embedLocked performs the actual embedding. Caller must hold s.mu.
func (s *CGOEmbeddingService) embedLocked(text string) (*EmbeddingResult, error) {
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))

	cJSON := C.embedding_engine_embed_json(s.engine, cText)
	if cJSON == nil {
		return nil, errors.New("embedding engine returned null")
	}
	defer C.embedding_free_string(cJSON)

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

	for _, text := range texts {
		embedding, err := s.embedLocked(text)
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
		C.embedding_engine_free(s.engine)
		s.engine = nil
		s.initialized = false
		s.modelInfo = nil
	}

	return nil
}
