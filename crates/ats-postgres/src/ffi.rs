//! C-compatible FFI for the Postgres attestation store.
//!
//! Same result types and ownership rules as `ats-duckdb/src/ffi.rs`: the
//! store is allocated on the Rust heap and freed with `postgres_storage_free`;
//! strings in result structs belong to the caller and are freed with the
//! matching `*_result_free`.

use std::os::raw::c_char;
use std::ptr;

use ats::storage::{AttestationStore, StoreError};
use qntx_ffi_common::{
    cstr_to_str, cstring_new_or_empty, cstring_new_or_fallback, free_boxed, free_cstring, FfiResult,
};
use qntx_proto::proto_convert;

use crate::error::PostgresError;
use crate::{PostgresStore, QueryFilter};

const MAX_ID_LENGTH: usize = 256;
const MAX_JSON_LENGTH: usize = 1_000_000;
/// The largest batch `postgres_storage_write_batch` takes.
const MAX_BATCH_JSON_LENGTH: usize = 256_000_000;

#[repr(C)]
pub struct StorageResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
}

#[repr(C)]
pub struct AttestationResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub attestation_json: *mut c_char,
}

#[repr(C)]
pub struct CountResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub count: usize,
}

/// The tables a namespace's migrations leave, as a JSON array of names, and
/// the version the server says it is.
#[repr(C)]
pub struct SchemaResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub tables_json: *mut c_char,
    pub server_version: *mut c_char,
}

impl FfiResult for StorageResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";
    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
        }
    }
}

impl FfiResult for AttestationResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";
    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            attestation_json: ptr::null_mut(),
        }
    }
}

impl FfiResult for CountResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";
    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            count: 0,
        }
    }
}

impl FfiResult for SchemaResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";
    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            tables_json: ptr::null_mut(),
            server_version: ptr::null_mut(),
        }
    }
}

fn ok() -> StorageResultC {
    StorageResultC {
        success: true,
        error_msg: ptr::null_mut(),
    }
}

fn attestation_json(json: String) -> AttestationResultC {
    AttestationResultC {
        success: true,
        error_msg: ptr::null_mut(),
        attestation_json: cstring_new_or_empty(&json),
    }
}

fn counted(count: usize) -> CountResultC {
    CountResultC {
        success: true,
        error_msg: ptr::null_mut(),
        count,
    }
}

/// What an error is when it leaves through the FFI: the sacred shape, as the
/// bytes of its JSON, naming the call it left through.
trait Crosses {
    fn crosses(self, call: &str) -> String;
}

impl Crosses for PostgresError {
    fn crosses(self, call: &str) -> String {
        self.sacred_json(call)
    }
}

impl Crosses for serde_json::Error {
    fn crosses(self, call: &str) -> String {
        PostgresError::Serde(self).sacred_json(call)
    }
}

impl Crosses for qntx_ffi_common::CStrError {
    fn crosses(self, call: &str) -> String {
        PostgresError::BadArgument {
            call: "",
            argument: "",
            source: self,
        }
        .sacred_json(call)
    }
}

/// The trait's error as the sacred shape. A `Backend` already carries the
/// shape as its string; the trait's own variants are built into one here.
impl Crosses for StoreError {
    fn crosses(self, call: &str) -> String {
        let region = match &self {
            StoreError::AlreadyExists(_) => "already-exists",
            StoreError::NotFound(_) => "not-found",
            StoreError::InvalidData(_) => "invalid-data",
            StoreError::Backend(_) => "backend",
            StoreError::Query(_) => "query",
            StoreError::Serialization(_) => "serialization",
            StoreError::QuotaExceeded { .. } => "quota-exceeded",
        };
        if let StoreError::Backend(json) = &self {
            if serde_json::from_str::<laye_error::Error>(json).is_ok() {
                return json.clone();
            }
        }
        let at = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_millis().to_string())
            .unwrap_or_default();
        let sacred = laye_error::Error {
            id: format!("err-{}-{at}", crate::error::SURFACE),
            severity: laye_error::Severity::Error,
            context: laye_error::Context {
                surface: crate::error::SURFACE.to_string(),
                region: Some(region.to_string()),
                anchor: None,
            },
            title: self.to_string(),
            why: String::new(),
            trace: Vec::new(),
            raw: Some(format!("{self:?}")),
            at,
            source: None,
            ffi_call: (!call.is_empty()).then(|| call.to_string()),
            location: None,
            js_stack: None,
            raw_stderr: None,
            requires_reload: false,
        };
        serde_json::to_string(&sacred).unwrap_or_default()
    }
}

/// A C string argument as a `&str`, or the typed refusal naming which
/// argument of which call was not one.
unsafe fn argument<'a>(
    call: &'static str,
    name: &'static str,
    ptr: *const c_char,
) -> Result<&'a str, PostgresError> {
    unsafe { cstr_to_str(ptr) }.map_err(|source| PostgresError::BadArgument {
        call,
        argument: name,
        source,
    })
}

/// Connect to `url` (over TLS against the CA at `ca` when it is not empty),
/// make the namespace's schema and apply its migrations. Returns NULL on
/// failure, writing the reason into `error_out`, which the caller frees with
/// `postgres_string_free`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn postgres_storage_new(
    url: *const c_char,
    ca: *const c_char,
    namespace: *const c_char,
    error_out: *mut *mut c_char,
) -> *mut PostgresStore {
    qntx_ffi_common::guarded(
        "postgres_storage_new",
        || {
            const CALL: &str = "postgres_storage_new";
            let fail = |e: PostgresError| -> *mut PostgresStore {
                if !error_out.is_null() {
                    let json = e.sacred_json(CALL);
                    unsafe {
                        *error_out =
                            cstring_new_or_fallback(&json, "the reason could not be encoded")
                    };
                }
                ptr::null_mut()
            };
            let url = match unsafe { argument(CALL, "url", url) } {
                Ok(s) => s,
                Err(e) => return fail(e),
            };
            let ca = match unsafe { argument(CALL, "ca", ca) } {
                Ok(s) => s,
                Err(e) => return fail(e),
            };
            let ns = match unsafe { argument(CALL, "namespace", namespace) } {
                Ok(s) => s,
                Err(e) => return fail(e),
            };
            match PostgresStore::open(url, ca, ns) {
                Ok(store) => Box::into_raw(Box::new(store)),
                Err(e) => fail(e),
            }
        },
        |_| ptr::null_mut(),
    )
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn postgres_storage_free(store: *mut PostgresStore) {
    qntx_ffi_common::guarded(
        "postgres_storage_free",
        || unsafe { free_boxed(store) },
        |_| (),
    )
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn postgres_storage_put(
    store: *mut PostgresStore,
    attestation_json: *const c_char,
) -> StorageResultC {
    qntx_ffi_common::guarded_result("postgres_storage_put", || {
        const CALL: &str = "postgres_storage_put";
        if store.is_null() {
            return StorageResultC::error("null store pointer");
        }
        let json = match unsafe { cstr_to_str(attestation_json) } {
            Ok(s) => s,
            Err(e) => return StorageResultC::error(e.crosses(CALL)),
        };
        if json.len() > MAX_JSON_LENGTH {
            return StorageResultC::error("attestation JSON exceeds maximum length");
        }
        let proto: qntx_proto::Attestation = match serde_json::from_str(json) {
            Ok(a) => a,
            Err(e) => return StorageResultC::error(e.crosses(CALL)),
        };
        let store = unsafe { &mut *store };
        match store.put(proto_convert::from_proto(proto)) {
            Ok(()) => ok(),
            Err(e) => StorageResultC::error(e.crosses(CALL)),
        }
    })
}

/// The attestation under `id`, or a NULL `attestation_json` when none is.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn postgres_storage_get(
    store: *const PostgresStore,
    id: *const c_char,
) -> AttestationResultC {
    qntx_ffi_common::guarded_result("postgres_storage_get", || {
        const CALL: &str = "postgres_storage_get";
        if store.is_null() {
            return AttestationResultC::error("null store pointer");
        }
        let id = match unsafe { cstr_to_str(id) } {
            Ok(s) => s,
            Err(e) => return AttestationResultC::error(e.crosses(CALL)),
        };
        if id.len() > MAX_ID_LENGTH {
            return AttestationResultC::error("ID exceeds maximum length");
        }
        match unsafe { &*store }.get(id) {
            Ok(Some(attestation)) => {
                match serde_json::to_string(&proto_convert::to_proto(attestation)) {
                    Ok(json) => attestation_json(json),
                    Err(e) => AttestationResultC::error(e.crosses(CALL)),
                }
            }
            Ok(None) => AttestationResultC {
                success: true,
                error_msg: ptr::null_mut(),
                attestation_json: ptr::null_mut(),
            },
            Err(e) => AttestationResultC::error(e.crosses(CALL)),
        }
    })
}

/// `success` is whether the attestation is held. A failure to ask carries its
/// reason in `error_msg`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn postgres_storage_exists(
    store: *const PostgresStore,
    id: *const c_char,
) -> StorageResultC {
    qntx_ffi_common::guarded_result("postgres_storage_exists", || {
        const CALL: &str = "postgres_storage_exists";
        if store.is_null() {
            return StorageResultC::error("null store pointer");
        }
        let id = match unsafe { cstr_to_str(id) } {
            Ok(s) => s,
            Err(e) => return StorageResultC::error(e.crosses(CALL)),
        };
        match unsafe { &*store }.exists(id) {
            Ok(true) => ok(),
            Ok(false) => StorageResultC {
                success: false,
                error_msg: ptr::null_mut(),
            },
            Err(e) => StorageResultC::error(e.crosses(CALL)),
        }
    })
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn postgres_storage_count(store: *const PostgresStore) -> CountResultC {
    qntx_ffi_common::guarded_result("postgres_storage_count", || {
        if store.is_null() {
            return CountResultC::error("null store pointer");
        }
        match unsafe { &*store }.count() {
            Ok(count) => counted(count),
            Err(e) => CountResultC::error(e.crosses("postgres_storage_count")),
        }
    })
}

/// Attestations matching a JSON filter, as a JSON array, newest first. Same
/// shapes as `duckdb_storage_query`.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn postgres_storage_query(
    store: *const PostgresStore,
    filter_json: *const c_char,
) -> AttestationResultC {
    qntx_ffi_common::guarded_result("postgres_storage_query", || {
        const CALL: &str = "postgres_storage_query";
        if store.is_null() {
            return AttestationResultC::error("null store pointer");
        }
        let json = match unsafe { cstr_to_str(filter_json) } {
            Ok(s) => s,
            Err(e) => return AttestationResultC::error(e.crosses(CALL)),
        };
        if json.len() > MAX_JSON_LENGTH {
            return AttestationResultC::error("filter JSON exceeds maximum length");
        }
        let filter: QueryFilter = match serde_json::from_str(json) {
            Ok(f) => f,
            Err(e) => return AttestationResultC::error(e.crosses(CALL)),
        };
        let attestations = match unsafe { &*store }.query(&filter) {
            Ok(a) => a,
            Err(e) => return AttestationResultC::error(e.crosses(CALL)),
        };
        let protos: Vec<qntx_proto::Attestation> = attestations
            .into_iter()
            .map(proto_convert::to_proto)
            .collect();
        match serde_json::to_string(&protos) {
            Ok(json) => attestation_json(json),
            Err(e) => AttestationResultC::error(e.crosses(CALL)),
        }
    })
}

/// Write a JSON array of attestations the landing file sent, in one
/// transaction. The count is the rows of the batch the record now holds.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn postgres_storage_write_batch(
    store: *const PostgresStore,
    attestations_json: *const c_char,
) -> CountResultC {
    qntx_ffi_common::guarded_result("postgres_storage_write_batch", || {
        const CALL: &str = "postgres_storage_write_batch";
        if store.is_null() {
            return CountResultC::error("null store pointer");
        }
        let json = match unsafe { cstr_to_str(attestations_json) } {
            Ok(s) => s,
            Err(e) => return CountResultC::error(e.crosses(CALL)),
        };
        if json.len() > MAX_BATCH_JSON_LENGTH {
            return CountResultC::error("attestation batch JSON exceeds maximum length");
        }
        let protos: Vec<qntx_proto::Attestation> = match serde_json::from_str(json) {
            Ok(p) => p,
            Err(e) => return CountResultC::error(e.crosses(CALL)),
        };
        let attestations: Vec<_> = protos.into_iter().map(proto_convert::from_proto).collect();
        match unsafe { &*store }.write_batch(&attestations) {
            Ok(rows) => counted(rows),
            Err(e) => CountResultC::error(e.crosses(CALL)),
        }
    })
}

/// The tables a namespace's migrations leave standing, applied by this crate's
/// runner, and the version the server says it is.
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn postgres_schema(
    url: *const c_char,
    ca: *const c_char,
    namespace: *const c_char,
) -> SchemaResultC {
    qntx_ffi_common::guarded_result("postgres_schema", || {
        const CALL: &str = "postgres_schema";
        let args = || -> Result<(&str, &str, &str), PostgresError> {
            Ok((
                unsafe { argument(CALL, "url", url) }?,
                unsafe { argument(CALL, "ca", ca) }?,
                unsafe { argument(CALL, "namespace", namespace) }?,
            ))
        };
        let (url, ca, ns) = match args() {
            Ok(a) => a,
            Err(e) => return SchemaResultC::error(e.crosses(CALL)),
        };
        let (tables, version) = match crate::schema_tables(url, ca, ns) {
            Ok(found) => found,
            Err(e) => return SchemaResultC::error(e.crosses(CALL)),
        };
        match serde_json::to_string(&tables) {
            Ok(json) => SchemaResultC {
                success: true,
                error_msg: ptr::null_mut(),
                tables_json: cstring_new_or_empty(&json),
                server_version: cstring_new_or_empty(&version),
            },
            Err(e) => SchemaResultC::error(e.crosses(CALL)),
        }
    })
}

#[no_mangle]
pub extern "C" fn postgres_storage_result_free(result: StorageResultC) {
    qntx_ffi_common::guarded(
        "postgres_storage_result_free",
        || unsafe { free_cstring(result.error_msg) },
        |_| (),
    )
}

#[no_mangle]
pub extern "C" fn postgres_attestation_result_free(result: AttestationResultC) {
    qntx_ffi_common::guarded(
        "postgres_attestation_result_free",
        || unsafe {
            free_cstring(result.error_msg);
            free_cstring(result.attestation_json);
        },
        |_| (),
    )
}

#[no_mangle]
pub extern "C" fn postgres_count_result_free(result: CountResultC) {
    qntx_ffi_common::guarded(
        "postgres_count_result_free",
        || unsafe { free_cstring(result.error_msg) },
        |_| (),
    )
}

#[no_mangle]
pub extern "C" fn postgres_schema_result_free(result: SchemaResultC) {
    qntx_ffi_common::guarded(
        "postgres_schema_result_free",
        || unsafe {
            free_cstring(result.error_msg);
            free_cstring(result.tables_json);
            free_cstring(result.server_version);
        },
        |_| (),
    )
}

qntx_ffi_common::define_string_free!(postgres_string_free);
qntx_ffi_common::define_version_fn!(postgres_storage_version);
