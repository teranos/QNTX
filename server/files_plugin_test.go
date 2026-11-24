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

// "and slowly" "it needs to kind of get out of the way of other things happening"
// 「[Fable 5.2] A file meant for a peer is kept and answered for at once, and waits to be brought:」
// 「[Fable 5.2] who handed it over is told nothing of the peer, and waits on none.」
func TestAFileForAPeerIsKeptAtOnceAndWaitsToBeBrought(t *testing.T) {
	srv := createFileTestServer(t)
	kept, err := srv.KeepFileForPlugin("ipluk", "shot.jpeg", screenshot, "k")
	if err != nil {
		t.Fatalf("the node did not keep it: %v", err)
	}
	if kept.ID == "" || kept.Asset != nameOf(screenshot) {
		t.Errorf("kept as %q, %q", kept.ID, kept.Asset)
	}
	if kept.BroughtTo != "" || kept.NotBrought != "" {
		t.Errorf("the answer speaks of the peer: brought to %q, not brought because %q", kept.BroughtTo, kept.NotBrought)
	}

	// The held line says where it is meant to go, and that is what waits.
	waiting, err := srv.stillToBring()
	if err != nil {
		t.Fatalf("what waits did not read: %v", err)
	}
	if len(waiting) != 1 {
		t.Fatalf("%d files wait to be brought, want 1", len(waiting))
	}
	if got := waiting[0]; got.asset != kept.Asset || got.fileID != kept.ID || got.to != "k" || got.filename != "shot.jpeg" {
		t.Errorf("waiting = %+v", got)
	}

	// A file meant for no peer waits for nothing.
	if _, err := srv.KeepFileForPlugin("ipluk", "report.pdf", pdfHeader(), ""); err != nil {
		t.Fatalf("the node did not keep it: %v", err)
	}
	if waiting, _ := srv.stillToBring(); len(waiting) != 1 {
		t.Errorf("%d files wait after one meant for no peer, want 1", len(waiting))
	}
}

// 「[Fable 5.2] Once a line says the peer holds it, it waits no more: a restart brings it not again.」
func TestAFileThatWasBroughtWaitsNoMore(t *testing.T) {
	srv := createFileTestServer(t)
	kept, err := srv.KeepFileForPlugin("ipluk", "shot.jpeg", screenshot, "k")
	if err != nil {
		t.Fatalf("the node did not keep it: %v", err)
	}
	// The same bytes handed over twice are brought once.
	if _, err := srv.KeepFileForPlugin("ipluk", "shot-again.jpeg", screenshot, "k"); err != nil {
		t.Fatalf("the node did not keep it: %v", err)
	}
	waiting, err := srv.stillToBring()
	if err != nil || len(waiting) != 1 {
		t.Fatalf("%d files wait (%v), want the one asset once", len(waiting), err)
	}

	if err := srv.attestBrought(waiting[0], "the-id-at-k"); err != nil {
		t.Fatalf("that it is at k was not written: %v", err)
	}
	if waiting, _ := srv.stillToBring(); len(waiting) != 0 {
		t.Errorf("%d files wait after it was brought, want none", len(waiting))
	}

	// "k would be storing media assets": brought to k, by this node.
	var brought int
	for _, line := range saidOf(t, srv, kept.Asset) {
		if len(line.Predicates) == 1 && line.Predicates[0] == "brought" {
			brought++
			if len(line.Contexts) != 1 || line.Contexts[0] != "k" {
				t.Errorf("contexts = %v, want [k]", line.Contexts)
			}
			if len(line.Actors) != 1 || line.Actors[0] != fileTestNode {
				t.Errorf("actors = %v, want this node, [%s]", line.Actors, fileTestNode)
			}
			if line.Attributes["file_id_there"] != "the-id-at-k" {
				t.Errorf("file_id_there = %v", line.Attributes["file_id_there"])
			}
		}
	}
	if brought != 1 {
		t.Errorf("%d brought lines, want 1", brought)
	}
}
