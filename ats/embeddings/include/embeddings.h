#ifndef QNTX_EMBEDDINGS_H
#define QNTX_EMBEDDINGS_H

#ifdef __cplusplus
extern "C" {
#endif

// Initialize the embedding engine with a model file
// Returns 0 on success, -1 on error
int embedding_engine_init(const char* model_path);

// Free the embedding engine
void embedding_engine_free(void);

// Get the dimensionality of embeddings
// Returns dimensions on success, -1 on error
int embedding_engine_dimensions(void);

// Embed a text string
// Parameters:
//   text: The text to embed
//   embedding_out: Pre-allocated buffer to store the embedding
//   dimensions: Size of the embedding_out buffer
// Returns: Number of dimensions written, or -1 on error
int embedding_engine_embed(const char* text, float* embedding_out, int dimensions);

// Embed a text and return JSON result
// Returns: JSON string (must be freed with embedding_free_string), or NULL on error
char* embedding_engine_embed_json(const char* text);

// Free a string returned by the FFI
void embedding_free_string(char* s);

#ifdef __cplusplus
}
#endif

#endif // QNTX_EMBEDDINGS_H