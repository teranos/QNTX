//! PTY Session Management
//!
//! Handles creating, tracking, and destroying pseudo-terminal sessions.

use parking_lot::Mutex;
use portable_pty::{native_pty_system, CommandBuilder, PtySize};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::Arc;
use thiserror::Error;
use tracing::{debug, info};
use uuid::Uuid;

#[derive(Error, Debug)]
pub enum PTYError {
    #[error("Failed to spawn PTY: {0}")]
    SpawnError(String),

    #[error("Session not found: {0}")]
    SessionNotFound(String),

    #[error("I/O error: {0}")]
    IoError(#[from] std::io::Error),
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct PTYSession {
    pub id: String,
    pub glyph_id: String,
    pub cwd: String,
    pub created: String,
}

/// PTY Manager - handles all PTY sessions
pub struct PTYManager {
    sessions: HashMap<String, Arc<Mutex<PTYSessionHandle>>>,
    home_directory: String,
}

pub struct PTYSessionHandle {
    session: PTYSession,
    reader: Box<dyn std::io::Read + Send>,
    writer: Box<dyn std::io::Write + Send>,
    master: Box<dyn portable_pty::MasterPty + Send>,
    _child: Box<dyn portable_pty::Child + Send + Sync>,
}

impl PTYSessionHandle {
    /// Read from PTY
    pub fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        self.reader.read(buf)
    }

    /// Write to PTY
    pub fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        self.writer.write(buf)
    }

    /// Resize PTY
    pub fn resize(&self, cols: u16, rows: u16) -> std::io::Result<()> {
        self.master
            .resize(portable_pty::PtySize {
                rows,
                cols,
                pixel_width: 0,
                pixel_height: 0,
            })
            .map_err(|e| std::io::Error::new(std::io::ErrorKind::Other, e.to_string()))
    }

    /// Get session metadata
    pub fn session(&self) -> &PTYSession {
        &self.session
    }
}

impl PTYManager {
    pub fn new(home_directory: String) -> Self {
        Self {
            sessions: HashMap::new(),
            home_directory,
        }
    }

    /// Create a new PTY session
    pub fn create_session(&mut self, glyph_id: &str) -> Result<String, PTYError> {
        let session_id = Uuid::new_v4().to_string();

        info!("Creating PTY session {} for glyph {}", session_id, glyph_id);

        // Get PTY system
        let pty_system = native_pty_system();

        // Create PTY with reasonable default size
        let pair = pty_system
            .openpty(PtySize {
                rows: 24,
                cols: 80,
                pixel_width: 0,
                pixel_height: 0,
            })
            .map_err(|e| PTYError::SpawnError(format!("openpty failed: {}", e)))?;

        // Spawn shell
        let shell = std::env::var("SHELL").unwrap_or_else(|_| "/bin/bash".to_string());

        let mut cmd = CommandBuilder::new(&shell);
        cmd.env("TERM", "xterm-256color");

        // Use configured home directory
        cmd.cwd(&self.home_directory);

        let child = pair
            .slave
            .spawn_command(cmd)
            .map_err(|e| PTYError::SpawnError(format!("spawn failed: {}", e)))?;

        debug!("Spawned shell {} in {}", shell, self.home_directory);

        // Create session metadata
        let session = PTYSession {
            id: session_id.clone(),
            glyph_id: glyph_id.to_string(),
            cwd: self.home_directory.clone(),
            created: chrono::Utc::now().to_rfc3339(),
        };

        // Get reader and writer from master
        let reader = pair
            .master
            .try_clone_reader()
            .map_err(|e| PTYError::SpawnError(format!("Failed to clone reader: {}", e)))?;
        let writer = pair
            .master
            .take_writer()
            .map_err(|e| PTYError::SpawnError(format!("Failed to take writer: {}", e)))?;

        let handle = PTYSessionHandle {
            session: session.clone(),
            reader,
            writer,
            master: pair.master,
            _child: child,
        };

        self.sessions
            .insert(session_id.clone(), Arc::new(Mutex::new(handle)));

        info!("PTY session {} created successfully", session_id);

        Ok(session_id)
    }

    /// Get session info
    pub fn get_session(&self, id: &str) -> Option<PTYSession> {
        self.sessions
            .get(id)
            .map(|handle| handle.lock().session.clone())
    }

    /// Get session handle for I/O operations
    pub fn get_session_handle(&self, id: &str) -> Option<Arc<Mutex<PTYSessionHandle>>> {
        self.sessions.get(id).cloned()
    }

    /// Kill a PTY session
    pub fn kill_session(&mut self, id: &str) -> Result<(), PTYError> {
        info!("Killing PTY session {}", id);

        self.sessions
            .remove(id)
            .ok_or_else(|| PTYError::SessionNotFound(id.to_string()))?;

        info!("PTY session {} terminated", id);

        Ok(())
    }

    /// Get count of active sessions
    pub fn session_count(&self) -> usize {
        self.sessions.len()
    }

    /// Shutdown all sessions
    pub fn shutdown_all(&mut self) {
        info!("Shutting down {} PTY sessions", self.sessions.len());
        self.sessions.clear();
    }

    /// List all sessions
    pub fn list_sessions(&self) -> Vec<PTYSession> {
        self.sessions
            .values()
            .map(|handle| handle.lock().session.clone())
            .collect()
    }
}
