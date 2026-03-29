package server

import (
	"context"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
	pb "github.com/teranos/QNTX/glyph/proto"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// mockQueryStore implements queryStore for testing.
// Returns attestations keyed by glyph context ID.
type mockQueryStore struct {
	byContext map[string][]*types.As
}

func (m *mockQueryStore) ExecuteAxQuery(_ context.Context, filter types.AxFilter) ([]*types.As, error) {
	var results []*types.As
	for _, ctx := range filter.Contexts {
		results = append(results, m.byContext[ctx]...)
	}
	return results, nil
}

// promptResultAs creates a prompt-result attestation for a glyph at a given time.
func promptResultAs(glyphID, template, response string, ts time.Time) *types.As {
	return &types.As{
		ID:         "AS-" + glyphID,
		Contexts:   []string{glyphID},
		Predicates: []string{"prompt-result"},
		Timestamp:  ts,
		Attributes: map[string]any{
			"template": template,
			"response": response,
		},
	}
}

// setupCanvasWithGlyphs creates glyphs and a composition with edges in the real DB.
func setupCanvasWithGlyphs(t *testing.T, store *glyphstorage.CanvasStore, glyphIDs []string, compID string, edges []*pb.CompositionEdge) {
	t.Helper()
	ctx := context.Background()

	for _, id := range glyphIDs {
		err := store.UpsertGlyph(ctx, &glyphstorage.CanvasGlyph{
			ID:     id,
			Symbol: "prompt",
			X:      0,
			Y:      0,
		})
		if err != nil {
			t.Fatalf("UpsertGlyph %s: %v", id, err)
		}
	}

	err := store.UpsertComposition(ctx, &glyphstorage.CanvasComposition{
		ID:    compID,
		Edges: edges,
	})
	if err != nil {
		t.Fatalf("UpsertComposition: %v", err)
	}
}

// --- Tim: happy path ---

// TestAssembleMessages_Tim_LinearChain tests a 10-turn linear conversation about building STO.
// Each turn is a real exchange from the session where STO was designed and implemented.
// Chain: g1 → g2 → g3 → ... → g10. Assemble for g10 returns 9 upstream turns in order.
func TestAssembleMessages_Tim_LinearChain(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	canvasStore := glyphstorage.NewCanvasStore(db)
	ctx := context.Background()

	glyphIDs := []string{"g1", "g2", "g3", "g4", "g5", "g6", "g7", "g8", "g9", "g10"}

	var edges []*pb.CompositionEdge
	for i := 0; i < len(glyphIDs)-1; i++ {
		edges = append(edges, &pb.CompositionEdge{
			From:      glyphIDs[i],
			To:        glyphIDs[i+1],
			Direction: "right",
			Position:  int32(i),
		})
	}

	setupCanvasWithGlyphs(t, canvasStore, glyphIDs, "comp-sto", edges)

	t0 := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	turns := []struct {
		id       string
		template string
		response string
	}{
		{"g1", "STO", "STO — Single-turn only. The C++ engine has multi-turn support via prepare_prompt(), but the Go prompt handler sends each request as a new single-turn call."},
		{"g2", "is it a straightforward thing? explain your understanding of the situation", "The llama.cpp plugin already accepts a messages array in C++. The gap is the Go side: HandlePromptDirect builds a single system+user pair and doesn't populate the Messages field on ChatRequest."},
		{"g3", "how do we get started?", "Three layers need changes: proto (add repeated ChatMessage), C++ plugin (already done internally, expose via gRPC), and Go handler (assemble history from canvas melds, populate ChatRequest.Messages)."},
		{"g4", "create a new branch for STO", "Created branch llama-cpp-multi-turn from main."},
		{"g5", "do step 1 — the proto change", "Added repeated ChatMessage messages = 8 to LLMChatRequest. Marked system_prompt and user_prompt as deprecated."},
		{"g6", "focus on the cpp plugin", "Updated Chat and StreamChat handlers to read req->messages() when populated. Falls back to deprecated system_prompt/user_prompt for backwards compat."},
		{"g7", "is the frontend the right place to construct message history?", "No — the canvas IS the conversation. Meld edges define which glyphs are upstream. The server should assemble history from the canvas graph, not the frontend."},
		{"g8", "we have the melds in the backend", "Right. ConversationAssembler traces meld edges upstream from a glyph, queries prompt-result attestations for each ancestor, and builds an ordered message array sorted by timestamp."},
		{"g9", "can we write tests for this?", "Yes — it's pure Go. Mock the queryStore interface, use real DB for canvas store with qntxtest.CreateTestDB. Test data: this conversation about STO itself."},
	}

	byContext := make(map[string][]*types.As)
	for i, turn := range turns {
		as := promptResultAs(turn.id, turn.template, turn.response, t0.Add(time.Duration(i)*time.Minute))
		byContext[turn.id] = []*types.As{as}
	}

	qs := &mockQueryStore{byContext: byContext}
	assembler := NewConversationAssembler(canvasStore, qs)

	messages, err := assembler.AssembleMessages(ctx, "g10")
	if err != nil {
		t.Fatalf("AssembleMessages: %v", err)
	}

	// 9 upstream turns × 2 messages each (user + assistant) = 18
	if len(messages) != 18 {
		t.Fatalf("expected 18 messages, got %d", len(messages))
	}

	// Verify alternating user/assistant
	for i, m := range messages {
		expectedRole := "user"
		if i%2 == 1 {
			expectedRole = "assistant"
		}
		if m.Role != expectedRole {
			t.Errorf("message[%d]: expected role %q, got %q", i, expectedRole, m.Role)
		}
	}

	// First user message is "STO"
	if got := messages[0].TextContent(); got != "STO" {
		t.Errorf("first message: expected %q, got %q", "STO", got)
	}

	// Last assistant message is g9's response
	if got := messages[17].TextContent(); got != turns[8].response {
		t.Errorf("last message: expected g9 response, got %q", got)
	}

	// Verify chronological ordering — each user message matches the expected turn
	for i, turn := range turns {
		got := messages[i*2].TextContent()
		if got != turn.template {
			t.Errorf("turn %d user message: expected %q, got %q", i+1, turn.template, got)
		}
	}
}

// TestAssembleMessages_Tim_SingleGlyph tests that a glyph with no composition returns nil.
func TestAssembleMessages_Tim_SingleGlyph(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	canvasStore := glyphstorage.NewCanvasStore(db)
	ctx := context.Background()

	err := canvasStore.UpsertGlyph(ctx, &glyphstorage.CanvasGlyph{
		ID:     "lone",
		Symbol: "prompt",
	})
	if err != nil {
		t.Fatalf("UpsertGlyph: %v", err)
	}

	qs := &mockQueryStore{byContext: map[string][]*types.As{}}
	assembler := NewConversationAssembler(canvasStore, qs)

	messages, err := assembler.AssembleMessages(ctx, "lone")
	if err != nil {
		t.Fatalf("AssembleMessages: %v", err)
	}
	if messages != nil {
		t.Errorf("expected nil for single glyph, got %d messages", len(messages))
	}
}

// TestAssembleMessages_Tim_RootOfChain tests that the first glyph in a chain has no upstream.
func TestAssembleMessages_Tim_RootOfChain(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	canvasStore := glyphstorage.NewCanvasStore(db)
	ctx := context.Background()

	glyphIDs := []string{"root", "child"}
	edges := []*pb.CompositionEdge{
		{From: "root", To: "child", Direction: "right"},
	}
	setupCanvasWithGlyphs(t, canvasStore, glyphIDs, "comp-root", edges)

	qs := &mockQueryStore{byContext: map[string][]*types.As{}}
	assembler := NewConversationAssembler(canvasStore, qs)

	messages, err := assembler.AssembleMessages(ctx, "root")
	if err != nil {
		t.Fatalf("AssembleMessages: %v", err)
	}
	if messages != nil {
		t.Errorf("expected nil for root glyph, got %d messages", len(messages))
	}
}

// --- Spike: edge cases ---

// TestAssembleMessages_Spike_MidChain tests assembling from the middle of a chain.
// g1 → g2 → g3 → g4. Assemble for g3 should return g1 and g2 only, not g4.
func TestAssembleMessages_Spike_MidChain(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	canvasStore := glyphstorage.NewCanvasStore(db)
	ctx := context.Background()

	glyphIDs := []string{"g1", "g2", "g3", "g4"}
	edges := []*pb.CompositionEdge{
		{From: "g1", To: "g2", Direction: "right", Position: 0},
		{From: "g2", To: "g3", Direction: "right", Position: 1},
		{From: "g3", To: "g4", Direction: "right", Position: 2},
	}
	setupCanvasWithGlyphs(t, canvasStore, glyphIDs, "comp-mid", edges)

	t0 := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	byContext := map[string][]*types.As{
		"g1": {promptResultAs("g1", "first question", "first answer", t0)},
		"g2": {promptResultAs("g2", "second question", "second answer", t0.Add(time.Minute))},
		"g3": {promptResultAs("g3", "third question", "third answer", t0.Add(2*time.Minute))},
		"g4": {promptResultAs("g4", "fourth question", "fourth answer", t0.Add(3*time.Minute))},
	}

	qs := &mockQueryStore{byContext: byContext}
	assembler := NewConversationAssembler(canvasStore, qs)

	messages, err := assembler.AssembleMessages(ctx, "g3")
	if err != nil {
		t.Fatalf("AssembleMessages: %v", err)
	}

	// Only g1 and g2 upstream of g3 → 4 messages
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages (2 upstream turns), got %d", len(messages))
	}

	if got := messages[0].TextContent(); got != "first question" {
		t.Errorf("expected first question, got %q", got)
	}
	if got := messages[2].TextContent(); got != "second question" {
		t.Errorf("expected second question, got %q", got)
	}
}

// --- Jenny: complex scenarios ---

// TODO(MELD-CONV): Two conversations spliced together via meld into a single target glyph.
// Design space: when threadA (spiderman) and threadB (toy story) both have edges pointing
// to a target glyph, the assembler would walk both threads upstream and concatenate them
// chronologically. But which thread is "leading"? Is the merged context what we want,
// or should one thread be primary and the other supplementary?
//
// This is the canvas-as-conversation-topology idea taken to its logical conclusion:
// meld topology defines conversation context. Splicing two threads means the model
// sees both histories. Whether that's useful depends on the user's intent when
// creating the meld.
//
// Not in scope for the STO branch — STO is about making linear chains work first.
// Revisit when meld UI supports multi-parent targets.
//
// func TestAssembleMessages_Jenny_MeldSplice(t *testing.T) {
// 	// threadA: a1 → a2 → a3 ──┐
// 	//                          ├──→ target
// 	// threadB: b1 → b2 ───────┘
// 	//
// 	// Questions to resolve before implementing:
// 	// - Does the meld UI even allow creating this topology today?
// 	// - Should both threads have equal weight, or is one "context" and the other "conversation"?
// 	// - What order should messages appear in? Pure chronological? Or threadA then threadB?
// 	// - Should the assembler expose which thread each message came from?
// }
