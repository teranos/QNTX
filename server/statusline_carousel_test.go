package server

import (
	"testing"
	"time"
)

// A carousel whose clock is ours, so the ramp is measured rather than waited on.
func heldAt(start time.Time) (*carousel, *time.Time) {
	clock := start
	c := newCarousel()
	c.now = func() time.Time { return clock }
	return c, &clock
}

// The set has to be readable before it is worth resting on. Three sweeps at the
// fast interval is what makes it known.
func TestTheFirstSweepsAreFast(t *testing.T) {
	const items = 10
	for advances := 0; advances < carouselSweeps*items; advances++ {
		if got := carouselInterval(advances, items); got != carouselFast {
			t.Fatalf("advance %d held %s, want %s", advances, got, carouselFast)
		}
	}
}

// After the sweeps it slows, and it does not slow past resting.
func TestItSlowsToRestAndStops(t *testing.T) {
	const items = 10
	first := carouselInterval(carouselSweeps*items, items)
	if first != carouselFast {
		t.Fatalf("the first slowed frame held %s, want %s", first, carouselFast)
	}

	previous := first
	for step := 1; step < 40; step++ {
		got := carouselInterval(carouselSweeps*items+step, items)
		if got < previous {
			t.Fatalf("step %d held %s, shorter than the %s before it", step, got, previous)
		}
		if got > carouselRest {
			t.Fatalf("step %d held %s, past the %s rest", step, got, carouselRest)
		}
		previous = got
	}
	if previous != carouselRest {
		t.Fatalf("it settled at %s, want %s", previous, carouselRest)
	}
}

// The frame does not move until its time is up, however often it is asked.
func TestAskingDoesNotAdvance(t *testing.T) {
	c, _ := heldAt(time.Unix(1_700_000_000, 0))

	first := c.frame(10)
	for i := 0; i < 50; i++ {
		if got := c.frame(10); got != first {
			t.Fatalf("asking moved the row from %d to %d", first, got)
		}
	}
}

// Drawing the row is what moves it, and it moves by one.
func TestTimePassingAdvancesOne(t *testing.T) {
	c, clock := heldAt(time.Unix(1_700_000_000, 0))

	first := c.frame(10)
	*clock = clock.Add(carouselFast)

	if got := c.frame(10); got != first+1 {
		t.Fatalf("after %s the row is at %d, want %d", carouselFast, got, first+1)
	}
}

// A node nobody looked at for an hour owes a fresh frame, not a replay of the
// ones that went unseen while it sat there.
func TestALongSilenceAdvancesOnce(t *testing.T) {
	c, clock := heldAt(time.Unix(1_700_000_000, 0))

	first := c.frame(10)
	*clock = clock.Add(time.Hour)

	if got := c.frame(10); got != first+1 {
		t.Fatalf("after an hour the row is at %d, want %d", got, first+1)
	}
}

// It wraps rather than running off the end of what it was given.
func TestItWrapsAtTheEnd(t *testing.T) {
	c, clock := heldAt(time.Unix(1_700_000_000, 0))

	seen := map[int]bool{}
	for i := 0; i < 12; i++ {
		seen[c.frame(3)] = true
		*clock = clock.Add(carouselFast)
	}

	for want := 0; want < 3; want++ {
		if !seen[want] {
			t.Fatalf("frame %d never came up in twelve advances of three items", want)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("drew %d distinct frames out of 3", len(seen))
	}
}

// Nothing to rotate is not a division by zero.
func TestAnEmptyCarouselIsFrameZero(t *testing.T) {
	c, _ := heldAt(time.Unix(1_700_000_000, 0))

	if got := c.frame(0); got != 0 {
		t.Fatalf("an empty carousel drew frame %d", got)
	}
	if got := carouselInterval(0, 0); got != carouselRest {
		t.Fatalf("an empty carousel held %s, want %s", got, carouselRest)
	}
}
