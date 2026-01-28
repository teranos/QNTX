//go:build integration

package grpc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap/zaptest"
)

// skipIfShort skips keepalive tests in short mode as they require timing operations
func skipIfShort(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping keepalive test in short mode - requires timing operations")
	}
}

// =============================================================================
// KeepaliveConfig Tests
// =============================================================================

func TestDefaultKeepaliveConfig(t *testing.T) {
	config := DefaultKeepaliveConfig()

	assert.True(t, config.Enabled)
	assert.Equal(t, DefaultPingInterval, config.PingInterval)
	assert.Equal(t, DefaultPongTimeout, config.PongTimeout)
	assert.Equal(t, DefaultReconnectAttempts, config.ReconnectAttempts)
	assert.Equal(t, DefaultReconnectBaseWait, config.ReconnectBaseWait)
}

func TestNewKeepaliveConfigFromSettings(t *testing.T) {
	// Helper to create int pointers
	intPtr := func(v int) *int { return &v }

	tests := []struct {
		name              string
		enabled           bool
		pingIntervalSecs  *int
		pongTimeoutSecs   *int
		reconnectAttempts *int
		wantPingInterval  time.Duration
		wantPongTimeout   time.Duration
		wantReconnect     int
	}{
		{
			name:              "all defaults (nil)",
			enabled:           true,
			pingIntervalSecs:  nil,
			pongTimeoutSecs:   nil,
			reconnectAttempts: nil,
			wantPingInterval:  DefaultPingInterval,
			wantPongTimeout:   DefaultPongTimeout,
			wantReconnect:     DefaultReconnectAttempts,
		},
		{
			name:              "explicit zero means zero",
			enabled:           true,
			pingIntervalSecs:  intPtr(0),
			pongTimeoutSecs:   intPtr(0),
			reconnectAttempts: intPtr(0),
			wantPingInterval:  0,
			wantPongTimeout:   0,
			wantReconnect:     0,
		},
		{
			name:              "custom values",
			enabled:           false,
			pingIntervalSecs:  intPtr(15),
			pongTimeoutSecs:   intPtr(45),
			reconnectAttempts: intPtr(5),
			wantPingInterval:  15 * time.Second,
			wantPongTimeout:   45 * time.Second,
			wantReconnect:     5,
		},
		{
			name:              "mixed custom and defaults",
			enabled:           true,
			pingIntervalSecs:  intPtr(20),
			pongTimeoutSecs:   nil,
			reconnectAttempts: nil,
			wantPingInterval:  20 * time.Second,
			wantPongTimeout:   DefaultPongTimeout,
			wantReconnect:     DefaultReconnectAttempts,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := NewKeepaliveConfigFromSettings(
				tt.enabled,
				tt.pingIntervalSecs,
				tt.pongTimeoutSecs,
				tt.reconnectAttempts,
			)

			assert.Equal(t, tt.enabled, config.Enabled)
			assert.Equal(t, tt.wantPingInterval, config.PingInterval)
			assert.Equal(t, tt.wantPongTimeout, config.PongTimeout)
			assert.Equal(t, tt.wantReconnect, config.ReconnectAttempts)
		})
	}
}

// =============================================================================
// KeepaliveMetrics Tests
// =============================================================================

func TestKeepaliveMetrics_RecordPing(t *testing.T) {
	metrics := NewKeepaliveMetrics()

	assert.Equal(t, uint64(0), metrics.GetTotalPings())

	metrics.RecordPing()
	assert.Equal(t, uint64(1), metrics.GetTotalPings())

	metrics.RecordPing()
	metrics.RecordPing()
	assert.Equal(t, uint64(3), metrics.GetTotalPings())

	// Verify last ping time is updated
	assert.False(t, metrics.GetLastPingTime().IsZero())
}

func TestKeepaliveMetrics_RecordPong(t *testing.T) {
	metrics := NewKeepaliveMetrics()

	assert.Equal(t, uint64(0), metrics.GetTotalPongs())
	assert.Equal(t, time.Duration(0), metrics.GetAverageLatency())

	metrics.RecordPong(100 * time.Millisecond)
	assert.Equal(t, uint64(1), metrics.GetTotalPongs())
	assert.Equal(t, 100*time.Millisecond, metrics.GetAverageLatency())

	metrics.RecordPong(200 * time.Millisecond)
	assert.Equal(t, uint64(2), metrics.GetTotalPongs())
	assert.Equal(t, 150*time.Millisecond, metrics.GetAverageLatency())

	// Verify last pong time is updated
	assert.False(t, metrics.GetLastPongTime().IsZero())
}

func TestKeepaliveMetrics_RecordReconnect(t *testing.T) {
	metrics := NewKeepaliveMetrics()

	assert.Equal(t, uint64(0), metrics.GetReconnectCount())

	metrics.RecordReconnect()
	assert.Equal(t, uint64(1), metrics.GetReconnectCount())
}

func TestKeepaliveMetrics_ConnectionUptime(t *testing.T) {
	skipIfShort(t)
	metrics := NewKeepaliveMetrics()

	// Initial uptime should be very small
	time.Sleep(10 * time.Millisecond)
	uptime := metrics.GetConnectionUptime()
	assert.True(t, uptime >= 10*time.Millisecond)

	// Reset and verify
	time.Sleep(5 * time.Millisecond)
	metrics.ResetConnectionStart()
	time.Sleep(5 * time.Millisecond)
	newUptime := metrics.GetConnectionUptime()
	assert.True(t, newUptime < uptime)
}

func TestKeepaliveMetrics_LatencySampleLimit(t *testing.T) {
	metrics := NewKeepaliveMetrics()

	// Add more samples than the limit
	for i := 0; i < 150; i++ {
		metrics.RecordPong(time.Duration(i) * time.Millisecond)
	}

	// Should still work and not panic
	assert.Equal(t, uint64(150), metrics.GetTotalPongs())

	// Average should be based on last 100 samples (50-149)
	// Average of 50..149 = (50+149)/2 = 99.5ms
	avg := metrics.GetAverageLatency()
	assert.True(t, avg > 90*time.Millisecond && avg < 110*time.Millisecond)
}

func TestKeepaliveMetrics_ConcurrentAccess(t *testing.T) {
	skipIfShort(t)
	metrics := NewKeepaliveMetrics()
	var wg sync.WaitGroup

	// Concurrent pings
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			metrics.RecordPing()
		}()
	}

	// Concurrent pongs
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(latency time.Duration) {
			defer wg.Done()
			metrics.RecordPong(latency)
		}(time.Duration(i) * time.Millisecond)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = metrics.GetTotalPings()
			_ = metrics.GetTotalPongs()
			_ = metrics.GetAverageLatency()
			_ = metrics.GetConnectionUptime()
		}()
	}

	wg.Wait()

	assert.Equal(t, uint64(100), metrics.GetTotalPings())
	assert.Equal(t, uint64(100), metrics.GetTotalPongs())
}

// =============================================================================
// KeepaliveHandler Tests
// =============================================================================

func TestKeepaliveHandler_StartStop(t *testing.T) {
	skipIfShort(t)
	logger := zaptest.NewLogger(t).Sugar()
	config := KeepaliveConfig{
		Enabled:      true,
		PingInterval: 50 * time.Millisecond,
		PongTimeout:  100 * time.Millisecond,
	}

	handler := NewKeepaliveHandler(config, logger)

	var pingCount int32
	sendPing := func(timestamp int64) error {
		atomic.AddInt32(&pingCount, 1)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start keepalive
	handler.Start(ctx, sendPing)
	assert.True(t, handler.IsRunning())

	// Wait for a few pings
	time.Sleep(150 * time.Millisecond)

	// Stop keepalive
	handler.Stop()
	assert.False(t, handler.IsRunning())

	// Verify some pings were sent
	assert.True(t, atomic.LoadInt32(&pingCount) >= 2)
}

func TestKeepaliveHandler_DisabledDoesNotStart(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	config := KeepaliveConfig{
		Enabled:      false,
		PingInterval: 10 * time.Millisecond,
	}

	handler := NewKeepaliveHandler(config, logger)

	var pingCount int32
	sendPing := func(timestamp int64) error {
		atomic.AddInt32(&pingCount, 1)
		return nil
	}

	handler.Start(context.Background(), sendPing)

	// Should not be running
	assert.False(t, handler.IsRunning())

	time.Sleep(50 * time.Millisecond)

	// No pings should have been sent
	assert.Equal(t, int32(0), atomic.LoadInt32(&pingCount))
}

func TestKeepaliveHandler_HandlePing(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	handler := NewKeepaliveHandler(DefaultKeepaliveConfig(), logger)

	timestamp := time.Now().UnixNano()
	pingMsg := &protocol.WebSocketMessage{
		Type:      protocol.WebSocketMessage_PING,
		Timestamp: timestamp,
	}

	pongMsg := handler.HandlePing(pingMsg)

	require.NotNil(t, pongMsg)
	assert.Equal(t, protocol.WebSocketMessage_PONG, pongMsg.Type)
	assert.Equal(t, timestamp, pongMsg.Timestamp)
}

func TestKeepaliveHandler_HandlePong(t *testing.T) {
	skipIfShort(t)
	logger := zaptest.NewLogger(t).Sugar()
	handler := NewKeepaliveHandler(DefaultKeepaliveConfig(), logger)

	// Send a pong with timestamp from 100ms ago
	sentTime := time.Now().Add(-100 * time.Millisecond)
	pongMsg := &protocol.WebSocketMessage{
		Type:      protocol.WebSocketMessage_PONG,
		Timestamp: sentTime.UnixNano(),
	}

	handler.HandlePong(pongMsg)

	// Verify metrics recorded
	metrics := handler.Metrics()
	assert.Equal(t, uint64(1), metrics.GetTotalPongs())

	// Latency should be approximately 100ms
	latency := metrics.GetAverageLatency()
	assert.True(t, latency >= 90*time.Millisecond && latency <= 200*time.Millisecond)
}

func TestKeepaliveHandler_HandlePongWithoutTimestamp(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	handler := NewKeepaliveHandler(DefaultKeepaliveConfig(), logger)

	pongMsg := &protocol.WebSocketMessage{
		Type:      protocol.WebSocketMessage_PONG,
		Timestamp: 0, // No timestamp
	}

	handler.HandlePong(pongMsg)

	// Should still record pong
	metrics := handler.Metrics()
	assert.Equal(t, uint64(1), metrics.GetTotalPongs())
	assert.Equal(t, time.Duration(0), metrics.GetAverageLatency())
}

func TestKeepaliveHandler_HandleMessage(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	handler := NewKeepaliveHandler(DefaultKeepaliveConfig(), logger)

	tests := []struct {
		name        string
		msg         *protocol.WebSocketMessage
		wantReply   bool
		wantErr     bool
		wantErrMsg  string
		checkMetric func(t *testing.T, m *KeepaliveMetrics)
	}{
		{
			name: "PING returns PONG",
			msg: &protocol.WebSocketMessage{
				Type:      protocol.WebSocketMessage_PING,
				Timestamp: time.Now().UnixNano(),
			},
			wantReply: true,
			wantErr:   false,
		},
		{
			name: "PONG updates metrics",
			msg: &protocol.WebSocketMessage{
				Type:      protocol.WebSocketMessage_PONG,
				Timestamp: time.Now().Add(-50 * time.Millisecond).UnixNano(),
			},
			wantReply: false,
			wantErr:   false,
			checkMetric: func(t *testing.T, m *KeepaliveMetrics) {
				assert.True(t, m.GetTotalPongs() > 0)
			},
		},
		{
			name: "ERROR returns error",
			msg: &protocol.WebSocketMessage{
				Type: protocol.WebSocketMessage_ERROR,
				Data: []byte("connection reset"),
			},
			wantReply:  false,
			wantErr:    true,
			wantErrMsg: "websocket error: connection reset",
		},
		{
			name: "DATA is ignored",
			msg: &protocol.WebSocketMessage{
				Type: protocol.WebSocketMessage_DATA,
				Data: []byte("hello"),
			},
			wantReply: false,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reply, err := handler.HandleMessage(tt.msg)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
			} else {
				require.NoError(t, err)
			}

			if tt.wantReply {
				require.NotNil(t, reply)
				assert.Equal(t, protocol.WebSocketMessage_PONG, reply.Type)
			} else {
				assert.Nil(t, reply)
			}

			if tt.checkMetric != nil {
				tt.checkMetric(t, handler.Metrics())
			}
		})
	}
}

func TestKeepaliveHandler_CheckTimeout(t *testing.T) {
	skipIfShort(t)
	logger := zaptest.NewLogger(t).Sugar()
	config := KeepaliveConfig{
		Enabled:     true,
		PongTimeout: 50 * time.Millisecond,
	}

	handler := NewKeepaliveHandler(config, logger)

	// Initially not timed out
	assert.False(t, handler.CheckTimeout())

	// Wait past timeout
	time.Sleep(60 * time.Millisecond)
	assert.True(t, handler.CheckTimeout())

	// Simulate receiving a pong
	handler.HandlePong(&protocol.WebSocketMessage{
		Type:      protocol.WebSocketMessage_PONG,
		Timestamp: time.Now().UnixNano(),
	})

	// Should not be timed out anymore
	assert.False(t, handler.CheckTimeout())
}

func TestKeepaliveHandler_ConnectWithRetry_Success(t *testing.T) {
	skipIfShort(t)
	logger := zaptest.NewLogger(t).Sugar()
	config := KeepaliveConfig{
		ReconnectAttempts: 3,
		ReconnectBaseWait: 10 * time.Millisecond,
	}

	handler := NewKeepaliveHandler(config, logger)

	var attempts int32
	connect := func() error {
		atomic.AddInt32(&attempts, 1)
		return nil // Success on first attempt
	}

	err := handler.ConnectWithRetry(context.Background(), connect)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts))
	assert.Equal(t, uint64(1), handler.Metrics().GetReconnectCount())
}

func TestKeepaliveHandler_ConnectWithRetry_SuccessAfterFailures(t *testing.T) {
	skipIfShort(t)
	logger := zaptest.NewLogger(t).Sugar()
	config := KeepaliveConfig{
		ReconnectAttempts: 5,
		ReconnectBaseWait: 5 * time.Millisecond,
	}

	handler := NewKeepaliveHandler(config, logger)

	var attempts int32
	connect := func() error {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			return errors.New("connection failed")
		}
		return nil // Success on third attempt
	}

	err := handler.ConnectWithRetry(context.Background(), connect)
	require.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
}

func TestKeepaliveHandler_ConnectWithRetry_AllFail(t *testing.T) {
	skipIfShort(t)
	logger := zaptest.NewLogger(t).Sugar()
	config := KeepaliveConfig{
		ReconnectAttempts: 3,
		ReconnectBaseWait: 5 * time.Millisecond,
	}

	handler := NewKeepaliveHandler(config, logger)

	var attempts int32
	connect := func() error {
		atomic.AddInt32(&attempts, 1)
		return errors.New("connection failed")
	}

	err := handler.ConnectWithRetry(context.Background(), connect)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 3 reconnect attempts")
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
}

func TestKeepaliveHandler_ConnectWithRetry_ContextCanceled(t *testing.T) {
	skipIfShort(t)
	logger := zaptest.NewLogger(t).Sugar()
	config := KeepaliveConfig{
		ReconnectAttempts: 10,
		ReconnectBaseWait: 100 * time.Millisecond,
	}

	handler := NewKeepaliveHandler(config, logger)

	ctx, cancel := context.WithCancel(context.Background())

	var attempts int32
	connect := func() error {
		count := atomic.AddInt32(&attempts, 1)
		if count == 2 {
			cancel() // Cancel after second attempt
		}
		return errors.New("connection failed")
	}

	err := handler.ConnectWithRetry(ctx, connect)
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)

	// Should have stopped after context was canceled
	assert.True(t, atomic.LoadInt32(&attempts) <= 3)
}

func TestKeepaliveHandler_MultipleStartStopCycles(t *testing.T) {
	skipIfShort(t)
	logger := zaptest.NewLogger(t).Sugar()
	config := KeepaliveConfig{
		Enabled:      true,
		PingInterval: 20 * time.Millisecond,
	}

	handler := NewKeepaliveHandler(config, logger)

	sendPing := func(timestamp int64) error {
		return nil
	}

	ctx := context.Background()

	// Multiple start/stop cycles
	for i := 0; i < 3; i++ {
		handler.Start(ctx, sendPing)
		assert.True(t, handler.IsRunning())

		time.Sleep(30 * time.Millisecond)

		handler.Stop()
		assert.False(t, handler.IsRunning())
	}
}

func TestKeepaliveHandler_DoubleStartIgnored(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	config := KeepaliveConfig{
		Enabled:      true,
		PingInterval: 50 * time.Millisecond,
	}

	handler := NewKeepaliveHandler(config, logger)

	var pingCount int32
	sendPing := func(timestamp int64) error {
		atomic.AddInt32(&pingCount, 1)
		return nil
	}

	ctx := context.Background()

	// Start twice
	handler.Start(ctx, sendPing)
	handler.Start(ctx, sendPing)

	time.Sleep(75 * time.Millisecond)

	handler.Stop()

	// Should only have one set of pings (not doubled)
	count := atomic.LoadInt32(&pingCount)
	assert.True(t, count >= 1 && count <= 3)
}

func TestKeepaliveHandler_DoubleStopSafe(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	handler := NewKeepaliveHandler(DefaultKeepaliveConfig(), logger)

	sendPing := func(timestamp int64) error {
		return nil
	}

	handler.Start(context.Background(), sendPing)
	handler.Stop()
	handler.Stop() // Should not panic

	assert.False(t, handler.IsRunning())
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestKeepaliveHandler_Integration_PingPongFlow(t *testing.T) {
	skipIfShort(t)
	logger := zaptest.NewLogger(t).Sugar()
	config := KeepaliveConfig{
		Enabled:      true,
		PingInterval: 20 * time.Millisecond,
		PongTimeout:  100 * time.Millisecond,
	}

	handler := NewKeepaliveHandler(config, logger)

	// Channel to receive pings
	pingCh := make(chan int64, 10)

	sendPing := func(timestamp int64) error {
		pingCh <- timestamp
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler.Start(ctx, sendPing)

	// Simulate receiving pongs for sent pings
	go func() {
		for timestamp := range pingCh {
			// Small delay to simulate network latency
			time.Sleep(5 * time.Millisecond)
			handler.HandlePong(&protocol.WebSocketMessage{
				Type:      protocol.WebSocketMessage_PONG,
				Timestamp: timestamp,
			})
		}
	}()

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)

	handler.Stop()
	// Wait for keepalive loop to exit before closing channel
	time.Sleep(50 * time.Millisecond)
	close(pingCh)

	// Verify metrics
	metrics := handler.Metrics()
	assert.True(t, metrics.GetTotalPings() >= 3)
	assert.True(t, metrics.GetTotalPongs() >= 2)
	assert.True(t, metrics.GetAverageLatency() > 0)
}
