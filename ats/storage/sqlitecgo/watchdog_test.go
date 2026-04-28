package sqlitecgo

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchdog_DetectsStuckMutex(t *testing.T) {
	var mu sync.Mutex
	var alerts atomic.Int32

	mu.Lock() // simulate stuck holder

	stop := StartMutexWatchdog(&mu, WatchdogConfig{
		Interval: 50 * time.Millisecond,
		Timeout:  10 * time.Millisecond,
		OnAlert: func(d time.Duration) {
			alerts.Add(1)
		},
	})

	// Wait for at least one alert
	time.Sleep(150 * time.Millisecond)
	mu.Unlock()
	stop()

	if alerts.Load() == 0 {
		t.Fatal("watchdog did not fire an alert for stuck mutex")
	}
}

func TestWatchdog_NoAlertWhenHealthy(t *testing.T) {
	var mu sync.Mutex
	var alerts atomic.Int32

	stop := StartMutexWatchdog(&mu, WatchdogConfig{
		Interval: 50 * time.Millisecond,
		Timeout:  100 * time.Millisecond,
		OnAlert: func(d time.Duration) {
			alerts.Add(1)
		},
	})

	time.Sleep(200 * time.Millisecond)
	stop()

	if alerts.Load() != 0 {
		t.Fatalf("watchdog fired %d alerts on healthy mutex", alerts.Load())
	}
}

func TestWatchdog_StopIsIdempotent(t *testing.T) {
	var mu sync.Mutex
	stop := StartMutexWatchdog(&mu, WatchdogConfig{
		Interval: 50 * time.Millisecond,
		Timeout:  10 * time.Millisecond,
		OnAlert:  func(d time.Duration) {},
	})
	stop()
	stop() // second call should not panic
}
