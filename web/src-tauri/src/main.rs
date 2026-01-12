// Prevents additional console window on Windows in release builds
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use log::info;
use qntx::error::Error;
use qntx::types::sym;
use std::sync::{Arc, Mutex};
use tauri::{Emitter, Manager, State};
use tauri_plugin_notification::NotificationExt;

// Desktop-only features (menu bar, tray, autostart, deep-link)
#[cfg(not(target_os = "ios"))]
use tauri::menu::{CheckMenuItem, Menu, MenuItem, PredefinedMenuItem, Submenu};
#[cfg(not(target_os = "ios"))]
use tauri::tray::{TrayIconBuilder, TrayIconEvent};
#[cfg(not(target_os = "ios"))]
use tauri_plugin_autostart::{MacosLauncher, ManagerExt};
#[cfg(not(target_os = "ios"))]
use tauri_plugin_deep_link::DeepLinkExt;

// Import generated types from Go source (single source of truth)
// These types are kept in sync with the backend via `make types`
#[allow(unused_imports)]
use qntx::types::{
    async_types::{Job, JobStatus},
    server::{DaemonStatusMessage, JobUpdateMessage, StorageWarningMessage},
};

// Video processing module (desktop-only)
// #[cfg(not(any(target_os = "ios", target_os = "android")))]
// mod vidstream;  // Disabled: blocked by CrabCamera ACL (PR #267)

const SERVER_PORT: &str = "877";

struct ServerState {
    child: Arc<Mutex<Option<std::process::Child>>>,
    port: String,
}

/// Helper function to send notifications with proper error propagation
fn send_notification(
    app: &tauri::AppHandle,
    title: &str,
    body: String,
    log_message: String,
) -> Result<(), Error> {
    app.notification()
        .builder()
        .title(title)
        .body(body)
        .show()
        .map_err(|e| Error::context("failed to show notification", e))?;
    info!("[notification] {}", log_message);
    Ok(())
}

/// Helper to show window, focus it, and emit an event with proper error propagation
fn show_window_and_emit<P: serde::Serialize + Clone>(
    app: &tauri::AppHandle,
    event_name: &str,
    payload: P,
) -> Result<(), Error> {
    let window = app
        .get_webview_window("main")
        .ok_or_else(|| Error::internal("main window not found"))?;

    window
        .show()
        .map_err(|e| Error::context("failed to show window", e))?;
    window
        .set_focus()
        .map_err(|e| Error::context("failed to focus window", e))?;
    window
        .emit(event_name, payload)
        .map_err(|e| Error::context(format!("failed to emit '{}' event", event_name), e))?;
    Ok(())
}

/// Handle deep link URLs (qntx://...)
/// Routes to appropriate panels or resources based on URL path
#[cfg(not(target_os = "ios"))]
fn handle_deep_link(app: &tauri::AppHandle, url: &str) {
    info!("[deep-link] Received: {}", url);

    // Parse the URL to extract the path
    // Format: qntx://path or qntx://path/id
    let path = url
        .strip_prefix("qntx://")
        .unwrap_or(url)
        .trim_end_matches('/');

    // Route based on path - log errors but don't fail the entire handler
    let result = match path {
        "pulse" => show_window_and_emit(app, "show-pulse-panel", ()),
        "config" | "settings" | "preferences" => show_window_and_emit(app, "show-config-panel", ()),
        "docs" | "prose" | "documentation" => show_window_and_emit(app, "show-prose-panel", ()),
        "code" | "editor" => show_window_and_emit(app, "show-code-panel", ()),
        "hixtory" | "history" => show_window_and_emit(app, "show-hixtory-panel", ()),
        _ if path.starts_with("job/") => {
            // qntx://job/<id> - emit event with job ID
            let job_id = path.strip_prefix("job/").unwrap_or("");
            if !job_id.is_empty() {
                show_window_and_emit(app, "deep-link-job", job_id.to_string())
            } else {
                show_window_and_emit(app, "show-hixtory-panel", ())
            }
        }
        _ => {
            // Unknown path - just show window
            info!("[deep-link] Unknown path: {}", path);
            show_window_and_emit(app, "show-pulse-panel", ())
        }
    };

    if let Err(e) = result {
        log::warn!("[deep-link] Failed to handle deep link: {}", e);
    }
}

#[tauri::command]
fn get_server_status(state: State<ServerState>) -> serde_json::Value {
    let child = state.child.lock().unwrap();
    let is_running = child.is_some();

    serde_json::json!({
        "status": if is_running { "running" } else { "stopped" },
        "url": if is_running {
            Some(format!("http://localhost:{}", state.port))
        } else {
            None
        }
    })
}

#[tauri::command]
fn get_server_url(state: State<ServerState>) -> Option<String> {
    let child = state.child.lock().unwrap();
    if child.is_some() {
        Some(format!("http://localhost:{}", state.port))
    } else {
        None
    }
}

/// Send a native notification for job completion
#[tauri::command]
fn notify_job_completed(
    app: tauri::AppHandle,
    handler_name: String,
    job_id: String,
    duration_ms: Option<i64>,
) -> Result<(), Error> {
    let duration_text = duration_ms
        .map(|ms| format!(" in {:.1}s", ms as f64 / 1000.0))
        .unwrap_or_default();

    send_notification(
        &app,
        "QNTX: Job Completed",
        format!("{}{}", handler_name, duration_text),
        format!("Job completed: {} ({})", handler_name, job_id),
    )
}

/// Send a native notification for job failure
#[tauri::command]
fn notify_job_failed(
    app: tauri::AppHandle,
    handler_name: String,
    job_id: String,
    error: Option<String>,
) -> Result<(), Error> {
    let error_text = error.unwrap_or_else(|| "Unknown error".to_string());

    send_notification(
        &app,
        "QNTX: Job Failed",
        format!("{}: {}", handler_name, error_text),
        format!("Job failed: {} ({}) - {}", handler_name, job_id, error_text),
    )
}

/// Send a native notification for storage warnings
#[tauri::command]
fn notify_storage_warning(
    app: tauri::AppHandle,
    actor: String,
    fill_percent: f64,
) -> Result<(), Error> {
    let percent = fill_percent * 100.0;
    send_notification(
        &app,
        "QNTX: Storage Warning",
        format!("{} is at {:.0}% capacity", actor, percent),
        format!("Storage warning: {} at {:.0}%", actor, percent),
    )
}

/// Send a native notification when server enters draining mode
/// This happens during graceful shutdown when completing remaining jobs
#[tauri::command]
fn notify_server_draining(
    app: tauri::AppHandle,
    active_jobs: i64,
    queued_jobs: i64,
) -> Result<(), Error> {
    let total = active_jobs + queued_jobs;
    let body = if total > 0 {
        format!("Completing {} remaining job(s) before shutdown", total)
    } else {
        "Preparing for shutdown...".to_string()
    };

    send_notification(
        &app,
        "QNTX: Server Draining",
        body,
        format!(
            "Server draining: {} active, {} queued",
            active_jobs, queued_jobs
        ),
    )
}

/// Send a native notification when server stops
#[tauri::command]
fn notify_server_stopped(app: tauri::AppHandle) -> Result<(), Error> {
    send_notification(
        &app,
        "QNTX: Server Stopped",
        "The QNTX server has stopped".to_string(),
        "Server stopped".to_string(),
    )
}

/// Set taskbar progress indicator (Windows feature, works on other platforms too)
/// - state: "none" | "normal" | "indeterminate" | "paused" | "error"
/// - progress: Optional percentage (0-100), only used for "normal" state
#[tauri::command]
fn set_taskbar_progress(
    app: tauri::AppHandle,
    state: String,
    progress: Option<u64>,
) -> Result<(), Error> {
    use tauri::window::ProgressBarState;
    use tauri::window::ProgressBarStatus;

    let window = app
        .get_webview_window("main")
        .ok_or_else(|| Error::internal("main window not found"))?;

    let status = match state.as_str() {
        "normal" => ProgressBarStatus::Normal,
        "indeterminate" => ProgressBarStatus::Indeterminate,
        "paused" => ProgressBarStatus::Paused,
        "error" => ProgressBarStatus::Error,
        _ => ProgressBarStatus::None,
    };

    let progress_state = ProgressBarState {
        status: Some(status),
        progress,
    };

    window
        .set_progress_bar(progress_state)
        .map_err(|e| Error::context("failed to set taskbar progress", e))
}

fn main() {
    let mut builder = tauri::Builder::default()
        .plugin(
            tauri_plugin_log::Builder::new()
                .targets([
                    tauri_plugin_log::Target::new(tauri_plugin_log::TargetKind::Stdout),
                    tauri_plugin_log::Target::new(tauri_plugin_log::TargetKind::Webview),
                ])
                .build(),
        )
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_single_instance::init(|app, args, _cwd| {
            // Show and focus the existing window
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.show();
                let _ = window.set_focus();
            }

            // On Windows/Linux, deep links are passed as CLI arguments
            // Check if any argument is a qntx:// URL
            #[cfg(not(target_os = "ios"))]
            for arg in args.iter() {
                if arg.starts_with("qntx://") {
                    handle_deep_link(app, arg);
                    break;
                }
            }
        }));

    // Desktop-only plugins
    #[cfg(not(target_os = "ios"))]
    {
        info!("Loading desktop plugins...");

        info!("  ✓ autostart");
        builder = builder.plugin(tauri_plugin_autostart::init(
            MacosLauncher::LaunchAgent,
            Some(vec!["--minimized"]),
        ));

        info!("  ✓ deep-link");
        builder = builder.plugin(tauri_plugin_deep_link::init());

        info!("Desktop plugins loaded");
    }

    builder
        .setup(|app| {
            #[cfg(not(target_os = "ios"))]
            {
                // Desktop only: Start QNTX server
                // Note: Using std::process::Command directly instead of Tauri's sidecar system
                // because Tauri v2's sidecar has output streaming issues in dev mode
                use std::process::{Command, Stdio};

                // Set working directory to project root (two levels up from src-tauri)
                let project_root = std::env::current_dir()
                    .ok()
                    .and_then(|d| d.parent().and_then(|p| p.parent()).map(|p| p.to_path_buf()));

                // Construct platform-specific binary name (e.g., qntx-aarch64-apple-darwin)
                let arch = std::env::consts::ARCH;
                let target_triple = match std::env::consts::OS {
                    "macos" => format!("{}-apple-darwin", arch),
                    "linux" => format!("{}-unknown-linux-gnu", arch),
                    "windows" => format!("{}-pc-windows-msvc", arch),
                    os => format!("{}-unknown-{}", arch, os), // Fallback
                };
                let binary_name = format!("qntx-{}", target_triple);
                let binary_path = if let Some(root) = &project_root {
                    root.join("web/src-tauri/bin").join(&binary_name)
                } else {
                    std::path::PathBuf::from("./bin").join(&binary_name)
                };

                // Determine working directory: prefer project root, fallback to current dir or "."
                let working_dir = project_root
                    .as_ref()
                    .cloned()
                    .or_else(|| std::env::current_dir().ok())
                    .unwrap_or_else(|| std::path::PathBuf::from("."));

                let child = match Command::new(&binary_path)
                    .args(["server", "--port", SERVER_PORT, "--dev", "--no-browser"])
                    .current_dir(&working_dir)
                    .stdout(Stdio::inherit())
                    .stderr(Stdio::inherit())
                    .spawn()
                {
                    Ok(child) => child,
                    Err(e) => {
                        eprintln!("[error] Failed to spawn qntx server: {}", e);
                        eprintln!("[error] The QNTX server will not be available.");

                        // Notify user about server failure
                        let _ = app
                            .notification()
                            .builder()
                            .title("QNTX Server Failed")
                            .body(format!(
                                "Failed to start server: {}. Features will not work.",
                                e
                            ))
                            .show();

                        // Store empty server state and continue without server
                        app.manage(ServerState {
                            child: Arc::new(Mutex::new(None)),
                            port: SERVER_PORT.to_string(),
                        });
                        return Ok(());
                    }
                };

                // Store server state (desktop has running sidecar)
                app.manage(ServerState {
                    child: Arc::new(Mutex::new(Some(child))),
                    port: SERVER_PORT.to_string(),
                });

                // Initialize video processing state (desktop only)
                // #[cfg(not(any(target_os = "ios", target_os = "android")))]
                // app.manage(vidstream::VideoEngineState::new());  // Disabled: blocked by CrabCamera ACL (PR #267)

                // Set up deep link handler for macOS (events) and check startup URL
                // On Windows/Linux, deep links come through single-instance CLI args
                let app_handle = app.handle().clone();

                // Check if app was started via deep link
                if let Ok(Some(urls)) = app.deep_link().get_current() {
                    for url in urls {
                        handle_deep_link(&app_handle, url.as_str());
                    }
                }

                // Listen for deep link events (macOS sends these while app is running)
                app.deep_link().on_open_url(move |event| {
                    for url in event.urls() {
                        handle_deep_link(&app_handle, url.as_str());
                    }
                });
            }

            // iOS: No sidecar, store empty server state
            #[cfg(target_os = "ios")]
            {
                app.manage(ServerState {
                    child: Arc::new(Mutex::new(None)),
                    port: SERVER_PORT.to_string(),
                });
            }

            // Desktop only: Create application menu bar
            #[cfg(not(target_os = "ios"))]
            {
                // macOS: Full app menu with Hide/Hide Others/Show All
                #[cfg(target_os = "macos")]
                let app_menu = Submenu::with_items(
                    app,
                    "QNTX",
                    true,
                    &[
                        &MenuItem::with_id(app, "about", "About QNTX", false, None::<&str>)?,
                        &PredefinedMenuItem::separator(app)?,
                        &MenuItem::with_id(
                            app,
                            "preferences",
                            "Preferences...",
                            true,
                            Some("CmdOrCtrl+,"),
                        )?,
                        &PredefinedMenuItem::separator(app)?,
                        &PredefinedMenuItem::hide(app, None)?,
                        &PredefinedMenuItem::hide_others(app, None)?,
                        &PredefinedMenuItem::show_all(app, None)?,
                        &PredefinedMenuItem::separator(app)?,
                        &PredefinedMenuItem::quit(app, None)?,
                    ],
                )?;

                // Windows/Linux: Simplified app menu (no Hide/Hide Others/Show All)
                #[cfg(not(target_os = "macos"))]
                let app_menu = Submenu::with_items(
                    app,
                    "QNTX",
                    true,
                    &[
                        &MenuItem::with_id(app, "about", "About QNTX", false, None::<&str>)?,
                        &PredefinedMenuItem::separator(app)?,
                        &MenuItem::with_id(
                            app,
                            "preferences",
                            "Preferences...",
                            true,
                            Some("CmdOrCtrl+,"),
                        )?,
                        &PredefinedMenuItem::separator(app)?,
                        &PredefinedMenuItem::quit(app, None)?,
                    ],
                )?;

                let edit_menu = Submenu::with_items(
                    app,
                    "Edit",
                    true,
                    &[
                        &MenuItem::with_id(app, "undo", "Undo", true, Some("CmdOrCtrl+Z"))?,
                        &MenuItem::with_id(app, "redo", "Redo", true, Some("CmdOrCtrl+Shift+Z"))?,
                        &PredefinedMenuItem::separator(app)?,
                        &MenuItem::with_id(app, "cut", "Cut", true, Some("CmdOrCtrl+X"))?,
                        &MenuItem::with_id(app, "copy", "Copy", true, Some("CmdOrCtrl+C"))?,
                        &MenuItem::with_id(app, "paste", "Paste", true, Some("CmdOrCtrl+V"))?,
                        &PredefinedMenuItem::separator(app)?,
                        &MenuItem::with_id(
                            app,
                            "select_all",
                            "Select All",
                            true,
                            Some("CmdOrCtrl+A"),
                        )?,
                    ],
                )?;

                let view_menu = Submenu::with_items(
                    app,
                    "View",
                    true,
                    &[
                        &MenuItem::with_id(
                            app,
                            "config_panel",
                            format!("{} Configuration", sym::AM),
                            true,
                            None::<&str>,
                        )?,
                        &MenuItem::with_id(
                            app,
                            "pulse_panel",
                            format!("{} Pulse Scheduler", sym::PULSE),
                            true,
                            Some("CmdOrCtrl+P"),
                        )?,
                        &MenuItem::with_id(
                            app,
                            "prose_panel",
                            format!("{} Documentation", sym::PROSE),
                            true,
                            Some("CmdOrCtrl+/"),
                        )?,
                        &MenuItem::with_id(
                            app,
                            "code_panel",
                            "Code Editor",
                            true,
                            Some("CmdOrCtrl+K"),
                        )?,
                        &MenuItem::with_id(
                            app,
                            "hixtory_panel",
                            format!("{} Hixtory", sym::IX),
                            true,
                            None::<&str>,
                        )?,
                        &PredefinedMenuItem::separator(app)?,
                        &MenuItem::with_id(
                            app,
                            "refresh_graph",
                            "Refresh Graph",
                            true,
                            Some("CmdOrCtrl+R"),
                        )?,
                        &MenuItem::with_id(
                            app,
                            "toggle_logs",
                            "Toggle Logs",
                            true,
                            Some("CmdOrCtrl+J"),
                        )?,
                    ],
                )?;

                let window_menu = Submenu::with_items(
                    app,
                    "Window",
                    true,
                    &[
                        &PredefinedMenuItem::minimize(app, None)?,
                        &PredefinedMenuItem::maximize(app, None)?,
                        &PredefinedMenuItem::separator(app)?,
                        &MenuItem::with_id(
                            app,
                            "bring_all_to_front",
                            "Bring All to Front",
                            true,
                            None::<&str>,
                        )?,
                    ],
                )?;

                let help_menu = Submenu::with_items(
                    app,
                    "Help",
                    true,
                    &[
                        &MenuItem::with_id(
                            app,
                            "documentation",
                            "Documentation",
                            true,
                            None::<&str>,
                        )?,
                        &MenuItem::with_id(app, "github", "View on GitHub", true, None::<&str>)?,
                    ],
                )?;

                let menu_bar = Menu::with_items(
                    app,
                    &[&app_menu, &edit_menu, &view_menu, &window_menu, &help_menu],
                )?;

                app.set_menu(menu_bar)?;

                // Handle menu bar events
                app.on_menu_event(|app_handle, event| {
                    match event.id.as_ref() {
                        // Edit menu - these are handled by the webview
                        "undo" | "redo" | "cut" | "copy" | "paste" | "select_all" => {
                            // Let webview handle standard edit commands
                            if let Some(window) = app_handle.get_webview_window("main") {
                                let _ = window.emit("menu-edit", event.id.as_ref());
                            }
                        }
                        // View menu - emit events to show panels
                        "config_panel" | "preferences" => {
                            if let Err(e) =
                                show_window_and_emit(app_handle, "show-config-panel", ())
                            {
                                log::warn!("[menu] Failed to show config panel: {}", e);
                            }
                        }
                        "pulse_panel" => {
                            if let Err(e) = show_window_and_emit(app_handle, "show-pulse-panel", ())
                            {
                                log::warn!("[menu] Failed to show pulse panel: {}", e);
                            }
                        }
                        "prose_panel" | "documentation" => {
                            if let Err(e) = show_window_and_emit(app_handle, "show-prose-panel", ())
                            {
                                log::warn!("[menu] Failed to show prose panel: {}", e);
                            }
                        }
                        "code_panel" => {
                            if let Err(e) = show_window_and_emit(app_handle, "show-code-panel", ())
                            {
                                log::warn!("[menu] Failed to show code panel: {}", e);
                            }
                        }
                        "hixtory_panel" => {
                            if let Err(e) =
                                show_window_and_emit(app_handle, "show-hixtory-panel", ())
                            {
                                log::warn!("[menu] Failed to show hixtory panel: {}", e);
                            }
                        }
                        "refresh_graph" => {
                            if let Some(window) = app_handle.get_webview_window("main") {
                                let _ = window.emit("refresh-graph", ());
                            }
                        }
                        "toggle_logs" => {
                            if let Some(window) = app_handle.get_webview_window("main") {
                                let _ = window.emit("toggle-logs", ());
                            }
                        }
                        // Help menu
                        "github" => {
                            if let Some(window) = app_handle.get_webview_window("main") {
                                let _ = window.emit("open-url", "https://github.com/teranos/QNTX");
                            }
                        }
                        _ => {}
                    }
                });

                // Create tray icon
                let quit_item = MenuItem::with_id(app, "quit", "Quit QNTX", true, None::<&str>)?;
                let status_item =
                    MenuItem::with_id(app, "status", "Server: Running ✓", false, None::<&str>)?;

                // Preferences menu item with Cmd+, accelerator (standard macOS pattern)
                let preferences_item = MenuItem::with_id(
                    app,
                    "preferences",
                    "Preferences...",
                    true,
                    Some("CmdOrCtrl+,"),
                )?;

                // Pulse daemon control
                let pause_pulse_item = MenuItem::with_id(
                    app,
                    "toggle_pulse",
                    "Pause Pulse Daemon",
                    true,
                    None::<&str>,
                )?;

                // Check current autostart state and create checkbox menu item
                let autostart_manager = app.autolaunch();
                let is_enabled = autostart_manager.is_enabled().unwrap_or(false);
                let autostart_item = CheckMenuItem::with_id(
                    app,
                    "autostart",
                    "Launch at Login",
                    true,
                    is_enabled,
                    None::<&str>,
                )?;

                let separator = PredefinedMenuItem::separator(app)?;
                let separator2 = PredefinedMenuItem::separator(app)?;
                let menu = Menu::with_items(
                    app,
                    &[
                        &status_item,
                        &separator,
                        &preferences_item,
                        &pause_pulse_item,
                        &separator2,
                        &autostart_item,
                        &quit_item,
                    ],
                )?;

                let _tray = TrayIconBuilder::with_id("main")
                    .icon(app.default_window_icon().unwrap().clone())
                    .menu(&menu)
                    .on_menu_event(|app, event| {
                        if event.id == "quit" {
                            // Stop server and quit
                            if let Some(state) = app.try_state::<ServerState>() {
                                let mut child = state.child.lock().unwrap();
                                if let Some(mut process) = child.take() {
                                    let _ = process.kill();
                                }
                            }
                            app.exit(0);
                        } else if event.id == "preferences" {
                            // Open preferences (config panel) - show window and emit event
                            if let Err(e) = show_window_and_emit(app, "show-config-panel", ()) {
                                log::warn!("[tray] Failed to show config panel: {}", e);
                            }
                        } else if event.id == "toggle_pulse" {
                            // Toggle Pulse daemon (emit event to frontend)
                            if let Some(window) = app.get_webview_window("main") {
                                let _ = window.emit("toggle-pulse-daemon", ());
                            }
                        } else if event.id == "autostart" {
                            // Toggle autostart
                            let autostart_manager = app.autolaunch();
                            let is_enabled = autostart_manager.is_enabled().unwrap_or(false);

                            if is_enabled {
                                let _ = autostart_manager.disable();
                            } else {
                                let _ = autostart_manager.enable();
                            }
                        }
                    })
                    .on_tray_icon_event(|tray, event| {
                        if let TrayIconEvent::Click { .. } = event {
                            let app = tray.app_handle();
                            if let Some(window) = app.get_webview_window("main") {
                                let _ = window.show();
                                let _ = window.set_focus();
                            }
                        }
                    })
                    .build(app)?;
            } // End desktop-only menu/tray setup

            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                // Hide window instead of closing (keep server running)
                window.hide().unwrap();
                api.prevent_close();
            }
        })
        .on_page_load(|window, _payload| {
            // Inject backend URL for Tauri environment
            let _ = window.eval(format!(
                "window.__BACKEND_URL__ = 'http://localhost:{}';",
                SERVER_PORT
            ));
        })
        .invoke_handler(tauri::generate_handler![
            get_server_status,
            get_server_url,
            notify_job_completed,
            notify_job_failed,
            notify_storage_warning,
            notify_server_draining,
            notify_server_stopped,
            set_taskbar_progress
            // Video processing (desktop only) - Disabled: blocked by CrabCamera ACL (PR #267)
            // #[cfg(not(any(target_os = "ios", target_os = "android")))]
            // vidstream::vidstream_init,
            // #[cfg(not(any(target_os = "ios", target_os = "android")))]
            // vidstream::vidstream_process_frame,
            // #[cfg(not(any(target_os = "ios", target_os = "android")))]
            // vidstream::vidstream_get_info
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
