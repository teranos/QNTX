//! WebSocket Handler for PTY I/O
//!
//! Handles bidirectional streaming between browser terminal and PTY.

use futures::StreamExt;
use parking_lot::RwLock;
use std::sync::Arc;
use tokio::sync::mpsc;
use tonic::{Status, Streaming};
use tracing::{debug, error, info, warn};

use crate::proto::web_socket_message::Type as WsType;
use crate::proto::WebSocketMessage;
use crate::pty::PTYManager;

pub async fn handle_pty_websocket(
    mut stream: Streaming<WebSocketMessage>,
    session_id: String,
    pty_manager: Arc<RwLock<PTYManager>>,
) -> Result<mpsc::Receiver<Result<WebSocketMessage, Status>>, Status> {
    info!(
        "WebSocket connection established for session {}",
        session_id
    );

    // Get PTY session handle
    let handle = pty_manager
        .read()
        .get_session_handle(&session_id)
        .ok_or_else(|| Status::not_found(format!("Session not found: {}", session_id)))?;

    // Create channel for sending messages back to client
    let (tx, rx) = mpsc::channel::<Result<WebSocketMessage, Status>>(100);

    // Spawn task to handle incoming WebSocket messages (client -> PTY)
    let handle_clone = handle.clone();
    let session_id_clone = session_id.clone();
    tokio::spawn(async move {
        while let Some(result) = stream.next().await {
            match result {
                Ok(msg) => {
                    match WsType::try_from(msg.r#type) {
                        Ok(WsType::Data) => {
                            // Terminal input data
                            if !msg.data.is_empty() {
                                let mut pty = handle_clone.lock();
                                match pty.write(&msg.data) {
                                    Ok(n) => {
                                        debug!("Wrote {} bytes to PTY", n);
                                    }
                                    Err(e) => {
                                        error!("Failed to write to PTY: {}", e);
                                        break;
                                    }
                                }
                            }
                        }
                        Ok(WsType::Ping) => {
                            // Handle resize in metadata
                            if let (Some(cols_str), Some(rows_str)) =
                                (msg.headers.get("cols"), msg.headers.get("rows"))
                            {
                                if let (Ok(cols), Ok(rows)) =
                                    (cols_str.parse::<u16>(), rows_str.parse::<u16>())
                                {
                                    debug!("Resizing PTY to {}x{}", cols, rows);
                                    let pty = handle_clone.lock();
                                    if let Err(e) = pty.resize(cols, rows) {
                                        warn!("Failed to resize PTY: {}", e);
                                    }
                                }
                            }
                        }
                        Ok(WsType::Close) => {
                            debug!("Client sent close message");
                            break;
                        }
                        _ => {
                            debug!("Unhandled message type: {:?}", msg.r#type);
                        }
                    }
                }
                Err(e) => {
                    error!("WebSocket error receiving message: {}", e);
                    break;
                }
            }
        }
        debug!(
            "WebSocket input handler finished for session {}",
            session_id_clone
        );
    });

    // Spawn task to read from PTY and send to WebSocket (PTY -> client)
    let handle_clone = handle.clone();
    let session_id_clone = session_id.clone();
    let pty_manager_clone = pty_manager.clone();
    tokio::spawn(async move {
        loop {
            // Read from PTY (blocking operation, run in blocking task)
            // Create fresh buffer for each iteration
            let handle_clone2 = handle_clone.clone();
            let read_result = tokio::task::spawn_blocking(move || {
                let mut buf = vec![0u8; 4096];
                let mut pty = handle_clone2.lock();
                match pty.read(&mut buf) {
                    Ok(n) => Ok((buf, n)),
                    Err(e) => Err(e),
                }
            })
            .await;

            match read_result {
                Ok(Ok((buf, n))) if n > 0 => {
                    debug!("Read {} bytes from PTY", n);
                    let msg = WebSocketMessage {
                        r#type: WsType::Data as i32,
                        data: buf[..n].to_vec(),
                        headers: Default::default(),
                        timestamp: 0,
                    };

                    if tx.send(Ok(msg)).await.is_err() {
                        debug!("Client disconnected");
                        break;
                    }
                }
                Ok(Ok(_)) => {
                    // EOF - PTY closed
                    debug!("PTY closed (EOF)");
                    break;
                }
                Ok(Err(e)) => {
                    error!("Error reading from PTY: {}", e);
                    break;
                }
                Err(e) => {
                    error!("Task join error: {}", e);
                    break;
                }
            }
        }

        // Clean up PTY session — prevents leak when WebSocket disconnects
        if let Err(e) = pty_manager_clone.write().kill_session(&session_id_clone) {
            warn!("Failed to clean up PTY session {}: {}", session_id_clone, e);
        } else {
            info!("Cleaned up PTY session {} after WebSocket disconnect", session_id_clone);
        }
    });

    Ok(rx)
}
