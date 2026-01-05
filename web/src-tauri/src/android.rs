//! Android-specific Tauri features
//!
//! This module contains placeholder implementations for Android-specific functionality.
//! These commands are currently registered but return errors when called.
//!
//! **Status**: Stub implementations for future development
//!
//! **Planned features**:
//! - Biometric authentication (fingerprint / face unlock)
//! - Android permission management (notifications, storage, etc.)
//! - Device info detection (model, Android version, notch/punch-hole)
//!
//! Only compiled when building for Android (`#[cfg(target_os = "android")]`).

/// Placeholder for Android biometric authentication
///
/// **Current behavior**: Always returns error
///
/// **Future**: Integrate with tauri-plugin-biometric or native Android APIs for fingerprint/face unlock
#[tauri::command]
pub fn android_authenticate_biometric() -> Result<bool, String> {
    println!("[android] Biometric authentication requested (not implemented)");
    Err("Biometric authentication not yet implemented".to_string())
}

/// Check if biometric authentication is available on this device
///
/// **Current behavior**: Always returns false
///
/// **Future**: Detect if device has fingerprint sensor or face unlock available
#[tauri::command]
pub fn android_biometric_available() -> bool {
    println!("[android] Checking biometric availability (not implemented)");
    false
}

/// Request Android-specific permissions (e.g., notifications, storage, camera)
///
/// **Current behavior**: Always returns error
///
/// **Future**: Implement Android permission handling via native APIs
#[tauri::command]
pub fn android_request_permissions(permission: String) -> Result<bool, String> {
    println!("[android] Permission request for: {}", permission);
    Err(format!(
        "Permission '{}' handling not implemented",
        permission
    ))
}

/// Get Android-specific device info
#[tauri::command]
pub fn android_device_info() -> serde_json::Value {
    serde_json::json!({
        "platform": "android",
        "model": "unknown", // TODO: Get actual device model
        "os_version": "unknown", // TODO: Get Android version
        "sdk_version": 0, // TODO: Get SDK/API level
        "has_notch": false, // TODO: Detect display cutout
    })
}

/// Initialize Android-specific features
pub fn init() {
    println!("[android] Android-specific features initialized");
    // Future initialization:
    // - Setup biometric handlers
    // - Configure Android-specific plugins
    // - Register for system events (battery, connectivity, etc.)
}
