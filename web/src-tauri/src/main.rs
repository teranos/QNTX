// Prevents additional console window on Windows in release builds
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::sync::{Arc, Mutex};
use tauri::menu::{Menu, MenuItem};
use tauri::tray::{TrayIconBuilder, TrayIconEvent};
use tauri::{Manager, State};
use tauri_plugin_notification::NotificationExt;
use tauri_plugin_shell::ShellExt;

// Import generated types from Go source (single source of truth)
// These types are kept in sync with the backend via `make types`
#[allow(unused_imports)]
use qntx_types::{
    async_types::{Job, JobStatus},
    server::{DaemonStatusMessage, JobUpdateMessage, StorageWarningMessage},
};

const SERVER_PORT: &str = "877";

struct ServerState {
    child: Arc<Mutex<Option<tauri_plugin_shell::process::CommandChild>>>,
    port: String,
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
) {
    let duration_text = duration_ms
        .map(|ms| format!(" in {:.1}s", ms as f64 / 1000.0))
        .unwrap_or_default();

    if let Err(e) = app
        .notification()
        .builder()
        .title("QNTX: Job Completed")
        .body(&format!("{}{}", handler_name, duration_text))
        .show()
    {
        eprintln!("[notification] Failed to show job completion: {}", e);
    }

    println!(
        "[notification] Job completed: {} ({})",
        handler_name, job_id
    );
}

/// Send a native notification for job failure
#[tauri::command]
fn notify_job_failed(
    app: tauri::AppHandle,
    handler_name: String,
    job_id: String,
    error: Option<String>,
) {
    let error_text = error.unwrap_or_else(|| "Unknown error".to_string());

    if let Err(e) = app
        .notification()
        .builder()
        .title("QNTX: Job Failed")
        .body(&format!("{}: {}", handler_name, error_text))
        .show()
    {
        eprintln!("[notification] Failed to show job failure: {}", e);
    }

    println!(
        "[notification] Job failed: {} ({}) - {}",
        handler_name, job_id, error_text
    );
}

/// Send a native notification for storage warnings
#[tauri::command]
fn notify_storage_warning(app: tauri::AppHandle, actor: String, fill_percent: f64) {
    if let Err(e) = app
        .notification()
        .builder()
        .title("QNTX: Storage Warning")
        .body(&format!(
            "{} is at {:.0}% capacity",
            actor,
            fill_percent * 100.0
        ))
        .show()
    {
        eprintln!("[notification] Failed to show storage warning: {}", e);
    }

    println!(
        "[notification] Storage warning: {} at {:.0}%",
        actor,
        fill_percent * 100.0
    );
}

/// Send a native notification when server enters draining mode
/// This happens during graceful shutdown when completing remaining jobs
#[tauri::command]
fn notify_server_draining(app: tauri::AppHandle, active_jobs: i64, queued_jobs: i64) {
    let total = active_jobs + queued_jobs;
    let body = if total > 0 {
        format!("Completing {} remaining job(s) before shutdown", total)
    } else {
        "Preparing for shutdown...".to_string()
    };

    if let Err(e) = app
        .notification()
        .builder()
        .title("QNTX: Server Draining")
        .body(&body)
        .show()
    {
        eprintln!("[notification] Failed to show server draining: {}", e);
    }

    println!(
        "[notification] Server draining: {} active, {} queued",
        active_jobs, queued_jobs
    );
}

/// Send a native notification when server stops
#[tauri::command]
fn notify_server_stopped(app: tauri::AppHandle) {
    if let Err(e) = app
        .notification()
        .builder()
        .title("QNTX: Server Stopped")
        .body("The QNTX server has stopped")
        .show()
    {
        eprintln!("[notification] Failed to show server stopped: {}", e);
    }

    println!("[notification] Server stopped");
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_notification::init())
        .setup(|app| {
            // Start QNTX server as sidecar
            let sidecar_command = app
                .shell()
                .sidecar("qntx")
                .expect("failed to create qntx sidecar command");

            let (mut rx, child) = sidecar_command
                .args(&["server", "--port", SERVER_PORT, "--dev", "--no-browser"])
                .spawn()
                .expect("Failed to spawn qntx server");

            // Log server output
            tauri::async_runtime::spawn(async move {
                while let Some(event) = rx.recv().await {
                    match event {
                        tauri_plugin_shell::process::CommandEvent::Stdout(line) => {
                            print!("[qntx] {}", String::from_utf8_lossy(&line));
                        }
                        tauri_plugin_shell::process::CommandEvent::Stderr(line) => {
                            eprint!("[qntx] {}", String::from_utf8_lossy(&line));
                        }
                        tauri_plugin_shell::process::CommandEvent::Terminated(status) => {
                            eprintln!("[qntx] Server terminated: {:?}", status);
                            break;
                        }
                        _ => {}
                    }
                }
            });

            // Store server state
            app.manage(ServerState {
                child: Arc::new(Mutex::new(Some(child))),
                port: SERVER_PORT.to_string(),
            });

            // Create tray icon
            let quit_item = MenuItem::with_id(app, "quit", "Quit QNTX", true, None::<&str>)?;
            let status_item =
                MenuItem::with_id(app, "status", "Server: Running ✓", false, None::<&str>)?;
            let menu = Menu::with_items(app, &[&status_item, &quit_item])?;

            let _tray = TrayIconBuilder::with_id("main")
                .icon(app.default_window_icon().unwrap().clone())
                .menu(&menu)
                .on_menu_event(|app, event| {
                    if event.id == "quit" {
                        // Stop server and quit
                        if let Some(state) = app.try_state::<ServerState>() {
                            let mut child = state.child.lock().unwrap();
                            if let Some(process) = child.take() {
                                let _ = process.kill();
                            }
                        }
                        app.exit(0);
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

            Ok(())
        })
        .on_window_event(|window, event| {
            match event {
                tauri::WindowEvent::CloseRequested { api, .. } => {
                    // Hide window instead of closing (keep server running)
                    window.hide().unwrap();
                    api.prevent_close();
                }
                _ => {}
            }
        })
        .on_page_load(|window, _payload| {
            // Inject backend URL for Tauri environment
            let _ = window.eval(&format!(
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
            notify_server_stopped
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
