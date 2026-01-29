package async

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestPythonScriptHandler_Name verifies the handler reports correct name
func TestPythonScriptHandler_Name(t *testing.T) {
	handler := NewPythonScriptHandler("http://localhost:8080", zap.NewNop().Sugar())
	assert.Equal(t, "python.script", handler.Name())
}

// TestPythonScriptHandler_Execute_Success verifies successful Python execution
func TestPythonScriptHandler_Execute_Success(t *testing.T) {
	// Create mock Python plugin server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "/api/python/execute", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		// Parse request body
		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		// Verify code is present
		code, ok := req["code"].(string)
		assert.True(t, ok, "code field should be present")
		assert.Contains(t, code, "print('test')")

		// Return success response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"result":  "OK",
			"stdout":  "test output\n",
			"stderr":  "",
		})
	}))
	defer server.Close()

	handler := NewPythonScriptHandler(server.URL, zap.NewNop().Sugar())

	// Create job with Python script in payload
	payload := map[string]interface{}{
		"script_code": "print('test')",
		"script_type": "git",
		"input":       []string{"https://github.com/test/repo"},
	}
	payloadJSON, _ := json.Marshal(payload)

	job := &Job{
		ID:      "test-job-123",
		Payload: payloadJSON,
	}

	// Execute
	err := handler.Execute(context.Background(), job)

	// Verify success
	assert.NoError(t, err)
}

// TestPythonScriptHandler_Execute_PythonError verifies Python execution errors are handled
func TestPythonScriptHandler_Execute_PythonError(t *testing.T) {
	// Create mock server that returns Python error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "NameError: name 'undefined_var' is not defined",
			"stderr":  "Traceback...",
		})
	}))
	defer server.Close()

	handler := NewPythonScriptHandler(server.URL, zap.NewNop().Sugar())

	payload := map[string]interface{}{
		"script_code": "print(undefined_var)",
		"script_type": "test",
		"input":       []string{},
	}
	payloadJSON, _ := json.Marshal(payload)

	job := &Job{
		ID:      "test-job-456",
		Payload: payloadJSON,
	}

	// Execute
	err := handler.Execute(context.Background(), job)

	// Verify error is returned
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NameError")
}

// TestPythonScriptHandler_Execute_InvalidPayload verifies invalid payloads are rejected
func TestPythonScriptHandler_Execute_InvalidPayload(t *testing.T) {
	handler := NewPythonScriptHandler("http://localhost:8080", zap.NewNop().Sugar())

	// Job with invalid JSON payload
	job := &Job{
		ID:      "test-job-789",
		Payload: []byte("not valid json"),
	}

	// Execute
	err := handler.Execute(context.Background(), job)

	// Verify error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid payload")
}

// TestPythonScriptHandler_Execute_MissingScriptCode verifies missing script_code is detected
func TestPythonScriptHandler_Execute_MissingScriptCode(t *testing.T) {
	handler := NewPythonScriptHandler("http://localhost:8080", zap.NewNop().Sugar())

	// Payload without script_code
	payload := map[string]interface{}{
		"script_type": "test",
		"input":       []string{},
	}
	payloadJSON, _ := json.Marshal(payload)

	job := &Job{
		ID:      "test-job-abc",
		Payload: payloadJSON,
	}

	// Execute
	err := handler.Execute(context.Background(), job)

	// Verify error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "script_code")
}

// TestPythonScriptHandler_Execute_ServerUnavailable verifies connection errors are handled
func TestPythonScriptHandler_Execute_ServerUnavailable(t *testing.T) {
	// Use invalid URL to simulate connection error
	handler := NewPythonScriptHandler("http://invalid-host-12345:9999", zap.NewNop().Sugar())

	payload := map[string]interface{}{
		"script_code": "print('test')",
		"script_type": "test",
		"input":       []string{},
	}
	payloadJSON, _ := json.Marshal(payload)

	job := &Job{
		ID:      "test-job-def",
		Payload: payloadJSON,
	}

	// Execute with short timeout
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately to fail fast

	err := handler.Execute(ctx, job)

	// Verify error
	assert.Error(t, err)
}
