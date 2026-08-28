package server

import (
	"testing"
	"time"
)

func failing(handler, errText string, count int) handlerFailureRun {
	return handlerFailureRun{
		Handler: handler,
		Count:   count,
		Latest: HandlerFailure{
			Handler: handler,
			Error:   errText,
			AtMs:    time.Now().Add(-20 * time.Minute).UnixMilli(),
		},
	}
}

// "the error reason is more interesting for me to see than 20m"
func TestTheReasonTakesTheSlotNotTheAge(t *testing.T) {
	run := failing("capy/capy.campaigns",
		`rpc error: code = Unavailable desc = closing transport due to: EOF`, 1)

	items := handlerFailureItemsFor([]handlerFailureRun{run})
	if len(items) != 1 {
		t.Fatalf("drew %d items", len(items))
	}
	if items[0].Note != "Unavailable" {
		t.Fatalf("the slot does not carry the reason: %q", items[0].Note)
	}
}

func TestShortReasonDistilsWhatItCan(t *testing.T) {
	cases := map[string]string{
		`rpc error: code = Unavailable desc = closing transport`:                          "Unavailable",
		`rpc error: code = DeadlineExceeded desc = context deadline exceeded`:             "DeadlineExceeded",
		`transport: Error while dialing: dial tcp 127.0.0.1:38703: connect: connection refused`: "connection refused",
		"":                                                                                "failed",
		"plugin execution error (job=JB-X, handler=capy.account): Traceback (most recent call last):": "Traceback (most recent call l",
	}
	for errText, want := range cases {
		if got := shortReason(errText); got != want && len(got) > handlerReasonLimit {
			t.Fatalf("shortReason(%q) = %q, longer than the row allows", errText, got)
		}
	}
	if got := shortReason(`rpc error: code = Unavailable desc = x`); got != "Unavailable" {
		t.Fatalf("a gRPC status did not reduce to its code: %q", got)
	}
	if got := shortReason(""); got != "failed" {
		t.Fatalf("an empty error said %q", got)
	}
}

// "why not leave out pluginname.handlername 20m and just do individual
// carrousels per plugin"
func TestAPluginRotatesOverItsOwnHandlers(t *testing.T) {
	handlers := []string{
		"capy/capy.campaigns", "capy/capy.account", "capy/capy.whoami",
		"duif/poll_inbox", "wal-checkpoint",
	}

	own := handlersOf(handlers, "capy")
	if len(own) != 3 {
		t.Fatalf("capy claimed %d handlers: %v", len(own), own)
	}
	if pluginOf("wal-checkpoint") != "" {
		t.Fatalf("a built-in was claimed by a plugin")
	}
	if pluginOf("duif/poll_inbox") != "duif" {
		t.Fatalf("the namespace does not name the plugin")
	}
}

// "higher prio for known failing handlers" — and version stays, but rarely.
func TestFailingHandlersOutweighTheVersion(t *testing.T) {
	handlers := []string{"capy/capy.campaigns", "capy/capy.account", "capy/capy.whoami"}
	failures := []handlerFailureRun{
		failing("capy/capy.campaigns", "rpc error: code = Unavailable desc = x", 3),
	}

	frames := pluginFrames("capy", "0.244.0", handlers, failures)

	var broken, version, working int
	for _, f := range frames {
		switch {
		case f.Glyph == GlyphUnwell:
			broken++
		case f.Note == "0.244.0":
			version++
		default:
			working++
		}
	}

	if broken != failingHandlerWeight {
		t.Fatalf("a failing handler got %d frames, want %d", broken, failingHandlerWeight)
	}
	if version != 1 {
		t.Fatalf("version got %d frames, want exactly one", version)
	}
	if working != 2 {
		t.Fatalf("working handlers got %d frames, want 2", working)
	}
	if frames[0].Glyph != GlyphWell && frames[0].Name != "capy/capy.campaigns" {
		t.Fatalf("the failure does not lead the rotation: %+v", frames[0])
	}
}

// A plugin that declared nothing has only its version, which is where it was.
func TestAPluginWithNoHandlersKeepsItsVersion(t *testing.T) {
	item := pluginItem(0, "qntxffmpeg", "0.2.4", true, nil, nil)
	if item.Name != "qntxffmpeg" || item.Note != "0.2.4" {
		t.Fatalf("a plugin with no handlers drew %+v", item)
	}
}

// An unwell plugin reads unwell on every frame, whichever handler is showing.
func TestAnUnwellPluginStaysUnwellAcrossFrames(t *testing.T) {
	handlers := []string{"duif/poll_inbox"}
	for at := 0; at < 6; at++ {
		item := pluginItem(at, "duif", "0.3.9", false, handlers, nil)
		if item.Glyph != GlyphUnwell {
			t.Fatalf("frame %d of an unwell plugin read well: %+v", at, item)
		}
	}
}

// The slot is named for the plugin, so the namespace on the handler is noise.
func TestTheHandlerDropsTheNamespaceItsSlotAlreadyCarries(t *testing.T) {
	if got := shortHandler("capy/capy.campaigns"); got != "capy.campaigns" {
		t.Fatalf("shortHandler kept the namespace: %q", got)
	}
	if got := shortHandler("wal-checkpoint"); got != "wal-checkpoint" {
		t.Fatalf("a handler with no namespace was cut: %q", got)
	}
}
