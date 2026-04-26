package sqlitecgo

import (
	"sync"
	"time"
)

// WatchdogConfig configures the mutex watchdog.
type WatchdogConfig struct {
	Interval time.Duration               // how often to probe
	Timeout  time.Duration               // how long to wait for mutex acquisition
	OnAlert  func(blocked time.Duration) // called when acquisition exceeds Timeout
}

// StartMutexWatchdog spawns a goroutine that periodically probes the given mutex.
// If acquisition takes longer than Timeout, OnAlert is called.
// Returns a stop function. Safe to call stop multiple times.
func StartMutexWatchdog(mu *sync.Mutex, cfg WatchdogConfig) func() {
	done := make(chan struct{})
	var once sync.Once

	go func() {
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				probe(mu, cfg.Timeout, cfg.OnAlert)
			}
		}
	}()

	return func() {
		once.Do(func() { close(done) })
	}
}

func probe(mu *sync.Mutex, timeout time.Duration, onAlert func(time.Duration)) {
	acquired := make(chan struct{})
	start := time.Now()

	go func() {
		mu.Lock()
		mu.Unlock() //nolint:SA2001 // probe — we only measure acquisition time
		close(acquired)
	}()

	select {
	case <-acquired:
		// healthy
	case <-time.After(timeout):
		onAlert(time.Since(start))
		// wait for the probe goroutine to finish so we don't leak
		<-acquired
	}
}
