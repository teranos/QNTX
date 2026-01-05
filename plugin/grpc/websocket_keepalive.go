package grpc

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// Default keepalive configuration values
const (
	DefaultPingInterval      = 30 * time.Second
	DefaultPongTimeout       = 60 * time.Second
	DefaultReconnectAttempts = 3
	DefaultReconnectBaseWait = time.Second
)

// KeepaliveConfig contains configuration for WebSocket keepalive behavior
type KeepaliveConfig struct {
	// Enabled determines if keepalive is active
	Enabled bool

	// PingInterval is how often to send PING messages
	PingInterval time.Duration

	// PongTimeout is how long to wait for PONG before considering connection dead
	PongTimeout time.Duration

	// ReconnectAttempts is the number of reconnect attempts on connection loss
	ReconnectAttempts int

	// ReconnectBaseWait is the base wait time for exponential backoff reconnection
	ReconnectBaseWait time.Duration
}

// DefaultKeepaliveConfig returns the default keepalive configuration
func DefaultKeepaliveConfig() KeepaliveConfig {
	return KeepaliveConfig{
		Enabled:           true,
		PingInterval:      DefaultPingInterval,
		PongTimeout:       DefaultPongTimeout,
		ReconnectAttempts: DefaultReconnectAttempts,
		ReconnectBaseWait: DefaultReconnectBaseWait,
	}
}

// NewKeepaliveConfigFromSettings creates a KeepaliveConfig from configuration values.
// This is useful for creating config from am.PluginKeepaliveConfig settings.
// Pass 0 for any value to use defaults.
func NewKeepaliveConfigFromSettings(enabled bool, pingIntervalSecs, pongTimeoutSecs, reconnectAttempts int) KeepaliveConfig {
	config := DefaultKeepaliveConfig()
	config.Enabled = enabled

	if pingIntervalSecs > 0 {
		config.PingInterval = time.Duration(pingIntervalSecs) * time.Second
	}
	if pongTimeoutSecs > 0 {
		config.PongTimeout = time.Duration(pongTimeoutSecs) * time.Second
	}
	if reconnectAttempts > 0 {
		config.ReconnectAttempts = reconnectAttempts
	}

	return config
}

// KeepaliveMetrics tracks connection health metrics
type KeepaliveMetrics struct {
	// mu protects the metrics
	mu sync.RWMutex

	// latencies stores recent ping/pong latencies for averaging
	latencies []time.Duration

	// maxLatencySamples is the maximum number of latency samples to keep
	maxLatencySamples int

	// totalPings is the total number of pings sent
	totalPings uint64

	// totalPongs is the total number of pongs received
	totalPongs uint64

	// reconnectCount is the number of reconnection attempts
	reconnectCount uint64

	// connectionUptime tracks when the connection was established
	connectionStart time.Time

	// lastPingTime is when the last ping was sent
	lastPingTime time.Time

	// lastPongTime is when the last pong was received
	lastPongTime time.Time
}

// NewKeepaliveMetrics creates a new KeepaliveMetrics instance
func NewKeepaliveMetrics() *KeepaliveMetrics {
	return &KeepaliveMetrics{
		latencies:         make([]time.Duration, 0, 100),
		maxLatencySamples: 100,
		connectionStart:   time.Now(),
	}
}

// RecordPing records that a ping was sent
func (m *KeepaliveMetrics) RecordPing() {
	atomic.AddUint64(&m.totalPings, 1)
	m.mu.Lock()
	m.lastPingTime = time.Now()
	m.mu.Unlock()
}

// RecordPong records that a pong was received with latency
func (m *KeepaliveMetrics) RecordPong(latency time.Duration) {
	atomic.AddUint64(&m.totalPongs, 1)
	m.mu.Lock()
	m.lastPongTime = time.Now()
	m.latencies = append(m.latencies, latency)
	if len(m.latencies) > m.maxLatencySamples {
		m.latencies = m.latencies[1:]
	}
	m.mu.Unlock()
}

// RecordReconnect records a reconnection attempt
func (m *KeepaliveMetrics) RecordReconnect() {
	atomic.AddUint64(&m.reconnectCount, 1)
}

// ResetConnectionStart resets the connection start time
func (m *KeepaliveMetrics) ResetConnectionStart() {
	m.mu.Lock()
	m.connectionStart = time.Now()
	m.mu.Unlock()
}

// GetAverageLatency returns the average ping/pong latency
func (m *KeepaliveMetrics) GetAverageLatency() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.latencies) == 0 {
		return 0
	}

	var total time.Duration
	for _, l := range m.latencies {
		total += l
	}
	return total / time.Duration(len(m.latencies))
}

// GetConnectionUptime returns how long the connection has been up
func (m *KeepaliveMetrics) GetConnectionUptime() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return time.Since(m.connectionStart)
}

// GetTotalPings returns the total number of pings sent
func (m *KeepaliveMetrics) GetTotalPings() uint64 {
	return atomic.LoadUint64(&m.totalPings)
}

// GetTotalPongs returns the total number of pongs received
func (m *KeepaliveMetrics) GetTotalPongs() uint64 {
	return atomic.LoadUint64(&m.totalPongs)
}

// GetReconnectCount returns the number of reconnection attempts
func (m *KeepaliveMetrics) GetReconnectCount() uint64 {
	return atomic.LoadUint64(&m.reconnectCount)
}

// GetLastPingTime returns when the last ping was sent
func (m *KeepaliveMetrics) GetLastPingTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastPingTime
}

// GetLastPongTime returns when the last pong was received
func (m *KeepaliveMetrics) GetLastPongTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastPongTime
}

// KeepaliveHandler manages the keepalive mechanism for a WebSocket connection
type KeepaliveHandler struct {
	config  KeepaliveConfig
	metrics *KeepaliveMetrics
	logger  *zap.SugaredLogger

	// mu protects state changes
	mu sync.Mutex

	// lastPong tracks when the last PONG was received
	lastPong time.Time

	// running indicates if the keepalive loop is active
	running bool

	// cancel is used to stop the keepalive loop
	cancel context.CancelFunc
}

// NewKeepaliveHandler creates a new KeepaliveHandler with the given configuration
func NewKeepaliveHandler(config KeepaliveConfig, logger *zap.SugaredLogger) *KeepaliveHandler {
	return &KeepaliveHandler{
		config:   config,
		metrics:  NewKeepaliveMetrics(),
		logger:   logger,
		lastPong: time.Now(),
	}
}

// Metrics returns the keepalive metrics
func (h *KeepaliveHandler) Metrics() *KeepaliveMetrics {
	return h.metrics
}

// Start begins the keepalive loop, sending periodic PINGs
// sendPing is called to send a PING message and should return an error if sending fails
func (h *KeepaliveHandler) Start(ctx context.Context, sendPing func(timestamp int64) error) {
	if !h.config.Enabled {
		h.logger.Debug("Keepalive disabled, not starting")
		return
	}

	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}

	ctx, h.cancel = context.WithCancel(ctx)
	h.running = true
	h.lastPong = time.Now()
	h.mu.Unlock()

	h.logger.Infow("Starting keepalive handler",
		"ping_interval", h.config.PingInterval,
		"pong_timeout", h.config.PongTimeout,
	)

	go h.keepaliveLoop(ctx, sendPing)
}

// Stop stops the keepalive loop
func (h *KeepaliveHandler) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return
	}

	h.logger.Debug("Stopping keepalive handler")
	if h.cancel != nil {
		h.cancel()
	}
	h.running = false
}

// IsRunning returns whether the keepalive loop is active
func (h *KeepaliveHandler) IsRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

// HandlePong processes a PONG message
func (h *KeepaliveHandler) HandlePong(msg *protocol.WebSocketMessage) {
	h.mu.Lock()
	h.lastPong = time.Now()
	h.mu.Unlock()

	// Calculate latency if timestamp is present
	if msg.Timestamp > 0 {
		sentTime := time.Unix(0, msg.Timestamp)
		latency := time.Since(sentTime)
		h.metrics.RecordPong(latency)
		h.logger.Debugw("PONG received", "latency", latency)
	} else {
		h.metrics.RecordPong(0)
		h.logger.Debug("PONG received (no timestamp)")
	}
}

// HandlePing processes a PING message and returns a PONG response
func (h *KeepaliveHandler) HandlePing(msg *protocol.WebSocketMessage) *protocol.WebSocketMessage {
	h.logger.Debug("PING received, sending PONG")
	return &protocol.WebSocketMessage{
		Type:      protocol.WebSocketMessage_PONG,
		Timestamp: msg.Timestamp, // Echo back the timestamp for latency calculation
	}
}

// CheckTimeout returns true if the connection should be considered dead
func (h *KeepaliveHandler) CheckTimeout() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if time.Since(h.lastPong) > h.config.PongTimeout {
		return true
	}
	return false
}

// keepaliveLoop runs the periodic PING sending
func (h *KeepaliveHandler) keepaliveLoop(ctx context.Context, sendPing func(timestamp int64) error) {
	ticker := time.NewTicker(h.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.logger.Debug("Keepalive loop stopped")
			return

		case <-ticker.C:
			// Check for pong timeout
			if h.CheckTimeout() {
				h.logger.Warn("WebSocket pong timeout, connection may be stale")
				// Continue sending pings in case the connection recovers
			}

			// Send PING with current timestamp
			timestamp := time.Now().UnixNano()
			h.metrics.RecordPing()

			if err := sendPing(timestamp); err != nil {
				h.logger.Warnw("Failed to send PING", "error", err)
				// Don't return, keep trying
			} else {
				h.logger.Debug("PING sent")
			}
		}
	}
}

// ConnectWithRetry attempts to establish a connection with exponential backoff
func (h *KeepaliveHandler) ConnectWithRetry(ctx context.Context, connect func() error) error {
	var lastErr error

	for attempt := 0; attempt < h.config.ReconnectAttempts; attempt++ {
		h.metrics.RecordReconnect()

		if err := connect(); err == nil {
			h.metrics.ResetConnectionStart()
			h.logger.Infow("Connection established",
				"attempt", attempt+1,
				"total_attempts", h.config.ReconnectAttempts,
			)
			return nil
		} else {
			lastErr = err
			h.logger.Warnw("Connection attempt failed",
				"attempt", attempt+1,
				"total_attempts", h.config.ReconnectAttempts,
				"error", err,
			)
		}

		// Calculate backoff with exponential increase
		backoff := h.config.ReconnectBaseWait * time.Duration(math.Pow(2, float64(attempt)))

		// Wait with context cancellation support
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("failed after %d reconnect attempts: %w", h.config.ReconnectAttempts, lastErr)
}

// HandleMessage processes incoming WebSocket messages for keepalive-related types
// Returns the PONG response for PING messages, nil for other types
// Returns an error for ERROR message types
func (h *KeepaliveHandler) HandleMessage(msg *protocol.WebSocketMessage) (*protocol.WebSocketMessage, error) {
	switch msg.Type {
	case protocol.WebSocketMessage_PING:
		return h.HandlePing(msg), nil

	case protocol.WebSocketMessage_PONG:
		h.HandlePong(msg)
		return nil, nil

	case protocol.WebSocketMessage_ERROR:
		errMsg := string(msg.Data)
		h.logger.Errorw("WebSocket error received", "error", errMsg)
		return nil, fmt.Errorf("websocket error: %s", errMsg)

	default:
		// Not a keepalive message, let caller handle it
		return nil, nil
	}
}
