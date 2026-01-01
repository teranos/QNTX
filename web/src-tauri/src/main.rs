// Prevents additional console window on Windows in release builds
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::sync::{Arc, Mutex};
use tauri::{Manager, State};
use tauri::tray::{TrayIconBuilder, TrayIconEvent};
use tauri::menu::{Menu, MenuItem};
use tauri_plugin_shell::ShellExt;

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

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .setup(|app| {
            // Start QNTX server as sidecar
            let sidecar_command = app.shell().sidecar("qntx")
                .expect("failed to create qntx sidecar command");

            let (mut rx, child) = sidecar_command
                .args(&["server", "--port", SERVER_PORT, "--dev"])
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
            let status_item = MenuItem::with_id(app, "status", "Server: Running ✓", false, None::<&str>)?;
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
        .invoke_handler(tauri::generate_handler![
            get_server_status,
            get_server_url
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
