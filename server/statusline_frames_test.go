package server

import (
	"strings"
	"testing"
	"time"
)

// A node with every answer, so a frame that fails to read one is visible.
type fullNode struct{}

func (fullNode) Uptime() time.Duration { return 26*time.Hour + 11*time.Minute }
func (fullNode) ParserVersion() string { return "0.4.0" }

func (fullNode) Pressure() (float64, float64, bool) { return 12, 63, true }
func (fullNode) Attestations() (int, bool)          { return 1_200_000, true }
func (fullNode) Watchers() int                      { return 7 }
func (fullNode) Schedules() int                     { return 3 }
func (fullNode) Handlers() int                      { return 11 }
func (fullNode) HandlerNames() []string             { return nil }
func (fullNode) Refusals() (int64, int64)           { return 4, 0 }
func (fullNode) Answered() (int64, int64)           { return 12, 0 }
func (fullNode) Goroutines() int                    { return 41 }
func (fullNode) HeapBytes() uint64                  { return 37 << 20 }

// A node that has answered nothing yet, which is what the first seconds after
// a restart look like.
type coldNode struct{ fullNode }

func (coldNode) ParserVersion() string              { return "" }
func (coldNode) Pressure() (float64, float64, bool) { return 0, 0, false }
func (coldNode) Attestations() (int, bool)          { return 0, false }

// Every frame draws something, and none of them draws a blank where a value
// belongs. A row that silently loses a frame is a row nobody can trust.
func TestEveryFrameProduces(t *testing.T) {
	for at, frame := range carouselFrames {
		item := frame.produce(fullNode{})
		if item.Name == "" {
			t.Fatalf("frame %d drew no name", at)
		}
		if item.Note == "" {
			t.Fatalf("frame %d (%s) drew no value", at, item.Name)
		}
		if item.Glyph != GlyphWell {
			t.Fatalf("frame %d (%s) is unwell against a node that answered", at, item.Name)
		}
	}
}

// A value the node could not produce says so and reads unwell, rather than
// drawing a zero that looks like a measurement.
func TestUnreadableFramesSaySo(t *testing.T) {
	unwell := 0
	for at, frame := range carouselFrames {
		item := frame.produce(coldNode{})
		if item.Note == "" {
			t.Fatalf("frame %d (%s) drew a blank", at, item.Name)
		}
		if item.Glyph == GlyphUnwell {
			unwell++
			if item.Note == "0" || item.Note == "0%" {
				t.Fatalf("frame %d (%s) drew a zero for something it could not read", at, item.Name)
			}
		}
	}
	if unwell != 4 {
		t.Fatalf("%d frames reported themselves unreadable, want 4 (ats, cpu, mem, attestations)", unwell)
	}
}

// A node that has turned nobody away has nothing to say about it, and the row
// is too short to spend a slot saying so.
type quietNode struct{ fullNode }

func (quietNode) Refusals() (int64, int64) { return 0, 0 }
func (quietNode) Answered() (int64, int64) { return 0, 0 }

func TestNobodyRefusedDrawsNoFrame(t *testing.T) {
	var shown int
	for _, frame := range carouselFrames {
		if frame.omit != nil && frame.omit(quietNode{}) {
			continue
		}
		shown++
	}
	// Two frames step aside on a node nobody was turned away from and nothing
	// was answered badly by: refused, and the 4xx count.
	if shown != len(carouselFrames)-2 {
		t.Fatalf("%d frames drew against a node that refused nobody, want %d",
			shown, len(carouselFrames)-2)
	}
}

// Stepping past an omitted frame is what keeps the sweep from stuttering: the
// slot moves on rather than going blank for one turn.
func TestTheSlotStepsPastAnOmittedFrame(t *testing.T) {
	h := &StatusLineHandler{carousel: newCarousel(), node: quietNode{}}
	// The refused frame is last, so land the sweep on it.
	h.carousel.at = len(carouselFrames) - 1

	items := h.carouselItem()
	if len(items) != 1 {
		t.Fatalf("the slot drew %d items on an omitted frame, want 1", len(items))
	}
	if items[0].Name == "refused" {
		t.Fatal("the slot drew the frame it was meant to step past")
	}
}

// A person refused signs in a second later. A machine refused keeps presenting
// the same dead credential, so that is the one worth a mark.
func TestATokenRefusedIsUnwell(t *testing.T) {
	item := refusedItem(97, 96)
	if item.Glyph != GlyphUnwell {
		t.Fatalf("96 refusals holding a token drew %q", item.Glyph)
	}
	if !strings.Contains(item.Note, "96") || !strings.Contains(item.Note, "97") {
		t.Fatalf("the note %q says neither how many nor how many held a token", item.Note)
	}

	// Refusals without one are people, and people are not an alarm.
	if quiet := refusedItem(97, 0); quiet.Glyph != GlyphWell {
		t.Fatalf("97 refusals with no token drew %q", quiet.Glyph)
	}
}

// Two units and no decimal, because that is the room a row has.
func TestShortDuration(t *testing.T) {
	for _, c := range []struct {
		in   time.Duration
		want string
	}{
		{0, "0m"},
		{45 * time.Second, "0m"},
		{9 * time.Minute, "9m"},
		{90 * time.Minute, "1h30m"},
		{26*time.Hour + 11*time.Minute, "1d2h"},
		{100 * 24 * time.Hour, "100d0h"},
		{-time.Hour, "0m"},
	} {
		if got := shortDuration(c.in); got != c.want {
			t.Fatalf("%s drew %q, want %q", c.in, got, c.want)
		}
	}
}

// The slot sits between the two pinned halves: who is asking, what the node is
// doing, what it runs.
func TestTheRotatingSlotSitsAfterTheCaller(t *testing.T) {
	h := &StatusLineHandler{carousel: newCarousel(), node: fullNode{}}

	items := h.carouselItem()
	if len(items) != 1 {
		t.Fatalf("the slot drew %d items, want exactly 1", len(items))
	}
	if strings.TrimSpace(items[0].Name) == "" {
		t.Fatal("the slot drew an unnamed item")
	}
}

// A handler with no node behind it draws the caller and nothing invented.
func TestNoNodeDrawsNoRotatingSlot(t *testing.T) {
	h := &StatusLineHandler{carousel: newCarousel()}
	if got := h.carouselItem(); got != nil {
		t.Fatalf("a handler with no node drew %v", got)
	}

	var nilHandler *StatusLineHandler
	if got := nilHandler.carouselItem(); got != nil {
		t.Fatalf("a nil handler drew %v", got)
	}
}
