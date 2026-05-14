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

// Opaque store handles
typedef struct SqliteStore SqliteStore;
typedef struct ReadConn ReadConn;
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
// Read Connection (separate pointer, concurrent with store)
// ============================================================================

/**
 * Open a read-only connection from a file-backed store.
 * Returns NULL for in-memory stores or on failure.
 */
ReadConn *storage_open_read_conn(const SqliteStore *store);

/**
 * Free a read connection.
 */
void read_conn_free(ReadConn *rc);

/**
 * Get an attestation by ID through the read connection.
 */
AttestationResultC read_conn_get(const ReadConn *rc, const char *id);

/**
 * Check if an attestation exists through the read connection.
 */
StorageResultC read_conn_exists(const ReadConn *rc, const char *id);

/**
 * Query attestations through the read connection.
 */
AttestationResultC read_conn_query(const ReadConn *rc, const char *filter_json);

/**
 * Get all attestation IDs through the read connection.
 */
StringArrayResultC read_conn_ids(const ReadConn *rc);

/**
 * Get total count through the read connection.
 */
CountResultC read_conn_count(const ReadConn *rc);

/**
 * Get distinct predicates through the read connection.
 */
StringArrayResultC read_conn_predicates(const ReadConn *rc);

/**
 * Get distinct contexts through the read connection.
 */
StringArrayResultC read_conn_contexts(const ReadConn *rc);

/**
 * Get storage stats through the read connection.
 */
AttestationResultC read_conn_stats(const ReadConn *rc);

/**
 * Execute a raw SELECT query through the read connection.
 */
AttestationResultC read_conn_query_raw(const ReadConn *rc, const char *sql, const char *params_json);

/**
 * Run PRAGMA integrity_check through the read connection.
 */
StringArrayResultC read_conn_integrity_check(const ReadConn *rc);

/**
 * Set enforcement config on the store.
 * When set, enforcement runs automatically after every storage_put().
 *
 * @param store Store handle
 * @param config_json JSON enforcement config, e.g. {"actor_context_limit":16,"actor_contexts_limit":64,"entity_actors_limit":64}
 * @return Result indicating success/failure
 */
StorageResultC storage_set_enforcement_config(SqliteStore *store, const char *config_json);

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

/**
 * Query attestations with filters.
 *
 * @param store Store handle
 * @param filter_json JSON-encoded AxFilter
 * @return Result with JSON array of matching attestations
 */
AttestationResultC storage_query(const SqliteStore *store, const char *filter_json);

// ============================================================================
// Enforcement & Stats
// ============================================================================

/**
 * Enforce storage limits and log events to storage_events table.
 * Returns JSON array of enforcement events that occurred.
 *
 * @param store Store handle
 * @param input_json JSON with actors, contexts, subjects, and config
 * @return Result with JSON array of enforcement events
 */
AttestationResultC storage_enforce_limits(SqliteStore *store, const char *input_json);

/**
 * Get storage statistics (counts of attestations, unique actors/subjects/contexts).
 *
 * @param store Store handle
 * @return Result with JSON stats object
 */
AttestationResultC storage_get_stats(const SqliteStore *store);

// ============================================================================
// Distinct Value Queries
// ============================================================================

/**
 * Get all distinct predicates.
 */
StringArrayResultC storage_predicates(const SqliteStore *store);

/**
 * Get all distinct contexts.
 */
StringArrayResultC storage_contexts(const SqliteStore *store);

// ============================================================================
// Raw Query (Go query builder → Rust connection)
// ============================================================================

/**
 * Execute a raw SELECT query against attestations through Rust's connection.
 * Go keeps its query builder; Rust just executes the SQL.
 * Query MUST select standard attestation columns in order.
 *
 * @param store Store handle
 * @param sql SQL SELECT query string
 * @param params_json JSON array of bind parameters, e.g. ["value1", 42]
 * @return Result with JSON array of matching attestations
 */
AttestationResultC storage_query_raw(const SqliteStore *store, const char *sql, const char *params_json);

// ============================================================================
// Generic SQL Execution (Go database/sql/driver)
// ============================================================================

typedef struct {
    bool success;
    char *error_msg;
    int64_t last_insert_id;
    int64_t rows_affected;
} ExecResultC;

typedef struct {
    bool success;
    char *error_msg;
    char *columns_json; // JSON array of column names
    char *rows_json;    // JSON array of row arrays
} QueryResultC;

/**
 * Execute a non-query SQL statement (INSERT, UPDATE, DELETE, DDL).
 *
 * @param store Store handle
 * @param sql SQL statement
 * @param params_json JSON array of bind parameters
 * @return Result with last_insert_id and rows_affected
 */
ExecResultC sql_exec(SqliteStore *store, const char *sql, const char *params_json);

/**
 * Execute a query SQL statement (SELECT).
 * Returns all rows pre-fetched as JSON.
 *
 * @param store Store handle
 * @param sql SQL SELECT query
 * @param params_json JSON array of bind parameters
 * @return Result with columns_json and rows_json
 */
QueryResultC sql_query(const SqliteStore *store, const char *sql, const char *params_json);

/**
 * Begin an immediate transaction (BEGIN IMMEDIATE).
 */
ExecResultC sql_begin(SqliteStore *store);

/**
 * Commit the current transaction.
 */
ExecResultC sql_commit(SqliteStore *store);

/**
 * Rollback the current transaction.
 */
ExecResultC sql_rollback(SqliteStore *store);

/**
 * Execute a SELECT query through the read connection (for database/sql/driver).
 */
QueryResultC read_conn_sql_query(const ReadConn *rc, const char *sql, const char *params_json);

/**
 * Set the caller tag for the current thread's flight recorder entries.
 * Call before issuing FFI calls to attribute queries to their source.
 *
 * @param caller Caller identifier (e.g. "db-stats", "watcher:ax-1234", "plugin:village")
 */
void flight_recorder_set_caller(const char *caller);

/**
 * Free an ExecResultC.
 */
void exec_result_free(ExecResultC result);

/**
 * Free a QueryResultC.
 */
void query_result_free(QueryResultC result);

// ============================================================================
// Integrity
// ============================================================================

/**
 * Run PRAGMA integrity_check on the database.
 * A healthy database returns a single string: "ok".
 *
 * @param store Store handle
 * @return Result with string array of integrity check lines
 */
StringArrayResultC storage_integrity_check(const SqliteStore *store);

// ============================================================================
// Backup
// ============================================================================

/**
 * Create a hot backup of the database.
 * Opens its own read-only source connection — does not touch the store pointer.
 *
 * @param src_path Source database path
 * @param dest_path Filesystem path for the backup file
 * @return Result indicating success/failure
 */
StorageResultC storage_backup(const char *src_path, const char *dest_path);

/**
 * Deliberately trigger SIGBUS to verify flight recorder.
 * Development/testing only.
 */
void storage_crash_test(void);

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
