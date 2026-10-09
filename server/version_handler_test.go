package server

import (
	"context"
	"testing"

	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
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

	got, ok := answer.(*protocol.VersionInfo)
	if !ok {
		t.Fatalf("answer is %T, not protocol.VersionInfo", answer)
	}
	if got.GetCommitHash() != version.CommitHash {
		t.Errorf("commit_hash = %q, want the full %q", got.GetCommitHash(), version.CommitHash)
	}
	if got.GetPlatform() == "" || got.GetGoVersion() == "" {
		t.Errorf("platform and go_version must be populated, got %+v", got)
	}
}
