//! 「[Opus 5.5] C-compatible FFI for the etcd window: what the node needs to put
//! each attestation it creates into it. Same result types and ownership rules
//! as ats-postgres's ffi.rs: the store is allocated on the Rust heap and freed
//! with `etcd_storage_free`; strings in result structs belong to the caller and
//! are freed with the matching `*_result_free`.」

use std::os::raw::c_char;
use std::ptr;

use ats::storage::{AttestationStore, StoreError};
use qntx_ffi_common::{cstr_to_str, free_boxed, free_cstring, FfiResult};
use qntx_proto::proto_convert;

use crate::error::EtcdError;
use crate::EtcdStore;

const MAX_JSON_LENGTH: usize = 1_000_000;

#[repr(C)]
pub struct StorageResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
}

/// 「[Opus 5.5] An opened store, or why it did not open.」
#[repr(C)]
pub struct OpenResultC {
    pub success: bool,
    pub error_msg: *mut c_char,
    pub store: *mut EtcdStore,
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

impl FfiResult for OpenResultC {
    const ERROR_FALLBACK: &'static str = "error message contains null";
    fn error_fields(error_msg: *mut c_char) -> Self {
        Self {
            success: false,
            error_msg,
            store: ptr::null_mut(),
        }
    }
}

/// 「[Opus 5.5] What an error is when it leaves through the FFI: the sacred
/// shape, as the bytes of its JSON, naming the call it left through.」
trait Crosses {
    fn crosses(self, call: &str) -> String;
}

impl Crosses for EtcdError {
    fn crosses(self, call: &str) -> String {
        self.sacred_json(Some(call))
    }
}

impl Crosses for serde_json::Error {
    fn crosses(self, call: &str) -> String {
        EtcdError::Serde(self).sacred_json(Some(call))
    }
}

/// 「[Opus 5.5] A `Backend` already carries the sacred shape as its string; the
/// trait's own variants are built into one.」
impl Crosses for StoreError {
    fn crosses(self, call: &str) -> String {
        if let StoreError::Backend(json) = &self {
            if serde_json::from_str::<sacred_error::Error>(json).is_ok() {
                return json.clone();
            }
        }
        EtcdError::Store(self).sacred_json(Some(call))
    }
}

/// 「[Opus 5.5] A C string argument as a `&str`, or the typed refusal naming
/// which argument of which call was not one.」
unsafe fn argument<'a>(
    call: &'static str,
    name: &'static str,
    ptr: *const c_char,
) -> Result<&'a str, EtcdError> {
    unsafe { cstr_to_str(ptr) }.map_err(|source| EtcdError::BadArgument {
        call,
        argument: name,
        source,
    })
}

/// 「[Opus 5.5] Connect to the etcd at `endpoint`, for one namespace. The
/// result is freed with `etcd_open_result_free`, which leaves the store.」
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn etcd_storage_new(
    endpoint: *const c_char,
    namespace: *const c_char,
) -> OpenResultC {
    qntx_ffi_common::guarded_result("etcd_storage_new", || {
        const CALL: &str = "etcd_storage_new";
        let args = || -> Result<(&str, &str), EtcdError> {
            Ok((unsafe { argument(CALL, "endpoint", endpoint) }?, unsafe {
                argument(CALL, "namespace", namespace)
            }?))
        };
        let (endpoint, ns) = match args() {
            Ok(a) => a,
            Err(e) => return OpenResultC::error(e.crosses(CALL)),
        };
        match EtcdStore::open(endpoint, ns) {
            Ok(store) => OpenResultC {
                success: true,
                error_msg: ptr::null_mut(),
                store: Box::into_raw(Box::new(store)),
            },
            Err(e) => OpenResultC::error(e.crosses(CALL)),
        }
    })
}

#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn etcd_storage_free(store: *mut EtcdStore) {
    qntx_ffi_common::guarded("etcd_storage_free", || unsafe { free_boxed(store) }, |_| ())
}

/// 「[Opus 5.5] Put one attestation, as the node's JSON for the proto, into the
/// window under a lease of SPAN.」
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn etcd_storage_put(
    store: *mut EtcdStore,
    attestation_json: *const c_char,
) -> StorageResultC {
    qntx_ffi_common::guarded_result("etcd_storage_put", || {
        const CALL: &str = "etcd_storage_put";
        if store.is_null() {
            return StorageResultC::error("null store pointer");
        }
        let json = match unsafe { argument(CALL, "attestation_json", attestation_json) } {
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
            Ok(()) => StorageResultC {
                success: true,
                error_msg: ptr::null_mut(),
            },
            Err(e) => StorageResultC::error(e.crosses(CALL)),
        }
    })
}

#[no_mangle]
pub extern "C" fn etcd_open_result_free(result: OpenResultC) {
    qntx_ffi_common::guarded(
        "etcd_open_result_free",
        || unsafe { free_cstring(result.error_msg) },
        |_| (),
    )
}

#[no_mangle]
pub extern "C" fn etcd_storage_result_free(result: StorageResultC) {
    qntx_ffi_common::guarded(
        "etcd_storage_result_free",
        || unsafe { free_cstring(result.error_msg) },
        |_| (),
    )
}

qntx_ffi_common::define_version_fn!(etcd_storage_version);
