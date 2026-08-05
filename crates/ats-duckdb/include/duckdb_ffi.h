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

/**
 * Filter query. filter_json is the JSON serialization of the Rust
 * QueryFilter struct (crates/qntx-duckdb/src/lib.rs). Returns a JSON
 * array of attestations in attestation_json (empty "[]" on no match).
 * Same input/output shape as qntx-sqlite's storage_query.
 */
AttestationResultC duckdb_storage_query(const DuckdbStore *store, const char *filter_json);

/**
 * Flush buffered attestations to a new Parquet file under
 * `<location>/attestations/`. No-op if the buffer is empty.
 * Called by Go periodically and at shutdown.
 */
StorageResultC     duckdb_storage_flush(const DuckdbStore *store);

/* Access tokens (ADR-025)
 *
 * A separate handle from the attestation store: tokens are one object each
 * under `<location>/access_tokens/`, not rows in the DuckDB table, and they
 * outlive any flush.
 *
 * now_ms is supplied by the caller on every operation needing a clock. Rust
 * never reads one, so the same inputs always give the same answer.
 */

typedef struct TokenStore TokenStore;

typedef struct {
    bool  success;
    char *error_msg;
    char *tokens_json;
} TokensResultC;

TokenStore *duckdb_tokens_new(const char *location);
void        duckdb_tokens_free(TokenStore *store);

/** Store a token. record_json is a TokenRecord (crates/qntx-duckdb/src/tokens.rs).
 *  The caller mints the raw token and hashes it; the raw value never crosses
 *  this boundary in either direction. */
StorageResultC duckdb_tokens_put(TokenStore *store, const char *record_json);

/** Whether the token with this hash authorizes a request at now_ms.
 *  success carries the answer; a false one with error_msg == NULL means
 *  "not usable", not "the store broke". The caller must tell those apart. */
StorageResultC duckdb_tokens_lookup(const TokenStore *store, const char *hash, int64_t now_ms);

/** Every token as a JSON array in tokens_json, hashes stripped.
 *  Free with duckdb_tokens_result_free. */
TokensResultC  duckdb_tokens_list(const TokenStore *store);

/** Revoke / un-revoke by token id. An id matching no token is an error, so a
 *  revoke that hit nothing cannot read as done. */
StorageResultC duckdb_tokens_revoke(TokenStore *store, const char *id, int64_t now_ms);
StorageResultC duckdb_tokens_enable(TokenStore *store, const char *id);

/** Record that the token with this hash was used at now_ms. */
StorageResultC duckdb_tokens_touch(TokenStore *store, const char *hash, int64_t now_ms);

/* Memory management */
void duckdb_string_free(char *s);
void duckdb_storage_result_free(StorageResultC result);
void duckdb_attestation_result_free(AttestationResultC result);
void duckdb_count_result_free(CountResultC result);
void duckdb_tokens_result_free(TokensResultC result);

/* Utilities */
const char *duckdb_storage_version(void);

#ifdef __cplusplus
}
#endif

#endif /* QNTX_DUCKDB_FFI_H */
