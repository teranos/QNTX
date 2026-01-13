/**
 * QNTX Fuzzy Engine - C API
 *
 * High-performance fuzzy matching library for QNTX.
 * This header provides the C interface for integration with Go via CGO.
 *
 * Memory Ownership Rules:
 * - fuzzy_engine_new() returns a pointer owned by the caller
 * - fuzzy_engine_free() must be called to deallocate the engine
 * - Result structs contain owned pointers that must be freed
 * - Use the corresponding _free() function for each result type
 *
 * Thread Safety:
 * - FuzzyEngine is internally thread-safe
 * - Multiple threads can call find_matches concurrently
 * - rebuild_index should not be called concurrently with find_matches
 */

#ifndef QNTX_FUZZY_ENGINE_H
#define QNTX_FUZZY_ENGINE_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/**
 * Opaque pointer to Rust FuzzyEngine.
 * Do not dereference - only pass to fuzzy_engine_* functions.
 */
typedef struct FuzzyEngine FuzzyEngine;

/**
 * Vocabulary type for matching.
 */
typedef enum {
    VOCAB_PREDICATES = 0,
    VOCAB_CONTEXTS = 1,
} VocabularyType;

/**
 * A single match result.
 * All string pointers are owned and must be freed.
 */
typedef struct {
    char *value;      /* Matched value (owned) */
    double score;     /* Match score 0.0-1.0 */
    char *strategy;   /* Strategy name: exact, prefix, etc. (owned) */
} RustMatchC;

/**
 * Result of find_matches operation.
 * Must be freed with fuzzy_match_result_free().
 */
typedef struct {
    bool success;           /* True if operation succeeded */
    char *error_msg;        /* Error message if !success (owned) */
    RustMatchC *matches;    /* Array of matches (owned) */
    size_t matches_len;     /* Number of matches */
    uint64_t search_time_us; /* Search time in microseconds */
} RustMatchResultC;

/**
 * Result of rebuild_index operation.
 * Must be freed with fuzzy_rebuild_result_free().
 */
typedef struct {
    bool success;           /* True if operation succeeded */
    char *error_msg;        /* Error message if !success (owned) */
    size_t predicate_count; /* Number of predicates indexed */
    size_t context_count;   /* Number of contexts indexed */
    uint64_t build_time_ms; /* Build time in milliseconds */
    char *index_hash;       /* Hash for change detection (owned) */
} RustRebuildResultC;

/* ============================================================================
 * Engine Lifecycle
 * ============================================================================ */

/**
 * Create a new FuzzyEngine instance.
 *
 * @return Pointer to new engine, or NULL on failure.
 *         Caller owns the pointer and must call fuzzy_engine_free().
 */
FuzzyEngine *fuzzy_engine_new(void);

/**
 * Free a FuzzyEngine instance.
 *
 * @param engine Engine to free (safe to pass NULL).
 */
void fuzzy_engine_free(FuzzyEngine *engine);

/* ============================================================================
 * Index Management
 * ============================================================================ */

/**
 * Rebuild the fuzzy index with new vocabulary.
 *
 * @param engine Valid engine pointer.
 * @param predicates Array of null-terminated predicate strings.
 * @param predicates_len Number of predicates.
 * @param contexts Array of null-terminated context strings.
 * @param contexts_len Number of contexts.
 * @return Result struct. Must free with fuzzy_rebuild_result_free().
 */
RustRebuildResultC fuzzy_engine_rebuild_index(
    const FuzzyEngine *engine,
    const char *const *predicates,
    size_t predicates_len,
    const char *const *contexts,
    size_t contexts_len
);

/**
 * Free a RustRebuildResultC.
 *
 * @param result Result to free.
 */
void fuzzy_rebuild_result_free(RustRebuildResultC result);

/* ============================================================================
 * Matching
 * ============================================================================ */

/**
 * Find matches for a query in the vocabulary.
 *
 * @param engine Valid engine pointer.
 * @param query Null-terminated UTF-8 query string.
 * @param vocabulary_type VOCAB_PREDICATES (0) or VOCAB_CONTEXTS (1).
 * @param limit Maximum results (0 for default of 20).
 * @param min_score Minimum score 0.0-1.0 (0.0 for default of 0.6).
 * @return Result struct. Must free with fuzzy_match_result_free().
 */
RustMatchResultC fuzzy_engine_find_matches(
    const FuzzyEngine *engine,
    const char *query,
    int vocabulary_type,
    size_t limit,
    double min_score
);

/**
 * Free a RustMatchResultC and all contained strings.
 *
 * @param result Result to free.
 */
void fuzzy_match_result_free(RustMatchResultC result);

/* ============================================================================
 * Utilities
 * ============================================================================ */

/**
 * Get the current index hash for change detection.
 *
 * @param engine Valid engine pointer.
 * @return Hash string (owned). Free with fuzzy_string_free().
 */
char *fuzzy_engine_get_hash(const FuzzyEngine *engine);

/**
 * Check if the engine index is ready (has vocabulary).
 *
 * @param engine Engine pointer.
 * @return True if index is ready.
 */
bool fuzzy_engine_is_ready(const FuzzyEngine *engine);

/**
 * Free a string returned by FFI functions.
 *
 * @param s String to free (safe to pass NULL).
 */
void fuzzy_string_free(char *s);

/**
 * Get the fuzzy-ax library version string.
 *
 * @return Version string (e.g., "0.1.0"). Do not free - points to static memory.
 */
const char *fuzzy_engine_version(void);

#ifdef __cplusplus
}
#endif

#endif /* QNTX_FUZZY_ENGINE_H */
