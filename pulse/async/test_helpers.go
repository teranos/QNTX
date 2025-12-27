package async

import "encoding/json"

// createTestJob is a shared helper for all tests to create jobs with generic payloads
func createTestJob(handlerName, source string, totalOps int, estimatedCost float64) (*Job, error) {
	payload := map[string]interface{}{
		"source": source,
		"actor":  "test-system",
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return NewJobWithPayload(handlerName, source, payloadJSON, totalOps, estimatedCost, "test-system")
}
