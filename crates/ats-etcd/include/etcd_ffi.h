/**
 * 「[Opus 5.5] ats-etcd FFI - C interface for the etcd window onto attestations.
 *
 * 「[Opus 5.5] Peer of ats-postgres's postgres_ffi.h, with the same result-type shape.」
 *
 * 「[Opus 5.5] Memory Management:」
 * 「[Opus 5.5] - Store pointers are freed with etcd_storage_free()」
 * 「[Opus 5.5] - Every result struct is freed with its *_result_free()」
 * 「[Opus 5.5] - A result that says success=false carries its reason in error_msg」」
 */

#ifndef QNTX_ETCD_FFI_H
#define QNTX_ETCD_FFI_H

#include <stdint.h>
#include <stdbool.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct EtcdStore EtcdStore;

typedef struct {
    bool success;
    char *error_msg;
} StorageResultC;

typedef struct {
    bool success;
    char *error_msg;
    EtcdStore *store;
} OpenResultC;

typedef struct EtcdKeys EtcdKeys;

typedef struct {
    bool success;
    char *error_msg;
    EtcdKeys *keys;
} KeysOpenResultC;

typedef struct {
    bool success;
    char *error_msg;
    char *json;
} HeldResultC;

KeysOpenResultC etcd_keys_new(const char *endpoint, const char *prefix);
void etcd_keys_free(EtcdKeys *keys);
StorageResultC etcd_keys_set(EtcdKeys *keys, const char *whole_json);
HeldResultC etcd_keys_held(EtcdKeys *keys);
void etcd_keys_open_result_free(KeysOpenResultC result);
void etcd_held_result_free(HeldResultC result);

OpenResultC etcd_storage_new(const char *endpoint, const char *ns);
void etcd_storage_free(EtcdStore *store);

StorageResultC etcd_storage_put(EtcdStore *store, const char *attestation_json);

void etcd_open_result_free(OpenResultC result);
void etcd_storage_result_free(StorageResultC result);

const char *etcd_storage_version(void);

#ifdef __cplusplus
}
#endif

#endif /* QNTX_ETCD_FFI_H */
