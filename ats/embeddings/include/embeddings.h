#ifndef QNTX_EMBEDDINGS_H
#define QNTX_EMBEDDINGS_H

#ifdef __cplusplus
extern "C" {
#endif

// Opaque engine pointer — all operations require this
typedef struct EmbeddingEngine EmbeddingEngine;

// Initialize the embedding engine with a model file
// Returns engine pointer on success, NULL on error
// Caller owns the pointer and must call embedding_engine_free()
EmbeddingEngine* embedding_engine_init(const char* model_path);

// Free the embedding engine
void embedding_engine_free(EmbeddingEngine* engine);

// Get the dimensionality of embeddings
// Returns dimensions on success, -1 on error
int embedding_engine_dimensions(const EmbeddingEngine* engine);

// Embed a text string
// Parameters:
//   engine: Valid engine pointer
//   text: The text to embed
//   embedding_out: Pre-allocated buffer to store the embedding
//   dimensions: Size of the embedding_out buffer
// Returns: Number of dimensions written, or -1 on error
int embedding_engine_embed(EmbeddingEngine* engine, const char* text, float* embedding_out, int dimensions);

// Embed a text and return JSON result
// Returns: JSON string (must be freed with embedding_free_string), or NULL on error
char* embedding_engine_embed_json(EmbeddingEngine* engine, const char* text);

// Free a string returned by the FFI
void embedding_free_string(char* s);

#ifdef __cplusplus
}
#endif

#endif // QNTX_EMBEDDINGS_H
