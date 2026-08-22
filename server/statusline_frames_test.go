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
