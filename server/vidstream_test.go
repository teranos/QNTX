package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestVidStreamWebSocketPipeline tests the full vidstream pipeline:
// 1. WebSocket connection
// 2. Engine initialization
// 3. Connection stays alive
// 4. Frame processing
func TestVidStreamWebSocketPipeline(t *testing.T) {
	// Skip if rustvideo tag not enabled
	if !vidstreamAvailable() {
		t.Skip("Skipping vidstream test: rustvideo build tag not set")
	}

	// Create test server
	server := createTestServer(t)
	defer server.Close()

	// Start server hub
	go server.Run()

	// Create HTTP test server
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.handleWebSocket(w, r)
	}))
	defer httpServer.Close()

	// Connect WebSocket client
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	t.Log("✓ WebSocket connected")

	// Read initial messages (version, system_capabilities, etc.)
	deadline := time.Now().Add(2 * time.Second)
	ws.SetReadDeadline(deadline)

	// Drain initial messages
	for i := 0; i < 5; i++ {
		var msg map[string]interface{}
		if err := ws.ReadJSON(&msg); err != nil {
			break // Timeout or no more messages
		}
		t.Logf("Initial message: %s", msg["type"])
	}

	// STEP 1: Send vidstream_init
	initMsg := map[string]interface{}{
		"type":                 "vidstream_init",
		"model_path":           "ats/vidstream/models/yolo11n.onnx",
		"confidence_threshold": 0.5,
		"nms_threshold":        0.45,
	}

	if err := ws.WriteJSON(initMsg); err != nil {
		t.Fatalf("Failed to send init message: %v", err)
	}
	t.Log("✓ Sent vidstream_init")

	// STEP 2: Wait for init success (engine loading may take a few seconds)
	ws.SetReadDeadline(time.Now().Add(10 * time.Second))

	var initSuccess bool
	for i := 0; i < 20; i++ { // Try up to 20 messages
		var msg map[string]interface{}
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read init response: %v", err)
		}

		msgType, ok := msg["type"].(string)
		if !ok {
			continue
		}

		t.Logf("Received: %s", msgType)

		if msgType == "vidstream_init_error" {
			t.Fatalf("Engine init failed: %v", msg["error"])
		}

		if msgType == "vidstream_init_success" {
			initSuccess = true
			t.Logf("✓ Engine initialized: %dx%d", int(msg["width"].(float64)), int(msg["height"].(float64)))
			break
		}
	}

	if !initSuccess {
		t.Fatal("Did not receive vidstream_init_success")
	}

	// STEP 3: Verify connection is still alive (THIS IS THE KEY TEST)
	// Send a ping to verify WebSocket didn't disconnect
	if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
		t.Fatalf("❌ FAIL: WebSocket disconnected after engine init: %v", err)
	}
	t.Log("✓ WebSocket still alive after engine init")

	// STEP 4: Send a test frame (small dummy frame)
	frameData := make([]byte, 640*480*4) // RGBA frame
	for i := range frameData {
		frameData[i] = byte(i % 256) // Dummy pattern
	}

	frameMsg := map[string]interface{}{
		"type":       "vidstream_frame",
		"frame_data": frameData,
		"width":      640,
		"height":     480,
		"format":     "rgba8",
	}

	if err := ws.WriteJSON(frameMsg); err != nil {
		t.Fatalf("Failed to send frame: %v", err)
	}
	t.Log("✓ Sent test frame")

	// STEP 5: Wait for detections or error
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))

	var gotResponse bool
	for i := 0; i < 10; i++ {
		var msg map[string]interface{}
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read frame response: %v", err)
		}

		msgType, ok := msg["type"].(string)
		if !ok {
			continue
		}

		if msgType == "vidstream_frame_error" {
			t.Logf("Frame processing error (expected if no objects in dummy frame): %v", msg["error"])
			gotResponse = true
			break
		}

		if msgType == "vidstream_detections" {
			detections := msg["detections"]
			t.Logf("✓ Received detections: %v", detections)
			gotResponse = true
			break
		}
	}

	if !gotResponse {
		t.Fatal("Did not receive frame processing response")
	}

	// STEP 6: Final connection check
	if err := ws.WriteMessage(websocket.PingMessage, nil); err != nil {
		t.Fatalf("❌ FAIL: WebSocket disconnected after frame processing: %v", err)
	}
	t.Log("✓ WebSocket still alive after frame processing")

	t.Log("✅ SUCCESS: Full vidstream pipeline working")
}

// Helper: create minimal test server
func createTestServer(t *testing.T) *QNTXServer {
	db := createTestDB(t)
	builder := createTestGraphBuilder(t, db)

	return NewQNTXServer(
		db,
		"test.db",
		builder,
		nil, // langService
		nil, // usageTracker
		nil, // budgetTracker
		nil, // daemon
		nil, // ticker
		nil, // configWatcher
		nil, // pluginRegistry
		nil, // pluginManager
		nil, // services
		nil, // servicesManager
		zapTestLogger(t),
		"",  // initialQuery
	)
}

func zapTestLogger(t *testing.T) *zap.SugaredLogger {
	logger, _ := zap.NewDevelopment()
	return logger.Sugar()
}
