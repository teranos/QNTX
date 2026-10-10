package grpc

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
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
// This is useful for creating config from config.PluginKeepaliveConfig settings.
// Nil values use defaults; explicit values (including 0) are used literally.
func NewKeepaliveConfigFromSettings(enabled bool, pingIntervalSecs, pongTimeoutSecs, reconnectAttempts *int) KeepaliveConfig {
	config := DefaultKeepaliveConfig()
	config.Enabled = enabled

	if pingIntervalSecs != nil {
		config.PingInterval = time.Duration(*pingIntervalSecs) * time.Second
	}
	if pongTimeoutSecs != nil {
		config.PongTimeout = time.Duration(*pongTimeoutSecs) * time.Second
	}
	if reconnectAttempts != nil {
		config.ReconnectAttempts = *reconnectAttempts
	}

	return config
}

// KeepaliveMetrics tracks connection health metrics
type KeepaliveMetrics struct {
	// mu protects the metrics
	mu sync.RWMutex

	// latencies stores recent ping/pong latencies for averaging
	latencies []time.Duration

	// sum and average are of latencies, worked out as each one lands
	sum, average time.Duration

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
	m.sum += latency
	if len(m.latencies) > m.maxLatencySamples {
		m.sum -= m.latencies[0]
		m.latencies = m.latencies[1:]
	}
	// Worked out here, where a latency has just landed, so it is never an
	// average of nothing.
	m.average = m.sum / time.Duration(len(m.latencies))
	m.mu.Unlock()
}

// RecordUnmeasuredPong records a pong that answers no ping this side sent, so
// it says the connection is alive and nothing about its latency.
func (m *KeepaliveMetrics) RecordUnmeasuredPong() {
	atomic.AddUint64(&m.totalPongs, 1)
	m.mu.Lock()
	m.lastPongTime = time.Now()
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

// GetAverageLatency returns the average ping/pong latency of the pongs that
// were measured. Before the first, none was measured and it holds 0.
func (m *KeepaliveMetrics) GetAverageLatency() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.average
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
	config     KeepaliveConfig
	metrics    *KeepaliveMetrics
	logger     *zap.SugaredLogger
	pluginName string

	// mu protects state changes
	mu sync.Mutex

	// lastPong tracks when the last PONG was received
	lastPong time.Time

	// pending is the pings sent and not yet answered, by the timestamp each
	// carried. A PONG is measured against the ping it answers.
	pending map[int64]bool

	// running indicates if the keepalive loop is active
	running bool

	// cancel is used to stop the keepalive loop
	cancel context.CancelFunc
}

// NewKeepaliveHandler creates a new KeepaliveHandler with the given configuration
func NewKeepaliveHandler(config KeepaliveConfig, logger *zap.SugaredLogger, pluginName string) *KeepaliveHandler {
	return &KeepaliveHandler{
		config:     config,
		metrics:    NewKeepaliveMetrics(),
		logger:     logger,
		pluginName: pluginName,
		lastPong:   time.Now(),
		pending:    map[int64]bool{},
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

	h.logger.Debugw("Starting keepalive handler",
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

	// running is only ever set together with cancel, in Start.
	h.logger.Debug("Stopping keepalive handler")
	h.cancel()
	h.running = false
}

// IsRunning returns whether the keepalive loop is active
func (h *KeepaliveHandler) IsRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

// HandlePong processes a PONG message. Any PONG says the connection is alive;
// only one answering a ping this side sent says how long the round trip took.
func (h *KeepaliveHandler) HandlePong(msg *protocol.WebSocketMessage) {
	h.mu.Lock()
	h.lastPong = time.Now()
	answers := h.pending[msg.Timestamp]
	// A ping sent before the one answered is not answered any more.
	for sent := range h.pending {
		if sent <= msg.Timestamp {
			delete(h.pending, sent)
		}
	}
	h.mu.Unlock()

	if !answers {
		h.metrics.RecordUnmeasuredPong()
		h.logger.Debugw("PONG received, answering no ping sent", "timestamp", msg.Timestamp)
		return
	}
	latency := time.Since(time.Unix(0, msg.Timestamp))
	h.metrics.RecordPong(latency)
	h.logger.Debugw("PONG received", "latency", latency)
}

// sent records a ping as sent with timestamp, for the PONG that answers it.
func (h *KeepaliveHandler) sent(timestamp int64) {
	h.mu.Lock()
	h.pending[timestamp] = true
	h.mu.Unlock()
	h.metrics.RecordPing()
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
			// No PONG within PongTimeout is a connection considered dead: the
			// keepalive stops pinging it.
			if h.CheckTimeout() {
				h.Stop()
				h.logger.Warnf("[%s] WebSocket pong timeout — not responding to keepalive, keepalive stopped", h.pluginName)
				return
			}

			// Send PING with current timestamp
			timestamp := time.Now().UnixNano()
			h.sent(timestamp)

			// A PING that cannot be sent is a stream that is gone.
			if err := sendPing(timestamp); err != nil {
				h.Stop()
				h.logger.Warnw("Failed to send PING, keepalive stopped", "plugin", h.pluginName, "error", err)
				return
			}
			h.logger.Debug("PING sent")
		}
	}
}

// ConnectWithRetry attempts to establish a connection with exponential backoff
// Each attempt's failure is kept in the error returned; 0 attempts is no
// connection.
func (h *KeepaliveHandler) ConnectWithRetry(ctx context.Context, connect func() error) error {
	failed := errors.Newf("failed after %d reconnect attempts", h.config.ReconnectAttempts)

	for attempt := 0; attempt < h.config.ReconnectAttempts; attempt++ {
		h.metrics.RecordReconnect()

		err := connect()
		if err == nil {
			h.metrics.ResetConnectionStart()
			h.logger.Infow("Connection established",
				"attempt", attempt+1,
				"total_attempts", h.config.ReconnectAttempts,
			)
			return nil
		}
		failed = errors.WithSecondaryError(failed,
			errors.Wrapf(err, "attempt %d of %d", attempt+1, h.config.ReconnectAttempts))

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

	return failed
}
