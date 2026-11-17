//! 「[Opus 5.5] C-compatible FFI for the etcd window: what the node needs to put
//! 「[Opus 5.5] each attestation it creates into it. Same result types and ownership rules」
//! 「[Opus 5.5] as ats-postgres's ffi.rs: the store is allocated on the Rust heap and freed」
//! 「[Opus 5.5] with `etcd_storage_free`; strings in result structs belong to the caller and」
//! 「[Opus 5.5] are freed with the matching `*_result_free`.」」

use std::os::raw::c_char;
use std::ptr;

use ats::storage::{AttestationStore, StoreError};
use qntx_ffi_common::{cstr_to_str, free_boxed, free_cstring, FfiResult};

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
/// 「[Opus 5.5] shape, as the bytes of its JSON, naming the call it left through.」」
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
/// 「[Opus 5.5] trait's own variants are built into one.」」
impl Crosses for StoreError {
    fn crosses(self, call: &str) -> String {
        // 「[Opus 5.5] A Backend error was made inside the crate with no call; it names
        // the one it leaves through here (CodeRabbit on #1765).」
        if let StoreError::Backend(json) = &self {
            if let Ok(mut sacred) = serde_json::from_str::<sacred_error::Error>(json) {
                sacred.ffi_call = Some(call.to_string());
                return serde_json::to_string(&sacred).unwrap_or_else(|_| json.clone());
            }
        }
        EtcdError::Store(self).sacred_json(Some(call))
    }
}

/// 「[Opus 5.5] A C string argument as a `&str`, or the typed refusal naming
/// 「[Opus 5.5] which argument of which call was not one.」」
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
/// 「[Opus 5.5] result is freed with `etcd_open_result_free`, which leaves the store.」」
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
/// 「[Opus 5.5] window under a lease of SPAN.」」
#[no_mangle]
#[allow(clippy::not_unsafe_ptr_arg_deref)]
pub extern "C" fn etcd_storage_put(
    store: *mut EtcdStore,
    attestation_json: *const c_char,
) -> StorageResultC {
    qntx_ffi_common::guarded_result("etcd_storage_put", || {
        const CALL: &str = "etcd_storage_put";
        if store.is_null() {
            return StorageResultC::error(
                EtcdError::NullPointer {
                    call: CALL,
                    argument: "store",
                }
                .crosses(CALL),
            );
        }
        let json = match unsafe { argument(CALL, "attestation_json", attestation_json) } {
            Ok(s) => s,
            Err(e) => return StorageResultC::error(e.crosses(CALL)),
        };
        if json.len() > MAX_JSON_LENGTH {
            return StorageResultC::error(
                EtcdError::TooLong {
                    call: CALL,
                    argument: "attestation_json",
                    length: json.len(),
                    most: MAX_JSON_LENGTH,
                }
                .crosses(CALL),
            );
        }
        let attestation = match crate::attestation_of_node_json(json) {
            Ok(a) => a,
            Err(e) => return StorageResultC::error(e.crosses(CALL)),
        };
        let store = unsafe { &mut *store };
        match store.put(attestation) {
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

#[cfg(test)]
#[allow(clippy::unwrap_used, clippy::expect_used, clippy::panic)]
mod tests {
    use super::*;
    use std::ffi::CStr;

    fn sacred_of(msg: *mut c_char) -> sacred_error::Error {
        let text = unsafe { CStr::from_ptr(msg) }.to_str().unwrap();
        serde_json::from_str(text).unwrap_or_else(|e| panic!("{text:?} is not a sacred error: {e}"))
    }

    /// 「[Opus 5.5] An error etcd gave names the call it left through: Backend carried it
    /// serialized with no call, and it crossed so (CodeRabbit on #1765).」
    #[test]
    fn a_backend_error_names_the_call_it_left_through() {
        let backend = StoreError::Backend(
            EtcdError::BadNamespace {
                value: "Clean".to_string(),
            }
            .sacred_json(None),
        );
        let crossed: sacred_error::Error =
            serde_json::from_str(&backend.crosses("etcd_storage_put")).unwrap();
        assert_eq!(crossed.ffi_call.as_deref(), Some("etcd_storage_put"));
    }

    /// 「[Opus 5.5] A null store is a sacred error naming its call, not a sentence
    /// (CodeRabbit on #1765).」
    #[test]
    fn a_null_store_is_said_as_a_sacred_error() {
        let result = etcd_storage_put(ptr::null_mut(), c"{}".as_ptr());
        assert!(!result.success);
        let said = sacred_of(result.error_msg);
        assert_eq!(said.ffi_call.as_deref(), Some("etcd_storage_put"));
        etcd_storage_result_free(result);
    }

    /// 「[Opus 5.5] JSON past its length is a sacred error naming its call, not a sentence
    /// (CodeRabbit on #1765). Run under scripts/with-etcd.sh: the store is a real one.」
    #[test]
    fn json_past_its_length_is_said_as_a_sacred_error() {
        let endpoint = std::env::var("QNTX_ETCD_ENDPOINT")
            .expect("QNTX_ETCD_ENDPOINT names no etcd; run under scripts/with-etcd.sh");
        let mut store = EtcdStore::open(&endpoint, "ffi_length").unwrap();
        let long = std::ffi::CString::new("x".repeat(MAX_JSON_LENGTH + 1)).unwrap();
        let result = etcd_storage_put(&mut store, long.as_ptr());
        assert!(!result.success);
        let said = sacred_of(result.error_msg);
        assert_eq!(said.ffi_call.as_deref(), Some("etcd_storage_put"));
        etcd_storage_result_free(result);
    }
}
