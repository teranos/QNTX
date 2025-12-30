package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// ConsoleLog represents a browser console message
type ConsoleLog struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`     // error, warn, info, debug
	Message   string    `json:"message"`
	Stack     string    `json:"stack,omitempty"`
	URL       string    `json:"url,omitempty"`
}

// ConsoleBuffer holds recent browser console logs
type ConsoleBuffer struct {
	logs     []ConsoleLog
	maxSize  int
	mu       sync.RWMutex
	onNewLog func(ConsoleLog) // Callback for printing to terminal
}

// NewConsoleBuffer creates a new console log buffer
func NewConsoleBuffer(maxSize int) *ConsoleBuffer {
	return &ConsoleBuffer{
		logs:    make([]ConsoleLog, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add adds a log entry to the buffer
func (cb *ConsoleBuffer) Add(log ConsoleLog) {
	cb.mu.Lock()

	// Add timestamp if not set
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now()
	}

	// Add to buffer (circular)
	if len(cb.logs) >= cb.maxSize {
		cb.logs = cb.logs[1:] // Remove oldest
	}
	cb.logs = append(cb.logs, log)

	// Get callback reference while holding lock
	callback := cb.onNewLog
	cb.mu.Unlock()

	// Call callback outside lock to prevent deadlocks
	if callback != nil {
		callback(log)
	}
}

// GetAll returns all logs in the buffer
func (cb *ConsoleBuffer) GetAll() []ConsoleLog {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// Return a copy
	result := make([]ConsoleLog, len(cb.logs))
	copy(result, cb.logs)
	return result
}

// HandleDebug handles browser console debugging endpoint
// POST: Add console log to buffer
// GET: Retrieve all console logs from buffer
func (s *QNTXServer) HandleDebug(w http.ResponseWriter, r *http.Request) {
	// Only allow in dev mode
	if !s.isDevMode() {
		http.Error(w, "Debug endpoint only available in dev mode", http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodPost:
		// Limit request body size to 100KB to prevent DoS
		r.Body = http.MaxBytesReader(w, r.Body, 100*1024)

		var log ConsoleLog
		if err := json.NewDecoder(r.Body).Decode(&log); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Validate input
		if len(log.Message) > 10000 {
			http.Error(w, "Message too long (max 10000 characters)", http.StatusBadRequest)
			return
		}
		if len(log.Stack) > 50000 {
			http.Error(w, "Stack trace too long (max 50000 characters)", http.StatusBadRequest)
			return
		}

		// Validate and sanitize level (always default to 'info' if empty or invalid)
		validLevels := map[string]bool{"error": true, "warn": true, "info": true, "debug": true}
		if log.Level == "" || !validLevels[log.Level] {
			log.Level = "info" // Sanitize to safe default
		}

		// Add to buffer
		if s.consoleBuffer != nil {
			s.consoleBuffer.Add(log)
		}

		w.WriteHeader(http.StatusNoContent)

	case http.MethodGet:
		logs := make([]ConsoleLog, 0)
		if s.consoleBuffer != nil {
			logs = s.consoleBuffer.GetAll()
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(logs); err != nil {
			s.logger.Errorw("Failed to encode console logs", "error", err)
		}

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
