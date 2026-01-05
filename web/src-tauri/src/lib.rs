// Library entry point for mobile platforms (iOS/Android)
// Desktop builds use src/main.rs directly

// iOS-specific features (biometrics, permissions, etc.)
#[cfg(target_os = "ios")]
mod ios;

// Android-specific features (biometrics, permissions, etc.)
#[cfg(target_os = "android")]
mod android;

// For mobile platforms, we need to export the app initialization
// Desktop continues to use the main.rs binary

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // Initialize platform-specific features
    #[cfg(target_os = "ios")]
    ios::init();

    #[cfg(target_os = "android")]
    android::init();

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

    #[cfg(target_os = "android")]
    {
        // NOTE: tauri_plugin_notification requires POST_NOTIFICATIONS permission on Android 13+
        // Ensure android_request_permissions is called before sending notifications
        tauri::Builder::default()
            .plugin(tauri_plugin_shell::init())
            .plugin(tauri_plugin_notification::init())
            .invoke_handler(tauri::generate_handler![
                android::android_authenticate_biometric,
                android::android_biometric_available,
                android::android_request_permissions,
                android::android_device_info,
            ])
            .run(tauri::generate_context!())
            .expect("error while running tauri application");
    }

    // Fallback for other platforms (shouldn't be reached in practice)
    #[cfg(not(any(target_os = "ios", target_os = "android")))]
    {
        tauri::Builder::default()
            .plugin(tauri_plugin_shell::init())
            .plugin(tauri_plugin_notification::init())
            .run(tauri::generate_context!())
            .expect("error while running tauri application");
    }
}
