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
