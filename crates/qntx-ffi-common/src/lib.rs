//! Common FFI utilities for QNTX C-compatible interfaces.
//!
//! This crate provides shared utilities for building C-compatible FFI layers,
//! reducing boilerplate across `qntx-sqlite`, `fuzzy-ax`, and `vidstream`.
//!
//! # Memory Ownership
//!
//! All functions follow Rust's ownership model for FFI:
//! - Functions returning `*mut c_char` transfer ownership to the caller
//! - Callers must use corresponding `free_*` functions to deallocate
//! - NULL pointers are handled safely (no-op for free functions)

use std::ffi::{CStr, CString};
use std::os::raw::c_char;
use std::ptr;
use std::slice;

/// Convert a Rust string to a C string pointer, with a fallback on failure.
///
/// If the input contains null bytes, returns the fallback string instead.
/// The returned pointer is owned by the caller and must be freed.
///
/// # Arguments
/// * `s` - The string to convert
/// * `fallback` - Fallback message if `s` contains null bytes
///
/// # Example
/// ```
/// use qntx_ffi_common::cstring_new_or_fallback;
///
/// let ptr = cstring_new_or_fallback("hello", "error");
/// // ptr is now owned by caller, must free with free_cstring
/// ```
#[inline]
pub fn cstring_new_or_fallback(s: &str, fallback: &'static str) -> *mut c_char {
    CString::new(s)
        .unwrap_or_else(|_| CString::new(fallback).expect("fallback must be valid"))
        .into_raw()
}

/// Convert a Rust string to a C string pointer, using empty string as fallback.
///
/// Convenience wrapper around `cstring_new_or_fallback` with empty fallback.
#[inline]
pub fn cstring_new_or_empty(s: &str) -> *mut c_char {
    CString::new(s).unwrap_or_default().into_raw()
}

/// Safely free a C string pointer.
///
/// Does nothing if the pointer is null.
///
/// # Safety
/// The pointer must have been allocated by `CString::into_raw()` or be null.
#[inline]
pub unsafe fn free_cstring(ptr: *mut c_char) {
    if !ptr.is_null() {
        unsafe {
            let _ = CString::from_raw(ptr);
        }
    }
}

/// Safely free a boxed value.
///
/// Does nothing if the pointer is null.
///
/// # Safety
/// The pointer must have been allocated by `Box::into_raw()` or be null.
#[inline]
pub unsafe fn free_boxed<T>(ptr: *mut T) {
    if !ptr.is_null() {
        unsafe {
            let _ = Box::from_raw(ptr);
        }
    }
}

/// Free a boxed slice and its contents.
///
/// Does nothing if the pointer is null or length is zero.
///
/// # Safety
/// The pointer must have been allocated by `Box::into_raw(slice.into_boxed_slice())`.
#[inline]
pub unsafe fn free_boxed_slice<T>(ptr: *mut T, len: usize) {
    if !ptr.is_null() && len > 0 {
        unsafe {
            let _ = Box::from_raw(ptr::slice_from_raw_parts_mut(ptr, len));
        }
    }
}

/// Convert a boxed slice to a raw pointer and length.
///
/// Returns null pointer and 0 length for empty vectors.
/// The returned pointer is owned by the caller.
#[inline]
pub fn vec_into_raw<T>(vec: Vec<T>) -> (*mut T, usize) {
    let len = vec.len();
    if len == 0 {
        (ptr::null_mut(), 0)
    } else {
        (Box::into_raw(vec.into_boxed_slice()) as *mut T, len)
    }
}

/// Convert a C string array to a Vec<String>.
///
/// # Arguments
/// * `arr` - Pointer to array of C string pointers
/// * `len` - Number of strings in the array
///
/// # Returns
/// `Ok(Vec<String>)` on success, `Err(String)` with error message on failure.
///
/// # Safety
/// - `arr` must point to `len` valid C string pointers, or be null (if len is 0)
/// - Each string pointer must be valid and null-terminated
pub unsafe fn convert_string_array(
    arr: *const *const c_char,
    len: usize,
) -> Result<Vec<String>, String> {
    if arr.is_null() || len == 0 {
        return Ok(Vec::new());
    }

    let slice = unsafe { slice::from_raw_parts(arr, len) };
    let mut result = Vec::with_capacity(len);

    for (i, &ptr) in slice.iter().enumerate() {
        if ptr.is_null() {
            return Err(format!("null string at index {}", i));
        }
        match unsafe { CStr::from_ptr(ptr) }.to_str() {
            Ok(s) => result.push(s.to_string()),
            Err(_) => return Err(format!("invalid UTF-8 at index {}", i)),
        }
    }

    Ok(result)
}

/// Safely convert a C string pointer to a Rust string reference.
///
/// # Arguments
/// * `ptr` - Pointer to null-terminated C string
///
/// # Returns
/// `Ok(&str)` on success, `Err(&'static str)` with error message on failure.
///
/// # Safety
/// The pointer must be valid and null-terminated, or null.
pub unsafe fn cstr_to_str<'a>(ptr: *const c_char) -> Result<&'a str, &'static str> {
    if ptr.is_null() {
        return Err("null pointer");
    }
    unsafe { CStr::from_ptr(ptr) }
        .to_str()
        .map_err(|_| "invalid UTF-8")
}

/// Safely convert a C string pointer to a Rust String (owned).
///
/// # Arguments
/// * `ptr` - Pointer to null-terminated C string
///
/// # Returns
/// `Ok(String)` on success, `Err(&'static str)` with error message on failure.
///
/// # Safety
/// The pointer must be valid and null-terminated, or null.
pub unsafe fn cstr_to_string(ptr: *const c_char) -> Result<String, &'static str> {
    cstr_to_str(ptr).map(|s| s.to_string())
}

/// Free an array of C strings and the array itself.
///
/// Frees each non-null string in the array, then frees the array.
///
/// # Safety
/// - `arr` must have been allocated by `Box::into_raw(vec.into_boxed_slice())`
/// - Each string must have been allocated by `CString::into_raw()`
pub unsafe fn free_cstring_array(arr: *mut *mut c_char, len: usize) {
    if arr.is_null() || len == 0 {
        return;
    }

    let slice = unsafe { slice::from_raw_parts_mut(arr, len) };
    for s in slice.iter() {
        free_cstring(*s);
    }
    free_boxed_slice(arr, len);
}

/// Trait for FFI result types with standardized error handling.
///
/// Types implementing this trait get a consistent `.error()` method
/// that converts error messages to C strings with fallback handling.
///
/// # Example
/// ```ignore
/// #[repr(C)]
/// pub struct MyResultC {
///     pub success: bool,
///     pub error_msg: *mut c_char,
///     pub data: *mut MyData,
/// }
///
/// impl FfiResult for MyResultC {
///     const ERROR_FALLBACK: &'static str = "unknown error";
///
///     fn error_fields(error_msg: *mut c_char) -> Self {
///         Self {
///             success: false,
///             error_msg,
///             data: ptr::null_mut(),
///         }
///     }
/// }
///
/// // Now you can use:
/// let result = MyResultC::error("operation failed");
/// ```
pub trait FfiResult: Sized {
    /// Fallback message used when the error message contains null bytes.
    const ERROR_FALLBACK: &'static str;

    /// Construct the result struct with the given error message pointer.
    ///
    /// This method receives the already-allocated error_msg pointer
    /// and should construct the full result struct with error state.
    fn error_fields(error_msg: *mut c_char) -> Self;

    /// Create an error result with the given message.
    ///
    /// Converts the message to a C string using `cstring_new_or_fallback`
    /// with `ERROR_FALLBACK`, then calls `error_fields` to construct the result.
    #[inline]
    fn error(msg: &str) -> Self {
        let error_msg = cstring_new_or_fallback(msg, Self::ERROR_FALLBACK);
        Self::error_fields(error_msg)
    }
}

/// Generate a version function that returns a static C string.
///
/// # Example
/// ```ignore
/// qntx_ffi_common::define_version_fn!(my_lib_version);
/// // Expands to:
/// // #[no_mangle]
/// // pub extern "C" fn my_lib_version() -> *const c_char {
/// //     concat!(env!("CARGO_PKG_VERSION"), "\0").as_ptr() as *const c_char
/// // }
/// ```
#[macro_export]
macro_rules! define_version_fn {
    ($fn_name:ident) => {
        #[no_mangle]
        pub extern "C" fn $fn_name() -> *const std::os::raw::c_char {
            concat!(env!("CARGO_PKG_VERSION"), "\0").as_ptr() as *const std::os::raw::c_char
        }
    };
}

/// Generate an engine free function for a given type.
///
/// # Example
/// ```ignore
/// qntx_ffi_common::define_engine_free!(my_engine_free, MyEngine);
/// // Expands to:
/// // #[no_mangle]
/// // #[allow(clippy::not_unsafe_ptr_arg_deref)]
/// // pub extern "C" fn my_engine_free(ptr: *mut MyEngine) {
/// //     qntx_ffi_common::free_boxed(ptr);
/// // }
/// ```
#[macro_export]
macro_rules! define_engine_free {
    ($fn_name:ident, $engine_type:ty) => {
        #[no_mangle]
        #[allow(clippy::not_unsafe_ptr_arg_deref)]
        pub extern "C" fn $fn_name(ptr: *mut $engine_type) {
            unsafe { $crate::free_boxed(ptr) };
        }
    };
}

/// Generate a string free function.
///
/// # Example
/// ```ignore
/// qntx_ffi_common::define_string_free!(my_string_free);
/// // Expands to:
/// // #[no_mangle]
/// // #[allow(clippy::not_unsafe_ptr_arg_deref)]
/// // pub extern "C" fn my_string_free(s: *mut c_char) {
/// //     qntx_ffi_common::free_cstring(s);
/// // }
/// ```
#[macro_export]
macro_rules! define_string_free {
    ($fn_name:ident) => {
        #[no_mangle]
        #[allow(clippy::not_unsafe_ptr_arg_deref)]
        pub extern "C" fn $fn_name(s: *mut std::os::raw::c_char) {
            unsafe { $crate::free_cstring(s) };
        }
    };
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_cstring_new_or_fallback() {
        let ptr = cstring_new_or_fallback("hello", "fallback");
        assert!(!ptr.is_null());
        let s = unsafe { CStr::from_ptr(ptr) }.to_str().unwrap();
        assert_eq!(s, "hello");
        unsafe { free_cstring(ptr) };
    }

    #[test]
    fn test_cstring_with_null_bytes_uses_fallback() {
        let ptr = cstring_new_or_fallback("hel\0lo", "fallback");
        assert!(!ptr.is_null());
        let s = unsafe { CStr::from_ptr(ptr) }.to_str().unwrap();
        assert_eq!(s, "fallback");
        unsafe { free_cstring(ptr) };
    }

    #[test]
    fn test_free_cstring_null_is_safe() {
        unsafe { free_cstring(ptr::null_mut()) };
    }

    #[test]
    fn test_free_boxed_null_is_safe() {
        unsafe { free_boxed::<i32>(ptr::null_mut()) };
    }

    #[test]
    fn test_vec_into_raw_empty() {
        let (ptr, len): (*mut i32, usize) = vec_into_raw(Vec::new());
        assert!(ptr.is_null());
        assert_eq!(len, 0);
    }

    #[test]
    fn test_vec_into_raw_non_empty() {
        let (ptr, len) = vec_into_raw(vec![1i32, 2, 3]);
        assert!(!ptr.is_null());
        assert_eq!(len, 3);
        unsafe { free_boxed_slice(ptr, len) };
    }

    #[test]
    fn test_convert_string_array_empty() {
        let result = unsafe { convert_string_array(ptr::null(), 0) };
        assert!(result.is_ok());
        assert!(result.unwrap().is_empty());
    }

    #[test]
    fn test_convert_string_array_valid() {
        let strings = [
            CString::new("hello").unwrap(),
            CString::new("world").unwrap(),
        ];
        let ptrs: Vec<*const c_char> = strings.iter().map(|s| s.as_ptr()).collect();

        let result = unsafe { convert_string_array(ptrs.as_ptr(), ptrs.len()) };
        assert!(result.is_ok());
        let vec = result.unwrap();
        assert_eq!(vec, vec!["hello", "world"]);
    }

    #[test]
    fn test_cstr_to_str_null() {
        let result = unsafe { cstr_to_str(ptr::null()) };
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "null pointer");
    }

    #[test]
    fn test_cstr_to_str_valid() {
        let s = CString::new("test").unwrap();
        let result = unsafe { cstr_to_str(s.as_ptr()) };
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), "test");
    }

    #[test]
    fn test_cstr_to_string_returns_owned() {
        let s = CString::new("owned").unwrap();
        let result = unsafe { cstr_to_string(s.as_ptr()) };
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), "owned".to_string());
    }

    #[test]
    fn test_free_cstring_array_valid() {
        // Create array of CStrings via the same path as production code
        let strings: Vec<*mut c_char> = vec!["one", "two", "three"]
            .into_iter()
            .map(|s| cstring_new_or_empty(s))
            .collect();
        let (ptr, len) = vec_into_raw(strings);

        // Should not crash/leak - this exercises the full cleanup path
        unsafe { free_cstring_array(ptr, len) };
    }
}
