/**
 * ats-duckdb FFI - C interface for the DuckDB/Parquet attestation store.
 *
 * Peer of ats-sqlite's storage_ffi.h. Same result-type shape so Go can share
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

/* Result types (shape-identical to ats-sqlite's) */
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
DuckdbStore *duckdb_storage_new(const char *location, const char *namespace_did,
                                char **error_out);

/**
 * Free a store and release all resources. Safe to call with NULL.
 */
void duckdb_storage_free(DuckdbStore *store);

/* CRUD */

StorageResultC     duckdb_storage_put(DuckdbStore *store, const char *attestation_json);
AttestationResultC duckdb_storage_get(const DuckdbStore *store, const char *id);
AttestationResultC duckdb_storage_get_many(const DuckdbStore *store, const char *ids_json);
StorageResultC     duckdb_storage_exists(const DuckdbStore *store, const char *id);
StorageResultC     duckdb_storage_delete(DuckdbStore *store, const char *id);
CountResultC       duckdb_storage_count(const DuckdbStore *store);
StorageResultC     duckdb_storage_clear(DuckdbStore *store);

/**
 * Filter query. filter_json is the JSON serialization of the Rust
 * QueryFilter struct (crates/ats-duckdb/src/lib.rs). Returns a JSON
 * array of attestations in attestation_json (empty "[]" on no match).
 * Same input/output shape as ats-sqlite's storage_query.
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
 * under `<location>/system/access_tokens/`, not rows in the DuckDB table, and
 * they outlive any flush. They sit in system rather than under the namespace
 * they authorize, because a bearer names no namespace until it is resolved.
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

TokenStore *duckdb_tokens_new(const char *location, char **error_out);
void        duckdb_tokens_free(TokenStore *store);

/** Store a token. record_json is a TokenRecord (crates/ats-duckdb/src/tokens.rs).
 *  The caller mints the raw token and hashes it; the raw value never crosses
 *  this boundary in either direction. */
StorageResultC duckdb_tokens_put(TokenStore *store, const char *record_json);

/** Whether the token with this hash authorizes a request at now_ms.
 *  success carries the answer; a false one with error_msg == NULL means
 *  "not usable", not "the store broke". The caller must tell those apart. */
StorageResultC duckdb_tokens_lookup(const TokenStore *store, const char *hash, int64_t now_ms);

/** The live token this hash names, as one TokenSummary object in tokens_json,
 *  or the JSON literal null when no live token has it. A bool cannot carry the
 *  namespace, scope and minter the middleware needs, which is why this exists.
 *  Free with duckdb_tokens_result_free. */
TokensResultC  duckdb_tokens_resolve(const TokenStore *store, const char *hash, int64_t now_ms);

/** Every token as a JSON array in tokens_json, hashes stripped.
 *  Free with duckdb_tokens_result_free. */
TokensResultC  duckdb_tokens_list(const TokenStore *store);

/** Revoke / un-revoke by token id. An id matching no token is an error, so a
 *  revoke that hit nothing cannot read as done. */
StorageResultC duckdb_tokens_revoke(TokenStore *store, const char *id, int64_t now_ms);
StorageResultC duckdb_tokens_enable(TokenStore *store, const char *id);

/** Replace what a token may read and write (TOKATTEST). scope_json is
 *  {"read":[...],"write":[...]} — one object, because they are one answer to
 *  what a token may touch. An id matching no token is an error. */
StorageResultC duckdb_tokens_set_scope(TokenStore *store, const char *id,
                                       const char *scope_json);

/** Record that the token with this hash was used at now_ms. */
StorageResultC duckdb_tokens_touch(TokenStore *store, const char *hash, int64_t now_ms);

/* Users (ADR-031). One object per User under <location>/system/users/,
 * rewritten whole. They sit in system because a User lives in namespaces
 * plural and cannot be kept inside one of them. */

/* The object is named by the User's own id, not by what reaches it: a User is
 * reached by any key or account it holds, and naming it after one of those
 * would make the others a second answer. */

typedef struct UserStore UserStore;

typedef struct {
    bool  success;
    char *error_msg;
    char *users_json;
} UsersResultC;

UserStore *duckdb_users_new(const char *location, char **error_out);
void       duckdb_users_free(UserStore *store);

/** Write a User. record_json is a UserRecord (crates/ats-duckdb/src/users.rs),
 *  keys and accounts included, and the object is replaced whole. */
StorageResultC duckdb_users_put(UserStore *store, const char *record_json);

/** The User an auth.root_identities entry reaches, as one UserRecord object in
 *  users_json, or the JSON literal null when none was minted for it.
 *  Free with duckdb_users_result_free. */
UsersResultC duckdb_users_by_route(const UserStore *store, const char *route);

/** Every User as a JSON array in users_json. How many there are is what
 *  decides whether the next admission mints ROOT.
 *  Free with duckdb_users_result_free. */
UsersResultC duckdb_users_list(const UserStore *store);

/* Namespaces (ADR-026, ADR-027). A namespace is the top-level prefix and
 * nothing else, so creating one writes whose it is and that write is what makes
 * it exist. Listing reads the objects under a location.
 */

typedef struct NamespaceStore NamespaceStore;

typedef struct {
    bool  success;
    char *error_msg;
    char *namespaces_json;
} NamespacesResultC;

NamespaceStore *duckdb_namespaces_new(const char *location, char **error_out);
void            duckdb_namespaces_free(NamespaceStore *store);

/** Every namespace as a JSON array in namespaces_json — name, owner or null,
 *  and the kinds it holds. Free with duckdb_namespaces_result_free. */
NamespacesResultC duckdb_namespaces_list(const NamespaceStore *store);

/** Create name by recording who owns it. owner_json is an Owner. A name that
 *  already carries one is an error. */
StorageResultC duckdb_namespaces_create(const NamespaceStore *store, const char *name,
                                        const char *owner_json);

/** The system namespace's signer identity (ADR-026): one record per location. */
typedef struct IdentityStore IdentityStore;

IdentityStore *duckdb_identity_new(const char *location, char **error_out);
void           duckdb_identity_free(IdentityStore *store);

/** Identity JSON in tokens_json, empty when there is none — first boot, not an
 *  error. Free with duckdb_tokens_result_free. */
TokensResultC  duckdb_identity_load(const IdentityStore *store);

StorageResultC duckdb_identity_save(IdentityStore *store, const char *record_json);

/* Watchers: a declaration is one object under `<location>/watchers/`; a fire
 * is a row under `<location>/watcher_fires/`, and the tally aggregates those
 * rather than being a column anyone writes. */

typedef struct WatcherStore WatcherStore;

typedef struct {
    bool  success;
    char *error_msg;
    char *watchers_json;
} WatchersResultC;

WatcherStore *duckdb_watchers_new(const char *location, const char *namespace_did,
                                  char **error_out);

/** Flushes buffered fires before closing. */
/* Returns NULL when the buffered fires were written, and the reason when they
   were not — fires that happened and are now gone. Free it with
   duckdb_string_free(). */
char *duckdb_watchers_free(WatcherStore *store);

/** Declare a watcher. record_json is a WatcherRecord
 *  (crates/ats-duckdb/src/watchers.rs). Returns when the object is durable. */
StorageResultC duckdb_watchers_put(WatcherStore *store, const char *record_json);

/** Every declaration as a JSON array in watchers_json.
 *  Free with duckdb_watchers_result_free. */
WatchersResultC duckdb_watchers_list(const WatcherStore *store);

/** Withdraw a declaration. An id matching nothing is an error, so a delete
 *  that hit nothing cannot read as done. The fires it emitted stay. */
StorageResultC duckdb_watchers_delete(WatcherStore *store, const char *id);

/** The last `limit` fires of one watcher as a JSON array, newest first.
 *  Free with duckdb_watchers_result_free. */
WatchersResultC duckdb_watchers_recent_fires(const WatcherStore *store, const char *id,
                                             int64_t limit);

/** Note a fire or an error. Both return without reaching storage.
 *  attestation_id is what caused it; empty for a run nothing triggered. */
StorageResultC duckdb_watchers_record_fire(WatcherStore *store, const char *id, int64_t at_ms,
                                           const char *attestation_id);
StorageResultC duckdb_watchers_record_error(WatcherStore *store, const char *id, int64_t at_ms,
                                            const char *message, const char *attestation_id);

/** Write the buffered events as one file; the caller decides how often. */
StorageResultC duckdb_watchers_flush(WatcherStore *store);

/** The counters for one watcher, as JSON in watchers_json. An id that never
 *  fired has a zero tally rather than an error. */
WatchersResultC duckdb_watchers_tally(const WatcherStore *store, const char *id);

/* Schedules: a declaration is one object under `<location>/schedules/`; a tick
 * is a row under `<location>/schedule_ticks/`, and next run, last run and last
 * execution aggregate those rather than being columns anyone writes.
 * See ADR-028. */

typedef struct ScheduleStore ScheduleStore;

typedef struct {
    bool  success;
    char *error_msg;
    char *schedules_json;
} SchedulesResultC;

ScheduleStore *duckdb_schedules_new(const char *location, const char *namespace_did,
                                    char **error_out);

/** Flushes buffered ticks before closing. */
/* Returns NULL when the buffered ticks were written, and the reason when they
   were not — a schedule that will run again believing it never ran. Free it
   with duckdb_string_free(). */
char *duckdb_schedules_free(ScheduleStore *store);

/** Declare a schedule. declaration_json is a protocol.ScheduleDeclaration.
 *  Returns when the object is durable. */
StorageResultC duckdb_schedules_put(ScheduleStore *store, const char *declaration_json);

/** Every declaration as a JSON array in schedules_json.
 *  Free with duckdb_schedules_result_free. */
SchedulesResultC duckdb_schedules_list(const ScheduleStore *store);

/** Active declarations owed at now_ms. Paused ones keep their next run. */
SchedulesResultC duckdb_schedules_due(const ScheduleStore *store, int64_t now_ms);

/** The soonest run owed, as an array of nought or one. */
SchedulesResultC duckdb_schedules_next(const ScheduleStore *store);

/** Withdraw a declaration. An id matching nothing is an error, so a delete
 *  that hit nothing cannot read as done. The ticks it emitted stay. */
StorageResultC duckdb_schedules_delete(ScheduleStore *store, const char *id);

/** Note a run, or move the next run without one. Both buffer. */
StorageResultC duckdb_schedules_record_run(ScheduleStore *store, const char *id, int64_t at_ms,
                                           const char *execution_id, int64_t next_run_at_ms);
StorageResultC duckdb_schedules_reschedule(ScheduleStore *store, const char *id, int64_t at_ms,
                                           int64_t next_run_at_ms);

/** Write the buffered ticks as one file; the caller decides how often. */
StorageResultC duckdb_schedules_flush(ScheduleStore *store);

/** What the ticks derive for one schedule, as a protocol.ScheduleProgress in
 *  schedules_json. An id that never ran has zeroes rather than an error. */
SchedulesResultC duckdb_schedules_progress(const ScheduleStore *store, const char *id);

/* Memory management */
void duckdb_string_free(char *s);
void duckdb_storage_result_free(StorageResultC result);
void duckdb_attestation_result_free(AttestationResultC result);
void duckdb_count_result_free(CountResultC result);
void duckdb_tokens_result_free(TokensResultC result);
void duckdb_users_result_free(UsersResultC result);
void duckdb_namespaces_result_free(NamespacesResultC result);
void duckdb_watchers_result_free(WatchersResultC result);
void duckdb_schedules_result_free(SchedulesResultC result);

/* Utilities */
const char *duckdb_storage_version(void);

#ifdef __cplusplus
}
#endif

#endif /* QNTX_DUCKDB_FFI_H */
