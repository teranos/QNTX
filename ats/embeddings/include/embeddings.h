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

// --- HDBSCAN clustering ---

// Result of HDBSCAN clustering
typedef struct ClusterResultC {
    int success;          // 1 on success, 0 on error
    char* error_msg;      // NULL on success; free with embedding_free_string
    int* labels;          // cluster IDs per point (-1 = noise); free with embedding_free_int_array
    float* probabilities; // membership per point [0,1]; free with embedding_free_float_array
    int count;            // number of points
    int n_clusters;       // distinct clusters (excl. noise)
    float* centroids;     // flat: n_clusters * centroid_dims; free with embedding_free_float_array
    int centroid_dims;    // dimensions per centroid (same as input dimensions)
} ClusterResultC;

// Run HDBSCAN clustering on embedding vectors
// data: flat array of n_points * dimensions floats
// Returns ClusterResultC; caller must free labels, probabilities, and error_msg
ClusterResultC embedding_cluster_hdbscan(
    const float* data, int n_points, int dimensions, int min_cluster_size);

// Free an int array returned by clustering
void embedding_free_int_array(int* arr, int len);

// Free a float array returned by clustering
void embedding_free_float_array(float* arr, int len);

#ifdef __cplusplus
}
#endif

#endif // QNTX_EMBEDDINGS_H
