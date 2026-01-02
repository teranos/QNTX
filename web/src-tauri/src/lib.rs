// Library entry point for mobile platforms (iOS/Android)
// Desktop builds use src/main.rs directly

// For mobile platforms, we need to export the app initialization
// Desktop continues to use the main.rs binary

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    // Use the same builder from main, but wrapped for mobile
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_notification::init())
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
