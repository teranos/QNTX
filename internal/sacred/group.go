package sacred

import (
	"fmt"
	"sort"
	"sync"
)

// Group is a WaitGroup that can say what it is still waiting on. A shutdown
// waited 36.8s on a count, and a count names nothing.
type Group struct {
	wg   sync.WaitGroup
	mu   sync.Mutex
	live map[string]int
}

// Go runs fn as Go does, counted under name until it returns.
func (g *Group) Go(name string, fn func()) {
	done := g.Add(name)
	go func() {
		defer done()
		defer Said(name)
		fn()
	}()
}

// Add counts one goroutine under name, for one this package does not start.
// The answer is its Done, and is called once.
func (g *Group) Add(name string) (done func()) {
	g.mu.Lock()
	if g.live == nil {
		g.live = map[string]int{}
	}
	g.live[name]++
	g.mu.Unlock()
	g.wg.Add(1)

	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.live[name]--
			if g.live[name] == 0 {
				delete(g.live, name)
			}
			g.mu.Unlock()
			g.wg.Done()
		})
	}
}

// Wait blocks until every counted goroutine is done.
func (g *Group) Wait() {
	g.wg.Wait()
}

// Running is every name still counted, sorted, with how many when more than one.
func (g *Group) Running() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	names := make([]string, 0, len(g.live))
	for name, n := range g.live {
		if n > 1 {
			name = fmt.Sprintf("%s ×%d", name, n)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
