package server

import (
	"testing"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/server/statusline"
)

// A job with no handler name must still name something the row can draw and a
// click can find, rather than drawing an empty span.
func TestUnnamedHandlerFallsBackToTheJobID(t *testing.T) {
	s := &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), handlerFailures: statusline.NewHandlerFailureLog()}
	s.noteHandlerFailure(statusline.HandlerFailure{
		ScheduledJobID: "AS-CAPYCAPY-SCHEDULE-PULSE-FZ36C6PW",
		Error:          "no name on the job",
	})

	held := s.handlerFailures.Since(statusline.HandlerFailureWindow)
	if len(held) != 1 {
		t.Fatalf("held %d failures: %+v", len(held), held)
	}
	if held[0].Handler != "AS-CAPYCAPY-SCHEDULE-PULSE-FZ36C6PW" {
		t.Fatalf("the row would draw a nameless span: %q", held[0].Handler)
	}
}
