/**
 * qntx-sqlite FFI - C interface for SQLite attestation storage
 *
 * This header provides a C-compatible interface to the Rust qntx-sqlite library
 * for use with CGO and other FFI systems.
 *
 * Memory Management:
 * - All *_free() functions must be called to prevent memory leaks
 * - Caller owns all returned strings and must free them
 * - Store pointers must be freed with storage_free()
 */

#ifndef QNTX_STORAGE_FFI_H
#define QNTX_STORAGE_FFI_H

#include <stdint.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

// Opaque store handle
typedef struct SqliteStore SqliteStore;

// Result types
typedef struct {
    bool success;
    char *error_msg;
} StorageResultC;

typedef struct {
    bool success;
    char *error_msg;
    char *attestation_json; // NULL if not found
} AttestationResultC;

typedef struct {
    bool success;
    char *error_msg;
    char **strings;
    size_t strings_len;
} StringArrayResultC;

typedef struct {
    bool success;
    char *error_msg;
    size_t count;
} CountResultC;

// ============================================================================
// Store Lifecycle
// ============================================================================

/**
 * Create a new in-memory SQLite store (for testing).
 * Returns NULL on failure.
 * Must call storage_free() when done.
 */
SqliteStore *storage_new_memory(void);

/**
 * Create a new file-backed SQLite store.
 * Returns NULL on failure.
 * Must call storage_free() when done.
 *
 * @param path Filesystem path for database file
 */
SqliteStore *storage_new_file(const char *path);

/**
 * Free a store and release all resources.
 * Safe to call with NULL pointer.
 *
 * @param store Store to free
 */
void storage_free(SqliteStore *store);

// ============================================================================
// CRUD Operations
// ============================================================================

/**
 * Store a new attestation.
 *
 * @param store Store handle
 * @param attestation_json JSON-encoded attestation
 * @return Result indicating success/failure
 */
StorageResultC storage_put(SqliteStore *store, const char *attestation_json);

/**
 * Retrieve an attestation by ID.
 *
 * @param store Store handle
 * @param id Attestation ID
 * @return Result with JSON attestation (attestation_json is NULL if not found)
 */
AttestationResultC storage_get(const SqliteStore *store, const char *id);

/**
 * Check if an attestation exists.
 *
 * @param store Store handle
 * @param id Attestation ID
 * @return Result indicating existence (success=true means exists)
 */
StorageResultC storage_exists(const SqliteStore *store, const char *id);

/**
 * Delete an attestation by ID.
 *
 * @param store Store handle
 * @param id Attestation ID
 * @return Result indicating success (success=true means deleted)
 */
StorageResultC storage_delete(SqliteStore *store, const char *id);

/**
 * Update an existing attestation.
 *
 * @param store Store handle
 * @param attestation_json JSON-encoded attestation
 * @return Result indicating success/failure
 */
StorageResultC storage_update(SqliteStore *store, const char *attestation_json);

/**
 * Get all attestation IDs.
 *
 * @param store Store handle
 * @return Result with string array of IDs
 */
StringArrayResultC storage_ids(const SqliteStore *store);

/**
 * Get total count of attestations.
 *
 * @param store Store handle
 * @return Result with count
 */
CountResultC storage_count(const SqliteStore *store);

/**
 * Clear all attestations from the store.
 *
 * @param store Store handle
 * @return Result indicating success/failure
 */
StorageResultC storage_clear(SqliteStore *store);

// ============================================================================
// Memory Management
// ============================================================================

/**
 * Free a string returned by the library.
 *
 * @param s String to free
 */
void storage_string_free(char *s);

/**
 * Free a StorageResultC.
 *
 * @param result Result to free
 */
void storage_result_free(StorageResultC result);

/**
 * Free an AttestationResultC.
 *
 * @param result Result to free
 */
void attestation_result_free(AttestationResultC result);

/**
 * Free a StringArrayResultC.
 *
 * @param result Result to free
 */
void string_array_result_free(StringArrayResultC result);

/**
 * Free a CountResultC.
 *
 * @param result Result to free
 */
void count_result_free(CountResultC result);

// ============================================================================
// Utilities
// ============================================================================

/**
 * Get library version string (static, do not free).
 *
 * @return Version string
 */
const char *storage_version(void);

#ifdef __cplusplus
}
#endif

#endif // QNTX_STORAGE_FFI_H
