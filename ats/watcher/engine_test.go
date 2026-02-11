//go:build qntxwasm

package watcher_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

func TestEngine_StartStop(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, "http://localhost:877", logger)

	// Start engine
	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stop should not panic
	engine.Stop()
}

func TestEngine_LoadWatchers(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, "http://localhost:877", logger)

	// Create some watchers
	store := storage.NewWatcherStore(db)

	enabledWatcher := &storage.Watcher{
		ID:                "enabled-1",
		Name:              "Enabled Watcher",
		ActionType:        storage.ActionTypePython,
		ActionData:        "print('hello')",
		MaxFiresPerMinute: 105,
		Enabled:           true,
		Filter: types.AxFilter{
			Subjects: []string{"user:123"},
		},
	}

	disabledWatcher := &storage.Watcher{
		ID:                "disabled-1",
		Name:              "Disabled Watcher",
		ActionType:        storage.ActionTypePython,
		ActionData:        "print('world')",
		MaxFiresPerMinute: 105,
		Enabled:           false,
	}

	axQueryWatcher := &storage.Watcher{
		ID:                "ax-query-1",
		Name:              "AX Query Watcher",
		ActionType:        storage.ActionTypePython,
		ActionData:        "print('ax')",
		MaxFiresPerMinute: 105,
		Enabled:           true,
		AxQuery:           "subjects=user:456 predicates=login",
	}

	if err := store.Create(context.Background(),enabledWatcher); err != nil {
		t.Fatalf("Create enabled watcher failed: %v", err)
	}
	if err := store.Create(context.Background(),disabledWatcher); err != nil {
		t.Fatalf("Create disabled watcher failed: %v", err)
	}
	if err := store.Create(context.Background(),axQueryWatcher); err != nil {
		t.Fatalf("Create ax query watcher failed: %v", err)
	}

	// Start engine (loads watchers)
	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Check that only enabled watchers are loaded
	if w, exists := engine.GetWatcher("enabled-1"); !exists {
		t.Error("Enabled watcher not loaded")
	} else if len(w.Filter.Subjects) != 1 || w.Filter.Subjects[0] != "user:123" {
		t.Errorf("Filter not preserved: %+v", w.Filter)
	}

	if _, exists := engine.GetWatcher("disabled-1"); exists {
		t.Error("Disabled watcher should not be loaded")
	}

	// Check AX query was parsed into filter
	// Note: The parser returns raw tokens in uppercase format
	if w, exists := engine.GetWatcher("ax-query-1"); !exists {
		t.Error("AX query watcher not loaded")
	} else {
		// Parser returns the raw query tokens, not parsed individual fields
		if len(w.Filter.Subjects) == 0 {
			t.Errorf("AX query not loaded into filter: %+v", w.Filter)
		}
	}
}

func TestEngine_MatchesFilter(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, "http://localhost:877", logger)

	// Create watcher with specific filter
	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "filter-test",
		Name:              "Filter Test",
		ActionType:        storage.ActionTypePython,
		ActionData:        "pass",
		MaxFiresPerMinute: 105,
		Enabled:           true,
		Filter: types.AxFilter{
			Subjects:   []string{"user:123", "user:456"},
			Predicates: []string{"login", "logout"},
			Contexts:   []string{"web"},
		},
	}
	if err := store.Create(context.Background(),w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Track matches
	var matches []string
	engine.SetBroadcastCallback(func(watcherID string, attestation *types.As) {
		matches = append(matches, attestation.ID)
	})

	testCases := []struct {
		name    string
		as      *types.As
		shouldMatch bool
	}{
		{
			name: "exact match",
			as: &types.As{
				ID:         "match-1",
				Subjects:   []string{"user:123"},
				Predicates: []string{"login"},
				Contexts:   []string{"web"},
			},
			shouldMatch: true,
		},
		{
			name: "overlapping subjects",
			as: &types.As{
				ID:         "match-2",
				Subjects:   []string{"user:456", "user:789"},
				Predicates: []string{"logout"},
				Contexts:   []string{"web"},
			},
			shouldMatch: true,
		},
		{
			name: "no subject match",
			as: &types.As{
				ID:         "no-match-1",
				Subjects:   []string{"user:999"},
				Predicates: []string{"login"},
				Contexts:   []string{"web"},
			},
			shouldMatch: false,
		},
		{
			name: "no predicate match",
			as: &types.As{
				ID:         "no-match-2",
				Subjects:   []string{"user:123"},
				Predicates: []string{"update"},
				Contexts:   []string{"web"},
			},
			shouldMatch: false,
		},
		{
			name: "no context match",
			as: &types.As{
				ID:         "no-match-3",
				Subjects:   []string{"user:123"},
				Predicates: []string{"login"},
				Contexts:   []string{"mobile"},
			},
			shouldMatch: false,
		},
		{
			name: "empty attestation fields",
			as: &types.As{
				ID:         "no-match-4",
				Subjects:   []string{},
				Predicates: []string{},
				Contexts:   []string{},
			},
			shouldMatch: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			matches = matches[:0] // Clear matches
			engine.OnAttestationCreated(tc.as)

			// Give broadcast callback time to execute
			time.Sleep(10 * time.Millisecond)

			if tc.shouldMatch && len(matches) == 0 {
				t.Errorf("Expected match for %s but got none", tc.as.ID)
			}
			if !tc.shouldMatch && len(matches) > 0 {
				t.Errorf("Expected no match for %s but got matches: %v", tc.as.ID, matches)
			}
		})
	}
}

func TestEngine_RateLimiting(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, "http://localhost:877", logger)

	// Create watcher with low rate limit (60/min = 1/sec)
	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "rate-limit-test",
		Name:              "Rate Limited",
		ActionType:        storage.ActionTypePython,
		ActionData:        "pass",
		MaxFiresPerMinute: 60, // 1 per second
		Enabled:           true,
		Filter: types.AxFilter{}, // Match all
	}
	if err := store.Create(context.Background(),w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	// Mock Python endpoint
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Point engine to mock server
	engine = watcher.NewEngine(db, server.URL, logger)
	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Fire multiple attestations quickly
	for i := 0; i < 5; i++ {
		engine.OnAttestationCreated(&types.As{
			ID:         "test-" + string(rune('0'+i)),
			Subjects:   []string{"test"},
			Predicates: []string{"test"},
		})
	}

	// Wait for actions to execute
	time.Sleep(100 * time.Millisecond)

	// Should only have executed once due to rate limiting
	if callCount != 1 {
		t.Errorf("Expected 1 execution due to rate limiting, got %d", callCount)
	}
}

func TestEngine_ExecutePython(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()

	// Mock Python endpoint
	var receivedCode string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/python/execute" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		receivedCode = req["content"].(string)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	engine := watcher.NewEngine(db, server.URL, logger)

	// Create Python watcher
	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "python-test",
		Name:              "Python Test",
		ActionType:        storage.ActionTypePython,
		ActionData:        "print(attestation['id'])",
		MaxFiresPerMinute: 105,
		Enabled:           true,
		Filter: types.AxFilter{}, // Match all
	}
	if err := store.Create(context.Background(),w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Trigger with attestation
	attestation := &types.As{
		ID:         "test-attestation",
		Subjects:   []string{"user:123"},
		Predicates: []string{"action"},
	}
	engine.OnAttestationCreated(attestation)

	// Wait for execution
	time.Sleep(100 * time.Millisecond)

	// Verify attestation was injected into Python code
	if receivedCode == "" {
		t.Fatal("Python endpoint was not called")
	}
	if !contains(receivedCode, "test-attestation") {
		t.Error("Attestation ID not found in injected code")
	}
	if !contains(receivedCode, "print(attestation['id'])") {
		t.Error("User code not preserved")
	}
}

func TestEngine_ExecuteWebhook(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()

	// Mock webhook endpoint
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	engine := watcher.NewEngine(db, "http://localhost:877", logger)

	// Create webhook watcher
	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "webhook-test",
		Name:              "Webhook Test",
		ActionType:        storage.ActionTypeWebhook,
		ActionData:        server.URL + "/webhook",
		MaxFiresPerMinute: 105,
		Enabled:           true,
		Filter: types.AxFilter{}, // Match all
	}
	if err := store.Create(context.Background(),w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Trigger with attestation
	attestation := &types.As{
		ID:         "webhook-attestation",
		Subjects:   []string{"user:456"},
		Predicates: []string{"webhook-test"},
	}
	engine.OnAttestationCreated(attestation)

	// Wait for execution
	time.Sleep(100 * time.Millisecond)

	// Verify webhook received correct data
	if receivedBody == nil {
		t.Fatal("Webhook was not called")
	}
	if receivedBody["watcher_id"] != "webhook-test" {
		t.Errorf("Wrong watcher_id: %v", receivedBody["watcher_id"])
	}
	attestationData := receivedBody["attestation"].(map[string]interface{})
	if attestationData["id"] != "webhook-attestation" {
		t.Errorf("Wrong attestation ID: %v", attestationData["id"])
	}
}


func TestEngine_QueryHistoricalMatches(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, "http://localhost:877", logger)

	// Insert some historical attestations
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		subjectsJSON, _ := json.Marshal([]string{"user:" + string(rune('0'+i))})
		predicatesJSON, _ := json.Marshal([]string{"login"})
		contextsJSON, _ := json.Marshal([]string{"web"})
		actorsJSON, _ := json.Marshal([]string{"system"})

		_, err := db.ExecContext(ctx, `
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			"historical-"+string(rune('0'+i)),
			subjectsJSON,
			predicatesJSON,
			contextsJSON,
			actorsJSON,
			time.Now().Format(time.RFC3339),
			"test",
		)
		if err != nil {
			t.Fatalf("Failed to insert attestation: %v", err)
		}
	}

	// Create watcher that matches some attestations
	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "historical-test",
		Name:              "Historical Test",
		ActionType:        storage.ActionTypePython,
		ActionData:        "pass",
		MaxFiresPerMinute: 105,
		Enabled:           true,
		Filter: types.AxFilter{
			Predicates: []string{"login"},
		},
	}
	if err := store.Create(context.Background(),w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Track historical matches
	var matches []string
	engine.SetBroadcastCallback(func(watcherID string, attestation *types.As) {
		matches = append(matches, attestation.ID)
	})

	// Query historical matches
	err := engine.QueryHistoricalMatches("historical-test")
	if err != nil {
		t.Fatalf("QueryHistoricalMatches failed: %v", err)
	}

	// Should have matched all 5 historical attestations
	if len(matches) != 5 {
		t.Errorf("Expected 5 historical matches, got %d", len(matches))
	}
}

func TestEngine_TimeFilters(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, "http://localhost:877", logger)

	now := time.Now()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	// Create watcher with time filter
	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "time-filter-test",
		Name:              "Time Filter Test",
		ActionType:        storage.ActionTypePython,
		ActionData:        "pass",
		MaxFiresPerMinute: 105,
		Enabled:           true,
		Filter: types.AxFilter{
			TimeStart: &past,
			TimeEnd:   &future,
		},
	}
	if err := store.Create(context.Background(),w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Track matches
	var matches []string
	engine.SetBroadcastCallback(func(watcherID string, attestation *types.As) {
		matches = append(matches, attestation.ID)
	})

	testCases := []struct {
		name        string
		timestamp   time.Time
		shouldMatch bool
	}{
		{"before range", past.Add(-1 * time.Hour), false},
		{"start of range", past, true},
		{"in range", now, true},
		{"end of range", future, true},
		{"after range", future.Add(1 * time.Hour), false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			matches = matches[:0] // Clear
			engine.OnAttestationCreated(&types.As{
				ID:         "time-" + tc.name,
				Subjects:   []string{"test"},
				Predicates: []string{"test"},
				Timestamp:  tc.timestamp,
			})

			time.Sleep(10 * time.Millisecond)

			if tc.shouldMatch && len(matches) == 0 {
				t.Error("Expected match but got none")
			}
			if !tc.shouldMatch && len(matches) > 0 {
				t.Error("Expected no match but got matches")
			}
		})
	}
}

func TestEngine_ZeroMaxFiresPerMinute(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()

	// Mock endpoint
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	engine := watcher.NewEngine(db, server.URL, logger)

	// Create watcher with MaxFiresPerMinute = 0 (should mean no fires per QNTX LAW)
	store := storage.NewWatcherStore(db)
	w := &storage.Watcher{
		ID:                "zero-rate-test",
		Name:              "Zero Rate Test",
		ActionType:        storage.ActionTypePython,
		ActionData:        "pass",
		MaxFiresPerMinute: 0, // Zero means zero - no fires allowed
		Enabled:           true,
		Filter: types.AxFilter{}, // Match all
	}
	if err := store.Create(context.Background(),w); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Try to trigger multiple times
	for i := 0; i < 3; i++ {
		engine.OnAttestationCreated(&types.As{
			ID:         "zero-test-" + string(rune('0'+i)),
			Subjects:   []string{"test"},
			Predicates: []string{"test"},
		})
	}

	// Wait for potential execution
	time.Sleep(100 * time.Millisecond)

	// Should NOT have executed (zero means zero)
	if callCount != 0 {
		t.Errorf("Expected 0 executions (MaxFiresPerMinute=0), got %d", callCount)
	}
}

func TestEngine_NoSharedMutation(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()

	// Track received attestations
	var mu sync.Mutex
	receivedAttestations := make([]*types.As, 0)

	// Mock endpoint that captures attestations
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		if attestationData, ok := body["attestation"]; ok {
			attestationJSON, _ := json.Marshal(attestationData)
			var as types.As
			json.Unmarshal(attestationJSON, &as)

			mu.Lock()
			receivedAttestations = append(receivedAttestations, &as)
			mu.Unlock()
		}

		// Simulate slow processing to ensure goroutines run concurrently
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	engine := watcher.NewEngine(db, "http://localhost:877", logger)
	store := storage.NewWatcherStore(db)

	// Create multiple webhook watchers
	for i := 0; i < 3; i++ {
		w := &storage.Watcher{
			ID:                fmt.Sprintf("webhook-%d", i),
			Name:              fmt.Sprintf("Webhook %d", i),
			ActionType:        storage.ActionTypeWebhook,
			ActionData:        server.URL,
			MaxFiresPerMinute: 105,
			Enabled:           true,
			Filter:            types.AxFilter{}, // Match all
		}
		if err := store.Create(context.Background(),w); err != nil {
			t.Fatalf("Create watcher %d failed: %v", i, err)
		}
	}

	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Create attestation
	originalAttestation := &types.As{
		ID:         "test-mutation",
		Subjects:   []string{"original-subject"},
		Predicates: []string{"original-predicate"},
		Contexts:   []string{"original-context"},
		Actors:     []string{"original-actor"},
	}

	// Trigger multiple watchers with same attestation
	engine.OnAttestationCreated(originalAttestation)

	// Wait for all webhooks to complete
	time.Sleep(200 * time.Millisecond)

	// Verify we received 3 attestations
	if len(receivedAttestations) != 3 {
		t.Fatalf("Expected 3 attestations, got %d", len(receivedAttestations))
	}

	// Verify each received attestation has the original values (no mutation)
	for i, as := range receivedAttestations {
		if as.ID != "test-mutation" {
			t.Errorf("Attestation %d: ID mismatch: %s", i, as.ID)
		}
		if len(as.Subjects) != 1 || as.Subjects[0] != "original-subject" {
			t.Errorf("Attestation %d: Subjects mutated: %v", i, as.Subjects)
		}
		if len(as.Predicates) != 1 || as.Predicates[0] != "original-predicate" {
			t.Errorf("Attestation %d: Predicates mutated: %v", i, as.Predicates)
		}
		if len(as.Contexts) != 1 || as.Contexts[0] != "original-context" {
			t.Errorf("Attestation %d: Contexts mutated: %v", i, as.Contexts)
		}
		if len(as.Actors) != 1 || as.Actors[0] != "original-actor" {
			t.Errorf("Attestation %d: Actors mutated: %v", i, as.Actors)
		}
	}

	// Also verify the original attestation wasn't modified
	if originalAttestation.Subjects[0] != "original-subject" {
		t.Error("Original attestation was mutated!")
	}
}

func TestEngine_GetParseError_SuccessfulWatcher(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, "http://localhost:877", logger)

	// Create watcher with valid AX query
	store := storage.NewWatcherStore(db)
	validWatcher := &storage.Watcher{
		ID:                "valid-query-watcher",
		Name:              "Valid Query Watcher",
		ActionType:        storage.ActionTypePython,
		ActionData:        "print('ok')",
		MaxFiresPerMinute: 105,
		Enabled:           true,
		AxQuery:           "ANNA is author",
	}

	if err := store.Create(context.Background(), validWatcher); err != nil {
		t.Fatalf("Create watcher failed: %v", err)
	}

	// Start engine (loads watchers and parses queries)
	if err := engine.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer engine.Stop()

	// Verify watcher was loaded successfully
	if _, exists := engine.GetWatcher("valid-query-watcher"); !exists {
		t.Fatal("Valid watcher not loaded")
	}

	// GetParseError should return nil for successful watcher
	parseErr := engine.GetParseError("valid-query-watcher")
	if parseErr != nil {
		t.Errorf("Expected nil parse error for successful watcher, got: %v", parseErr)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || (len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || contains(s[1:], substr))))
}