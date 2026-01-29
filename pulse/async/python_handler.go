package async

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/errors"
)

// PythonScriptHandler executes Python scripts stored in attestations
// It sends the script code to the Python plugin via /api/python/execute
type PythonScriptHandler struct {
	pythonURL  string // Base URL for Python plugin (e.g., "http://localhost:8080")
	httpClient *http.Client
	logger     *zap.SugaredLogger
}

// NewPythonScriptHandler creates a new Python script handler
func NewPythonScriptHandler(pythonURL string, logger *zap.SugaredLogger) *PythonScriptHandler {
	return &PythonScriptHandler{
		pythonURL: pythonURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Allow up to 60s for script execution
		},
		logger: logger,
	}
}

// Name returns the handler identifier
func (h *PythonScriptHandler) Name() string {
	return "python.script"
}

// PythonScriptPayload represents the job payload for Python script execution
type PythonScriptPayload struct {
	ScriptCode string `json:"script_code"` // Python code to execute
	ScriptType string `json:"script_type"` // Type of script (csv, webscraper, etc.)
}

// PythonExecuteRequest is the request format for /api/python/execute
type PythonExecuteRequest struct {
	Code             string `json:"code"`
	CaptureVariables bool   `json:"capture_variables"`
}

// PythonExecuteResponse is the response format from /api/python/execute
type PythonExecuteResponse struct {
	Success bool                   `json:"success"`
	Result  interface{}            `json:"result,omitempty"`
	Stdout  string                 `json:"stdout,omitempty"`
	Stderr  string                 `json:"stderr,omitempty"`
	Error   string                 `json:"error,omitempty"`
	Vars    map[string]interface{} `json:"vars,omitempty"`
}

// Execute runs the Python script from the job payload
func (h *PythonScriptHandler) Execute(ctx context.Context, job *Job) error {
	// Parse job payload
	var payload PythonScriptPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return errors.Wrap(err, "invalid payload: failed to unmarshal")
	}

	// Validate script code is present
	if payload.ScriptCode == "" {
		return errors.New("invalid payload: script_code is required")
	}

	h.logger.Infow("Executing Python script",
		"job_id", job.ID,
		"script_type", payload.ScriptType)

	// Execute the user's script as-is (no payload injection)
	// The script is self-contained and runs periodically
	wrappedCode := payload.ScriptCode

	// Prepare Python execution request
	execReq := PythonExecuteRequest{
		Code:             wrappedCode,
		CaptureVariables: false, // Don't need variable capture for script execution
	}

	reqBody, err := json.Marshal(execReq)
	if err != nil {
		return errors.Wrap(err, "failed to marshal execute request")
	}

	// Send request to Python plugin
	url := fmt.Sprintf("%s/api/python/execute", h.pythonURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return errors.Wrap(err, "failed to create HTTP request")
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return errors.Wrap(err, "failed to execute Python script (HTTP request failed)")
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "failed to read response body")
	}

	// Parse response
	var execResp PythonExecuteResponse
	if err := json.Unmarshal(body, &execResp); err != nil {
		return errors.Wrapf(err, "failed to parse response: %s", string(body))
	}

	// Check for execution errors
	if !execResp.Success {
		errorMsg := execResp.Error
		if errorMsg == "" {
			errorMsg = "Python script execution failed (no error message)"
		}

		// Include stderr if present
		if execResp.Stderr != "" {
			errorMsg = fmt.Sprintf("%s\n\nStderr:\n%s", errorMsg, execResp.Stderr)
		}

		return errors.Newf("Python script execution failed: %s", errorMsg)
	}

	h.logger.Infow("Python script executed successfully",
		"job_id", job.ID,
		"script_type", payload.ScriptType,
		"stdout_length", len(execResp.Stdout))

	return nil
}
