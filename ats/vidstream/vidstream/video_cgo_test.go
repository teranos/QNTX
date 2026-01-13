//go:build cgo && rustvideo

package vidstream

import (
	"os"
	"testing"
)

// getTestConfig returns a Config for testing with optional model path from environment.
// CI downloads YOLO11n model and sets QNTX_VIDSTREAM_TEST_MODEL to enable inference mode.
// Without model, tests run in stub mode verifying FFI bindings and basic functionality.
func getTestConfig(t *testing.T) Config {
	cfg := Config{
		ConfidenceThreshold: 0.5,
		NMSThreshold:        0.4,
		InputWidth:          640,
		InputHeight:         640,
		NumThreads:          1,
		UseGPU:              false,
	}

	// Check for test model path in environment
	if modelPath := os.Getenv("QNTX_VIDSTREAM_TEST_MODEL"); modelPath != "" {
		t.Logf("Using test model: %s", modelPath)
		cfg.ModelPath = modelPath
	} else {
		t.Log("No test model specified (set QNTX_VIDSTREAM_TEST_MODEL to test with ONNX)")
	}

	return cfg
}

func TestVideoEngineLifecycle(t *testing.T) {
	// Test engine creation and cleanup
	cfg := getTestConfig(t)
	engine, err := NewVideoEngineWithConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close()

	// Should be created successfully
	if engine == nil {
		t.Fatal("Engine is nil")
	}

	// Close should be idempotent
	if err := engine.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Errorf("Second close failed: %v", err)
	}
}

func TestExpectedFrameSize(t *testing.T) {
	tests := []struct {
		name   string
		width  uint32
		height uint32
		format FrameFormat
		want   int
	}{
		{"RGB8", 640, 480, FormatRGB8, 640 * 480 * 3},
		{"RGBA8", 640, 480, FormatRGBA8, 640 * 480 * 4},
		{"Gray8", 640, 480, FormatGray8, 640 * 480},
		{"YUV420", 640, 480, FormatYUV420, 640*480 + 640*480/2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpectedFrameSize(tt.width, tt.height, tt.format)
			if got != tt.want {
				t.Errorf("ExpectedFrameSize(%d, %d, %v) = %d, want %d",
					tt.width, tt.height, tt.format, got, tt.want)
			}
		})
	}
}

func TestProcessFrameBasic(t *testing.T) {
	cfg := getTestConfig(t)
	engine, err := NewVideoEngineWithConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close()

	// Create test frame (RGB8, 640x480)
	width := uint32(640)
	height := uint32(480)
	frameSize := ExpectedFrameSize(width, height, FormatRGB8)
	frameData := make([]byte, frameSize)
	// Fill with gray color
	for i := range frameData {
		frameData[i] = 128
	}

	// Process frame
	result, err := engine.ProcessFrame(frameData, width, height, FormatRGB8, 0)
	if err != nil {
		t.Fatalf("ProcessFrame failed: %v", err)
	}

	// Validate result
	if result == nil {
		t.Fatal("Result is nil")
	}

	// Stats should be populated
	if result.Stats.FrameWidth != width {
		t.Errorf("Stats.FrameWidth = %d, want %d", result.Stats.FrameWidth, width)
	}
	if result.Stats.FrameHeight != height {
		t.Errorf("Stats.FrameHeight = %d, want %d", result.Stats.FrameHeight, height)
	}

	// Detections might be empty in stub mode (no model loaded)
	// A nil slice is valid in Go (equivalent to empty slice)
	t.Logf("Detections: %d", len(result.Detections))
}

func TestProcessFrameEmptyData(t *testing.T) {
	cfg := getTestConfig(t)
	engine, err := NewVideoEngineWithConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close()

	// Test with empty frame data (should error)
	_, err = engine.ProcessFrame(nil, 640, 480, FormatRGB8, 0)
	if err == nil {
		t.Error("Expected error for nil frame data, got nil")
	}

	_, err = engine.ProcessFrame([]byte{}, 640, 480, FormatRGB8, 0)
	if err == nil {
		t.Error("Expected error for empty frame data, got nil")
	}
}

func TestProcessFrameClosedEngine(t *testing.T) {
	cfg := getTestConfig(t)
	engine, err := NewVideoEngineWithConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	engine.Close()

	// Test with closed engine (should error)
	frameData := make([]byte, 640*480*3)
	_, err = engine.ProcessFrame(frameData, 640, 480, FormatRGB8, 0)
	if err == nil {
		t.Error("Expected error for closed engine, got nil")
	}
}

func TestInputDimensions(t *testing.T) {
	cfg := getTestConfig(t)
	engine, err := NewVideoEngineWithConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close()

	width, height := engine.InputDimensions()
	// In stub mode (no model), might return 0,0 or default values
	// Just verify it doesn't crash
	t.Logf("Input dimensions: %dx%d", width, height)
}

func TestIsReady(t *testing.T) {
	cfg := getTestConfig(t)
	engine, err := NewVideoEngineWithConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close()

	ready := engine.IsReady()
	// Without ONNX feature or without a model, should be false
	// With ONNX and model loaded, should be true
	t.Logf("Engine ready: %v", ready)

	// After close, should be false
	engine.Close()
	if engine.IsReady() {
		t.Error("Engine should not be ready after close")
	}
}

// Benchmarks

func BenchmarkProcessFrame640x480(b *testing.B) {
	cfg := Config{
		ConfidenceThreshold: 0.5,
		NMSThreshold:        0.4,
		InputWidth:          640,
		InputHeight:         640,
		NumThreads:          1,
		UseGPU:              false,
	}
	if modelPath := os.Getenv("QNTX_VIDSTREAM_TEST_MODEL"); modelPath != "" {
		cfg.ModelPath = modelPath
	}

	engine, err := NewVideoEngineWithConfig(cfg)
	if err != nil {
		b.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close()

	// Prepare test frame (RGB8, 640x480)
	width := uint32(640)
	height := uint32(480)
	frameSize := ExpectedFrameSize(width, height, FormatRGB8)
	frameData := make([]byte, frameSize)
	for i := range frameData {
		frameData[i] = 128 // Gray frame
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, err := engine.ProcessFrame(frameData, width, height, FormatRGB8, uint64(i))
		if err != nil {
			b.Fatalf("ProcessFrame failed: %v", err)
		}
		if result == nil {
			b.Fatal("Result is nil")
		}
	}
}

func BenchmarkProcessFrame1920x1080(b *testing.B) {
	cfg := Config{
		ConfidenceThreshold: 0.5,
		NMSThreshold:        0.4,
		InputWidth:          640,
		InputHeight:         640,
		NumThreads:          1,
		UseGPU:              false,
	}
	if modelPath := os.Getenv("QNTX_VIDSTREAM_TEST_MODEL"); modelPath != "" {
		cfg.ModelPath = modelPath
	}

	engine, err := NewVideoEngineWithConfig(cfg)
	if err != nil {
		b.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close()

	// Prepare test frame (RGB8, 1920x1080 - Full HD)
	width := uint32(1920)
	height := uint32(1080)
	frameSize := ExpectedFrameSize(width, height, FormatRGB8)
	frameData := make([]byte, frameSize)
	for i := range frameData {
		frameData[i] = 128 // Gray frame
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, err := engine.ProcessFrame(frameData, width, height, FormatRGB8, uint64(i))
		if err != nil {
			b.Fatalf("ProcessFrame failed: %v", err)
		}
		if result == nil {
			b.Fatal("Result is nil")
		}
	}
}

func BenchmarkProcessFrameParallel(b *testing.B) {
	cfg := Config{
		ConfidenceThreshold: 0.5,
		NMSThreshold:        0.4,
		InputWidth:          640,
		InputHeight:         640,
		NumThreads:          1,
		UseGPU:              false,
	}
	if modelPath := os.Getenv("QNTX_VIDSTREAM_TEST_MODEL"); modelPath != "" {
		cfg.ModelPath = modelPath
	}

	engine, err := NewVideoEngineWithConfig(cfg)
	if err != nil {
		b.Fatalf("Failed to create engine: %v", err)
	}
	defer engine.Close()

	// Prepare test frame (RGB8, 640x480)
	width := uint32(640)
	height := uint32(480)
	frameSize := ExpectedFrameSize(width, height, FormatRGB8)
	frameData := make([]byte, frameSize)
	for i := range frameData {
		frameData[i] = 128
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		timestamp := uint64(0)
		for pb.Next() {
			result, err := engine.ProcessFrame(frameData, width, height, FormatRGB8, timestamp)
			if err != nil {
				b.Fatalf("ProcessFrame failed: %v", err)
			}
			if result == nil {
				b.Fatal("Result is nil")
			}
			timestamp++
		}
	})
}
