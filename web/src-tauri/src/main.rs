// Prevents additional console window on Windows in release builds
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::sync::Mutex;
use tauri::{Manager, State};
use tauri_plugin_shell::ShellExt;

struct ServerState {
    child: Mutex<Option<tauri_plugin_shell::process::CommandChild>>,
}

#[tauri::command]
fn get_server_status(state: State<ServerState>) -> String {
    let child = state.child.lock().unwrap();
    if child.is_some() {
        "running".to_string()
    } else {
        "stopped".to_string()
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
                .args(&["server", "--port", "8765"])
                .spawn()
                .expect("Failed to spawn qntx server");

            // Log server output in development
            #[cfg(debug_assertions)]
            tauri::async_runtime::spawn(async move {
                while let Some(event) = rx.recv().await {
                    match event {
                        tauri_plugin_shell::process::CommandEvent::Stdout(line) => {
                            println!("[qntx] {}", String::from_utf8_lossy(&line));
                        }
                        tauri_plugin_shell::process::CommandEvent::Stderr(line) => {
                            eprintln!("[qntx] {}", String::from_utf8_lossy(&line));
                        }
                        tauri_plugin_shell::process::CommandEvent::Terminated(status) => {
                            println!("[qntx] Server terminated with status: {:?}", status);
                            break;
                        }
                        _ => {}
                    }
                }
            });

            // Store child process handle
            app.manage(ServerState {
                child: Mutex::new(Some(child)),
            });

            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { .. } = event {
                // Stop server on app close
                if let Some(state) = window.try_state::<ServerState>() {
                    let mut child = state.child.lock().unwrap();
                    if let Some(mut process) = child.take() {
                        let _ = process.kill();
                    }
                }
            }
        })
        .invoke_handler(tauri::generate_handler![get_server_status])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
