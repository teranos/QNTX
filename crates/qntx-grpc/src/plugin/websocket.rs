//! WebSocket stream helper for QNTX plugins.
//!
//! Wraps the tonic bidirectional stream from `handle_web_socket` and handles
//! PING/PONG keepalive transparently. Plugin code only sees DATA messages —
//! keepalive is invisible.
//!
//! # Usage
//!
//! ```ignore
//! async fn handle_web_socket(
//!     &self,
//!     request: Request<Streaming<WebSocketMessage>>,
//! ) -> Result<Response<Self::HandleWebSocketStream>, Status> {
//!     let (ws, data_rx, response_stream) = PluginWebSocket::new(request.into_inner());
//!
//!     tokio::spawn(async move {
//!         while let Some(data) = data_rx.recv().await {
//!             // handle incoming DATA from browser
//!         }
//!     });
//!
//!     ws.send(b"hello").await;
//!
//!     Ok(Response::new(response_stream))
//! }
//! ```

use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::Streaming;
use tracing::debug;

use qntx_proto::{web_socket_message, WebSocketMessage};

/// A WebSocket helper that handles PING/PONG automatically.
///
/// Created from the incoming `Streaming<WebSocketMessage>` in `handle_web_socket`.
/// Responds to PINGs with PONGs transparently and forwards DATA payloads
/// to the plugin via a channel.
pub struct PluginWebSocket {
    outbound_tx: Arc<mpsc::Sender<Result<WebSocketMessage, tonic::Status>>>,
}

impl PluginWebSocket {
    /// Create a PluginWebSocket from the incoming gRPC stream.
    ///
    /// Returns:
    /// - `PluginWebSocket` for sending DATA messages to the browser
    /// - `mpsc::Receiver<Vec<u8>>` that receives DATA payloads from the browser
    /// - `ReceiverStream` to return from `handle_web_socket` as the response stream
    ///
    /// PING/PONG is handled internally — the receiver only gets DATA.
    pub fn new(
        incoming: Streaming<WebSocketMessage>,
    ) -> (
        Self,
        mpsc::Receiver<Vec<u8>>,
        ReceiverStream<Result<WebSocketMessage, tonic::Status>>,
    ) {
        let (outbound_tx, outbound_rx) = mpsc::channel(64);
        let (data_tx, data_rx) = mpsc::channel(64);

        let outbound_tx = Arc::new(outbound_tx);
        let reader_tx = outbound_tx.clone();

        // Read incoming stream: respond to PINGs, forward DATA to plugin
        tokio::spawn(async move {
            let mut stream = incoming;
            while let Some(result) = tokio_stream::StreamExt::next(&mut stream).await {
                let msg = match result {
                    Ok(msg) => msg,
                    Err(e) => {
                        debug!("WebSocket stream error: {}", e);
                        break;
                    }
                };

                match web_socket_message::Type::try_from(msg.r#type) {
                    Ok(web_socket_message::Type::Ping) => {
                        let pong = WebSocketMessage {
                            r#type: web_socket_message::Type::Pong as i32,
                            timestamp: msg.timestamp,
                            ..Default::default()
                        };
                        if reader_tx.send(Ok(pong)).await.is_err() {
                            break;
                        }
                    }
                    #[allow(clippy::collapsible_match)]
                    Ok(web_socket_message::Type::Data) => {
                        if data_tx.send(msg.data).await.is_err() {
                            break;
                        }
                    }
                    Ok(web_socket_message::Type::Connect) => {
                        debug!("WebSocket CONNECT received");
                    }
                    Ok(web_socket_message::Type::Close) => {
                        debug!("WebSocket CLOSE received");
                        break;
                    }
                    _ => {}
                }
            }
        });

        let response_stream = ReceiverStream::new(outbound_rx);
        (Self { outbound_tx }, data_rx, response_stream)
    }

    /// Send data to the browser.
    pub async fn send(&self, data: &[u8]) -> bool {
        let msg = WebSocketMessage {
            r#type: web_socket_message::Type::Data as i32,
            data: data.to_vec(),
            timestamp: now_nanos(),
            ..Default::default()
        };
        self.outbound_tx.send(Ok(msg)).await.is_ok()
    }

    /// Send a typed WebSocketMessage directly.
    pub async fn send_message(&self, msg: WebSocketMessage) -> bool {
        self.outbound_tx.send(Ok(msg)).await.is_ok()
    }
}

fn now_nanos() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos() as i64
}
