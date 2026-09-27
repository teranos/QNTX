package server

import (
	"context"
	"testing"

	"github.com/teranos/QNTX/internal/version"
)

// The full commit is the point. The WebSocket connect frame already sends
// Short(), so a sigil that truncated too would answer nothing new.
func TestAmVersionGivesTheFullCommit(t *testing.T) {
	store, db := createTestStore(t)
	srv, err := NewQNTXServer(db, servingOne(db, store), ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	original := version.CommitHash
	version.CommitHash = "019988dd200715175b97a2fcdde47f0e33ccf405"
	defer func() { version.CommitHash = original }()

	answer, refusal := srv.amSignum().Answers["version"](context.Background(), nil)
	if refusal != nil {
		t.Fatalf("version refused: %s", refusal.GetSays())
	}

	got, ok := answer.(version.Info)
	if !ok {
		t.Fatalf("answer is %T, not version.Info", answer)
	}
	if got.CommitHash != version.CommitHash {
		t.Errorf("commit_hash = %q, want the full %q", got.CommitHash, version.CommitHash)
	}
	if got.Platform == "" || got.GoVersion == "" {
		t.Errorf("platform and go_version must be populated, got %+v", got)
	}
}
