// Library entry point for mobile platforms (iOS/Android)
// Desktop builds use src/main.rs directly

// iOS-specific features (biometrics, permissions, etc.)
#[cfg(target_os = "ios")]
mod ios;

// For mobile platforms, we need to export the app initialization
// Desktop continues to use the main.rs binary

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // Initialize iOS-specific features
    #[cfg(target_os = "ios")]
    ios::init();

    // Build mobile app (no sidecar, no desktop-only features)
    #[cfg(target_os = "ios")]
    {
        tauri::Builder::default()
            .plugin(tauri_plugin_shell::init())
            .plugin(tauri_plugin_notification::init())
            .invoke_handler(tauri::generate_handler![
                ios::ios_authenticate_biometric,
                ios::ios_biometric_available,
                ios::ios_request_permissions,
                ios::ios_device_info,
            ])
            .run(tauri::generate_context!())
            .expect("error while running tauri application");
    }

    #[cfg(not(target_os = "ios"))]
    {
        tauri::Builder::default()
            .plugin(tauri_plugin_shell::init())
            .plugin(tauri_plugin_notification::init())
            .run(tauri::generate_context!())
            .expect("error while running tauri application");
    }
}
