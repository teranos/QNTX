package server

import (
	"sync"
	"time"
)

// The node advances the row, not the caller. Every surface drawing it is on the
// same frame, and a tmux pane that redraws twice a second does not race a
// terminal that redraws every ten.

// A row that changed every five minutes from the start would take most of an
// hour to show what it holds. Sweeping the whole set fast a few times first is
// how the set becomes known; settling afterwards is how it stops being noise.
const (
	carouselFast   = 2 * time.Second
	carouselSweeps = 3
	carouselRest   = 5 * time.Minute
)

// carouselInterval is how long the frame after `advances` moves stays up.
// Fast until the set has been swept carouselSweeps times, then doubling until
// it rests at carouselRest.
func carouselInterval(advances, items int) time.Duration {
	if items <= 0 {
		return carouselRest
	}
	if advances < carouselSweeps*items {
		return carouselFast
	}

	slowed := advances - carouselSweeps*items
	// A shift past the width of the type wraps to zero or negative, and the
	// cap is reached long before that.
	if slowed >= 32 {
		return carouselRest
	}
	held := carouselFast << slowed
	if held <= 0 || held > carouselRest {
		return carouselRest
	}
	return held
}

// carousel is which frame the row is showing and when it moves next. It holds
// an index rather than a value, so what is drawn is read fresh every time.
type carousel struct {
	mu       sync.Mutex
	at       int
	advances int
	movesAt  time.Time
	// Injected so a test can move time rather than wait for it.
	now func() time.Time
}

func newCarousel() *carousel {
	return &carousel{now: time.Now}
}

// frame returns the index to draw out of `items`, advancing first if the
// current one has had its time. Zero items is index zero and nothing drawn.
func (c *carousel) frame(items int) int {
	if items <= 0 {
		return 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if c.movesAt.IsZero() {
		c.movesAt = now.Add(carouselInterval(c.advances, items))
		return c.at % items
	}

	// A node asleep for an hour advances once, not eighteen hundred times.
	// What is owed is a fresh frame, not a replay of the ones nobody saw.
	if !now.Before(c.movesAt) {
		c.at++
		c.advances++
		c.movesAt = now.Add(carouselInterval(c.advances, items))
	}
	return c.at % items
}
