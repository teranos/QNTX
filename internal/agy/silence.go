package agy

import (
	"fmt"
	"sync/atomic"
	"time"
)

// silentAfter is how long a turn says nothing before it is told. Measured: gemini-3.8-flash
// answered 503 and agy said nothing for 3 minutes, which reads as a hang.
var silentAfter = time.Minute

// watchSilence tells told, with what logs says, each time after passes without heard being
// called. stop ends it, and nothing is told after stop returns.
func watchSilence(after time.Duration, logs func() string, told func(string)) (heard, stop func()) {
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	stopping, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		tick := time.NewTicker(after / 4)
		defer tick.Stop()
		for {
			select {
			case <-stopping:
				return
			case <-tick.C:
				if time.Since(time.Unix(0, last.Load())) >= after {
					last.Store(time.Now().UnixNano())
					told(fmt.Sprintf("agy has said nothing of this turn for %s. %s", after, logs()))
				}
			}
		}
	}()
	return func() { last.Store(time.Now().UnixNano()) }, func() {
		close(stopping)
		<-stopped
	}
}
