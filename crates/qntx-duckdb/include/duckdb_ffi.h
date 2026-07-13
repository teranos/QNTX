/**
 * qntx-duckdb FFI - C interface for the DuckDB/Parquet attestation store.
 *
 * Peer of qntx-sqlite's storage_ffi.h. Same result-type shape so Go can share
 * memory-management helpers. See ADR-024 for the design.
 *
 * Memory Management:
 * - All *_free() functions must be called to prevent leaks
 * - Caller owns all returned strings and must free them via duckdb_string_free()
 * - Store pointers must be freed with duckdb_storage_free()
 */

#ifndef QNTX_DUCKDB_FFI_H
#define QNTX_DUCKDB_FFI_H

#include <stdint.h>
#include <stdbool.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Opaque store handle */
typedef struct DuckdbStore DuckdbStore;

/* Result types (shape-identical to qntx-sqlite's) */
typedef struct {
    bool success;
    char *error_msg;
} StorageResultC;

typedef struct {
    bool success;
    char *error_msg;
    char *attestation_json; /* NULL if not found */
} AttestationResultC;

typedef struct {
    bool success;
    char *error_msg;
    size_t count;
} CountResultC;

/* Store lifecycle */

/**
 * Open a DuckDB-backed store at the given location URL.
 * Location may be "s3://bucket/prefix" or "file:///path".
 * Returns NULL on failure (details logged to stderr).
 * Must call duckdb_storage_free() when done.
 */
DuckdbStore *duckdb_storage_new(const char *location);

/**
 * Free a store and release all resources. Safe to call with NULL.
 */
void duckdb_storage_free(DuckdbStore *store);

/* CRUD */

StorageResultC     duckdb_storage_put(DuckdbStore *store, const char *attestation_json);
AttestationResultC duckdb_storage_get(const DuckdbStore *store, const char *id);
StorageResultC     duckdb_storage_exists(const DuckdbStore *store, const char *id);
StorageResultC     duckdb_storage_delete(DuckdbStore *store, const char *id);
CountResultC       duckdb_storage_count(const DuckdbStore *store);
StorageResultC     duckdb_storage_clear(DuckdbStore *store);

/* Memory management */
void duckdb_string_free(char *s);
void duckdb_storage_result_free(StorageResultC result);
void duckdb_attestation_result_free(AttestationResultC result);
void duckdb_count_result_free(CountResultC result);

/* Utilities */
const char *duckdb_storage_version(void);

#ifdef __cplusplus
}
#endif

#endif /* QNTX_DUCKDB_FFI_H */
