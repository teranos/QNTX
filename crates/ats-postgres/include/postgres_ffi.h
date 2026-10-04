/**
 * ats-postgres FFI - C interface for the Postgres attestation store.
 *
 * Peer of ats-duckdb's duckdb_ffi.h. Same result-type shape so Go can share
 * memory-management habits.
 *
 * Memory Management:
 * - Store pointers are freed with postgres_storage_free()
 * - Every result struct is freed with its *_result_free()
 * - A reason written into an error_out slot is freed with postgres_string_free()
 */

#ifndef QNTX_POSTGRES_FFI_H
#define QNTX_POSTGRES_FFI_H

#include <stdint.h>
#include <stdbool.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Opaque store handle */
typedef struct PostgresStore PostgresStore;

typedef struct {
    bool success;
    char *error_msg;
} StorageResultC;

typedef struct {
    bool success;
    char *error_msg;
    char *attestation_json;
} AttestationResultC;

typedef struct {
    bool success;
    char *error_msg;
    size_t count;
} CountResultC;

/* The tables a namespace's migrations leave, as a JSON array of names, and the
 * version the server says it is. */
typedef struct {
    bool success;
    char *error_msg;
    char *tables_json;
    char *server_version;
} SchemaResultC;

/* Connect to url (over TLS against the CA file ca when it is not empty), make
 * the namespace's schema and apply its migrations. NULL on failure, with the
 * reason in *error_out. */
PostgresStore *postgres_storage_new(const char *url, const char *ca, const char *namespace, char **error_out);
void postgres_storage_free(PostgresStore *store);

StorageResultC postgres_storage_put(PostgresStore *store, const char *attestation_json);
/* attestation_json is NULL when nothing is held under id. */
AttestationResultC postgres_storage_get(const PostgresStore *store, const char *id);
/* success is whether the attestation is held; error_msg says why asking failed. */
StorageResultC postgres_storage_exists(const PostgresStore *store, const char *id);
CountResultC postgres_storage_count(const PostgresStore *store);
AttestationResultC postgres_storage_query(const PostgresStore *store, const char *filter_json);
CountResultC postgres_storage_write_batch(const PostgresStore *store, const char *attestations_json);

SchemaResultC postgres_schema(const char *url, const char *ca, const char *namespace);

void postgres_string_free(char *s);
void postgres_storage_result_free(StorageResultC result);
void postgres_attestation_result_free(AttestationResultC result);
void postgres_count_result_free(CountResultC result);
void postgres_schema_result_free(SchemaResultC result);

const char *postgres_storage_version(void);

#ifdef __cplusplus
}
#endif

#endif /* QNTX_POSTGRES_FFI_H */
