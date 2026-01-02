// iOS-specific Tauri features
// Only compiled when building for iOS

/// Placeholder for iOS biometric authentication
/// Future: Integrate with Tauri plugins for Face ID / Touch ID
#[tauri::command]
pub async fn ios_authenticate_biometric() -> Result<bool, String> {
    // TODO: Implement using tauri-plugin-biometric or native iOS APIs
    // For now, return placeholder response
    println!("[ios] Biometric authentication requested (not implemented)");
    Err("Biometric authentication not yet implemented".to_string())
}

/// Check if biometric authentication is available on this device
#[tauri::command]
pub fn ios_biometric_available() -> bool {
    // TODO: Check if device has Face ID or Touch ID
    // For now, return false
    println!("[ios] Checking biometric availability (not implemented)");
    false
}

/// Request iOS-specific permissions (e.g., notifications, location)
#[tauri::command]
pub async fn ios_request_permissions(permission: String) -> Result<bool, String> {
    println!("[ios] Permission request for: {}", permission);
    // TODO: Implement iOS permission handling
    Err(format!("Permission '{}' handling not implemented", permission))
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
