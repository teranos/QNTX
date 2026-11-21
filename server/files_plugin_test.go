package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 「[Fable 5.2] A screenshot as a Mac's Photos library keeps one: the bytes a JPEG starts with.」
var screenshot = []byte("\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01 then the picture")

// "in reality, the photo syncs to my iCloud" "and iPluk picks it up"
func TestTheNodeKeepsWhatAPluginHandsIt(t *testing.T) {
	srv := createFileTestServer(t)
	kept, err := srv.KeepFileForPlugin("ipluk", "1816772E_4_5005_c.jpeg", screenshot, "")
	if err != nil {
		t.Fatalf("the node did not keep it: %v", err)
	}
	if kept.Asset != nameOf(screenshot) {
		t.Errorf("asset = %q, want %q", kept.Asset, nameOf(screenshot))
	}
	if kept.ContentType != "image/jpeg" || kept.Size != int64(len(screenshot)) {
		t.Errorf("kept as %q, %d bytes; want image/jpeg, %d", kept.ContentType, kept.Size, len(screenshot))
	}

	// It is served as a file a person brought is.
	serve := httptest.NewRecorder()
	srv.HandleFiles(serve, httptest.NewRequest(http.MethodGet, "/api/files/"+kept.ID, nil))
	if serve.Code != http.StatusOK || serve.Body.String() != string(screenshot) {
		t.Fatalf("served %d, %d bytes; want 200 and the %d handed", serve.Code, serve.Body.Len(), len(screenshot))
	}

	// "k would be storing media assets": held of this node, by the plugin that brought it.
	said := saidOf(t, srv, kept.Asset)
	if len(said) != 1 {
		t.Fatalf("%d lines about %s, want 1", len(said), kept.Asset)
	}
	line := said[0]
	if len(line.Predicates) != 1 || line.Predicates[0] != "held" {
		t.Errorf("predicates = %v, want [held]", line.Predicates)
	}
	if len(line.Contexts) != 1 || line.Contexts[0] != fileTestNode {
		t.Errorf("contexts = %v, want [%s]", line.Contexts, fileTestNode)
	}
	if len(line.Actors) != 1 || line.Actors[0] != "ipluk" {
		t.Errorf("actors = %v, want who brought it, [ipluk]", line.Actors)
	}
	if line.Attributes["file_id"] != kept.ID {
		t.Errorf("file_id = %v, want %s", line.Attributes["file_id"], kept.ID)
	}
}

// 「[Fable 5.2] What a person is refused, a plugin is refused, and is told why.」
func TestAPluginIsRefusedWhatAPersonIs(t *testing.T) {
	srv := createFileTestServer(t)
	for filename, content := range map[string][]byte{
		"tool.exe":    []byte("MZ a program"),
		"picture.jpg": []byte("MZ a program named as a picture"),
	} {
		_, err := srv.KeepFileForPlugin("ipluk", filename, content, "")
		if err == nil {
			t.Errorf("%s was kept", filename)
			continue
		}
		if !strings.Contains(err.Error(), filename) || !strings.Contains(err.Error(), "ipluk") {
			t.Errorf("the refusal of %s names neither the file nor who brought it: %v", filename, err)
		}
		if said := saidOf(t, srv, nameOf(content)); len(said) != 0 {
			t.Errorf("%d lines about a file that was refused", len(said))
		}
	}
}

// "to me it feels like k should own my Media Assets"
// 「[Fable 5.2] A node with no primary knows no peer: the file is kept here, and why it went nowhere is said.」
func TestAFileForAPeerIsKeptWhenItCannotBeBrought(t *testing.T) {
	srv := createFileTestServer(t)
	kept, err := srv.KeepFileForPlugin("ipluk", "shot.jpeg", screenshot, "k")
	if err != nil {
		t.Fatalf("the node did not keep it: %v", err)
	}
	if kept.ID == "" || kept.Asset != nameOf(screenshot) {
		t.Errorf("kept as %q, %q", kept.ID, kept.Asset)
	}
	if kept.BroughtTo != "" || kept.NotBrought == "" {
		t.Errorf("brought to %q, not brought because %q; want nowhere, and why", kept.BroughtTo, kept.NotBrought)
	}
	if said := saidOf(t, srv, kept.Asset); len(said) != 1 {
		t.Errorf("%d lines about %s, want the one held line of this node", len(said), kept.Asset)
	}
}
