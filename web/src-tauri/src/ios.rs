//! iOS-specific Tauri features
//!
//! This module contains placeholder implementations for iOS-specific functionality.
//! These commands are currently registered but return errors when called.
//!
//! **Status**: Stub implementations for future development
//!
//! **Planned features**:
//! - Biometric authentication (Face ID / Touch ID)
//! - iOS permission management (notifications, location, etc.)
//! - Device info detection (model, iOS version, notch/Dynamic Island)
//!
//! Only compiled when building for iOS (`#[cfg(target_os = "ios")]`).

/// Placeholder for iOS biometric authentication
///
/// **Current behavior**: Always returns error
///
/// **Future**: Integrate with tauri-plugin-biometric or native iOS APIs for Face ID / Touch ID
#[tauri::command]
pub fn ios_authenticate_biometric() -> Result<bool, String> {
    println!("[ios] Biometric authentication requested (not implemented)");
    Err("Biometric authentication not yet implemented".to_string())
}

/// Check if biometric authentication is available on this device
///
/// **Current behavior**: Always returns false
///
/// **Future**: Detect if device has Face ID or Touch ID available
#[tauri::command]
pub fn ios_biometric_available() -> bool {
    println!("[ios] Checking biometric availability (not implemented)");
    false
}

/// Request iOS-specific permissions (e.g., notifications, location)
///
/// **Current behavior**: Always returns error
///
/// **Future**: Implement iOS permission handling via native APIs
#[tauri::command]
pub fn ios_request_permissions(permission: String) -> Result<bool, String> {
    println!("[ios] Permission request for: {}", permission);
    Err(format!(
        "Permission '{}' handling not implemented",
        permission
    ))
}

/// Get iOS-specific device info
#[tauri::command]
pub fn ios_device_info() -> serde_json::Value {
    serde_json::json!({
        "platform": "ios",
        "model": "simulator", // TODO: Get actual device model
        "os_version": "unknown", // TODO: Get iOS version
        "has_notch": false, // TODO: Detect notch/Dynamic Island
    })
}

/// Initialize iOS-specific features
pub fn init() {
    println!("[ios] iOS-specific features initialized");
    // Future initialization:
    // - Setup biometric handlers
    // - Configure iOS-specific plugins
    // - Register for system events
}
