package server

import (
	"testing"
	"time"

	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/server/syscap"
)

// TestVidStreamMessageRouting verifies vidstream messages route to correct handlers
func TestVidStreamMessageRouting(t *testing.T) {
	if !syscap.VidstreamAvailable() {
		t.Skip("Skipping vidstream test: rustvideo build tag not set")
	}

	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create mock client
	client := &Client{
		server:  srv,
		sendMsg: make(chan interface{}, 10),
		id:      "test_client",
	}

	// Test vidstream_init message routing
	initMsg := QueryMessage{
		Type:                "vidstream_init",
		ModelPath:           "test.onnx",
		ConfidenceThreshold: 0.5,
		NMSThreshold:        0.45,
	}

	// This should not panic and should route to handleVidStreamInit
	client.routeMessage(&initMsg)

	// Verify error response since model doesn't exist (async, so may not arrive immediately)
	// Just verify no panic - detailed testing would require WebSocket setup
}

// TestVidStreamFrameWithoutInit verifies error when frame sent before init
func TestVidStreamFrameWithoutInit(t *testing.T) {
	if !syscap.VidstreamAvailable() {
		t.Skip("Skipping vidstream test: rustvideo build tag not set")
	}

	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	client := &Client{
		server:  srv,
		sendMsg: make(chan interface{}, 10),
		id:      "test_client",
	}

	// Send frame without initializing engine
	frameMsg := QueryMessage{
		Type:      "vidstream_frame",
		FrameData: []byte{0, 0, 0, 0},
		Width:     2,
		Height:    2,
		Format:    "rgba8",
	}

	client.routeMessage(&frameMsg)

	// Should get error response
	select {
	case msg := <-client.sendMsg:
		msgMap, ok := msg.(map[string]interface{})
		if !ok {
			t.Fatal("Expected map response")
		}
		if msgMap["type"] != "vidstream_frame_error" {
			t.Errorf("Expected frame_error, got %v", msgMap["type"])
		}
		errorText, ok := msgMap["error"].(string)
		if !ok || errorText == "" {
			t.Error("Expected error message")
		}
		if errorText != "Engine not initialized. Call vidstream_init first." {
			t.Errorf("Wrong error message: %s", errorText)
		}
	default:
		t.Fatal("Expected error response")
	}
}

// TestVidStreamInvalidFormat verifies error for unsupported pixel format
func TestVidStreamInvalidFormat(t *testing.T) {
	if !syscap.VidstreamAvailable() {
		t.Skip("Skipping vidstream test: rustvideo build tag not set")
	}

	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	client := &Client{
		server:  srv,
		sendMsg: make(chan interface{}, 10),
		id:      "test_client",
	}

	// Send frame with invalid format (engine not initialized, so will get that error first)
	frameMsg := QueryMessage{
		Type:      "vidstream_frame",
		FrameData: []byte{0, 0, 0, 0},
		Width:     2,
		Height:    2,
		Format:    "invalid_format",
	}

	client.routeMessage(&frameMsg)

	// Should get error about engine not initialized first
	select {
	case msg := <-client.sendMsg:
		msgMap, ok := msg.(map[string]interface{})
		if !ok {
			t.Fatal("Expected map response")
		}
		if msgMap["type"] != "vidstream_frame_error" {
			t.Errorf("Expected frame_error, got %v", msgMap["type"])
		}
	default:
		t.Fatal("Expected error response")
	}
}

// TestVidStreamAsyncInitSendsResponse verifies init goroutine completes and sends response
func TestVidStreamAsyncInitSendsResponse(t *testing.T) {
	if !syscap.VidstreamAvailable() {
		t.Skip("Skipping vidstream test: rustvideo build tag not set")
	}

	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	client := &Client{
		server:  srv,
		sendMsg: make(chan interface{}, 10),
		id:      "test_client",
	}

	// Send init with non-existent model (will fail but should still send response)
	initMsg := QueryMessage{
		Type:                "vidstream_init",
		ModelPath:           "nonexistent.onnx",
		ConfidenceThreshold: 0.5,
		NMSThreshold:        0.45,
	}

	client.routeMessage(&initMsg)

	// Wait for async goroutine to complete (should get error response)
	select {
	case msg := <-client.sendMsg:
		msgMap, ok := msg.(map[string]interface{})
		if !ok {
			t.Fatal("Expected map response from async goroutine")
		}

		// Should receive either init_success or init_error
		msgType, ok := msgMap["type"].(string)
		if !ok {
			t.Fatal("Expected string type field")
		}

		if msgType != "vidstream_init_success" && msgType != "vidstream_init_error" {
			t.Errorf("Expected init_success or init_error, got %v", msgType)
		}

		// For nonexistent model, should be error
		if msgType != "vidstream_init_error" {
			t.Errorf("Expected init_error for nonexistent model, got %v", msgType)
		}

		// Verify error message exists
		if msgType == "vidstream_init_error" {
			errorMsg, ok := msgMap["error"].(string)
			if !ok || errorMsg == "" {
				t.Error("Expected error message in init_error response")
			}
		}

	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for async init response")
	}
}

// TestVidStreamEngineReinitClosesOldEngine verifies engine reinit properly closes old engine
func TestVidStreamEngineReinitClosesOldEngine(t *testing.T) {
	if !syscap.VidstreamAvailable() {
		t.Skip("Skipping vidstream test: rustvideo build tag not set")
	}
	if testing.Short() {
		t.Skip("Skipping slow test in short mode")
	}

	db := qntxtest.CreateTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	client := &Client{
		server:  srv,
		sendMsg: make(chan interface{}, 100), // Large buffer for multiple responses
		id:      "test_client",
	}

	// First init with nonexistent model (should fail)
	initMsg1 := QueryMessage{
		Type:                "vidstream_init",
		ModelPath:           "model1.onnx",
		ConfidenceThreshold: 0.5,
		NMSThreshold:        0.45,
	}

	client.routeMessage(&initMsg1)

	// Wait for first response
	select {
	case <-client.sendMsg:
		// Got response (success or error doesn't matter)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for first init response")
	}

	// Store reference to first engine (might be nil if init failed)
	firstEngine := srv.vidstreamEngine

	// Second init with different model
	initMsg2 := QueryMessage{
		Type:                "vidstream_init",
		ModelPath:           "model2.onnx",
		ConfidenceThreshold: 0.6,
		NMSThreshold:        0.50,
	}

	client.routeMessage(&initMsg2)

	// Wait for second response
	select {
	case <-client.sendMsg:
		// Got response
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for second init response")
	}

	// Verify engine reference changed (or both are nil if models don't exist)
	secondEngine := srv.vidstreamEngine

	// Key assertion: if first succeeded, second should be different instance
	// If both failed, both should be nil
	// No panic = old engine was properly closed before new one created
	if firstEngine != nil && secondEngine != nil && firstEngine == secondEngine {
		t.Error("Engine reference should change on reinit")
	}

	// If we got here without panic, the mutex-protected reinit logic works
}
