package services

import (
	"context"
	"testing"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

const keepTestToken = "the token plugins reach the node's services with"

func keepTestServer(t *testing.T) *FileServiceServer {
	t.Helper()
	return NewFileServiceServer(t.TempDir(), keepTestToken, zap.NewNop().Sugar())
}

// "in reality, the photo syncs to my iCloud" "and iPluk picks it up"
func TestKeepFileHandsTheNodeWhatAPluginBrings(t *testing.T) {
	s := keepTestServer(t)
	var by, filename string
	var content []byte
	s.KeepWith(func(b, f string, c []byte, _ string) (KeptFile, error) {
		by, filename, content = b, f, c
		return KeptFile{ID: "an-id", Asset: "sha256:abc", ContentType: "image/jpeg", Size: int64(len(c))}, nil
	})
	resp, err := s.KeepFile(context.Background(), &protocol.KeepFileRequest{
		AuthToken: keepTestToken, By: "ipluk", Filename: "shot.jpeg", Content: []byte("the picture"),
	})
	if err != nil {
		t.Fatalf("KeepFile: %v", err)
	}
	if !resp.Success || resp.Error != "" {
		t.Fatalf("not kept: %q", resp.Error)
	}
	if by != "ipluk" || filename != "shot.jpeg" || string(content) != "the picture" {
		t.Errorf("the node was handed %q, %q, %q", by, filename, content)
	}
	if resp.FileId != "an-id" || resp.Asset != "sha256:abc" || resp.ContentType != "image/jpeg" || resp.Size != 11 {
		t.Errorf("answered %v", resp)
	}
}

func TestKeepFileRefuses(t *testing.T) {
	kept := func(string, string, []byte, string) (KeptFile, error) { return KeptFile{ID: "an-id"}, nil }
	refuses := func(string, string, []byte, string) (KeptFile, error) {
		return KeptFile{}, errors.New("unsupported file type: .exe")
	}
	for name, tc := range map[string]struct {
		keep FileKeeper
		req  *protocol.KeepFileRequest
	}{
		"a token that is not the node's": {kept, &protocol.KeepFileRequest{AuthToken: "another", By: "ipluk", Filename: "shot.jpeg"}},
		"a file brought by nobody":       {kept, &protocol.KeepFileRequest{AuthToken: keepTestToken, Filename: "shot.jpeg"}},
		"a node handed no way to keep":   {nil, &protocol.KeepFileRequest{AuthToken: keepTestToken, By: "ipluk", Filename: "shot.jpeg"}},
		"what the node refuses":          {refuses, &protocol.KeepFileRequest{AuthToken: keepTestToken, By: "ipluk", Filename: "tool.exe"}},
	} {
		t.Run(name, func(t *testing.T) {
			s := keepTestServer(t)
			if tc.keep != nil {
				s.KeepWith(tc.keep)
			}
			resp, err := s.KeepFile(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("KeepFile: %v", err)
			}
			if resp.Success || resp.Error == "" || resp.FileId != "" {
				t.Errorf("answered %v, want a refusal that says why", resp)
			}
		})
	}
}

// "its on k, i had to do nothing for it"
// 「[Fable 5.2] ipluk stands in a namespace, so the node hands it a token of its own (ADR-046) and」
// 「[Fable 5.2] not the shared one. On l, 2026-11-21 16:46, three screenshots were refused with」
// 「[Fable 5.2] "invalid authentication token": this service knew the shared token only.」
func TestAPluginIsKnownByItsOwnToken(t *testing.T) {
	s := keepTestServer(t)
	s.SetPluginNames(func(token string) (string, bool) {
		if token == "the token minted for ipluk" {
			return "ipluk", true
		}
		return "", false
	})
	var by string
	s.KeepWith(func(b, _ string, _ []byte, _ string) (KeptFile, error) {
		by = b
		return KeptFile{ID: "an-id"}, nil
	})

	// Its own token is taken, and who brought the file is the plugin the token
	// was minted for, whatever the call says of itself.
	resp, err := s.KeepFile(context.Background(), &protocol.KeepFileRequest{
		AuthToken: "the token minted for ipluk", By: "somebody else", Filename: "shot.jpeg", Content: []byte("the picture"),
	})
	if err != nil {
		t.Fatalf("KeepFile: %v", err)
	}
	if !resp.Success {
		t.Fatalf("a plugin was refused with its own token: %q", resp.Error)
	}
	if by != "ipluk" {
		t.Errorf("kept as brought by %q, want ipluk", by)
	}

	// A token the node minted for nobody is still refused.
	resp, err = s.KeepFile(context.Background(), &protocol.KeepFileRequest{
		AuthToken: "a token minted for nobody", By: "ipluk", Filename: "shot.jpeg",
	})
	if err != nil {
		t.Fatalf("KeepFile: %v", err)
	}
	if resp.Success || resp.Error == "" {
		t.Errorf("answered %v, want a refusal", resp)
	}
}
