package server

import (
	"net/http"
	"testing"
	"time"
)

// Stop closes every socket and cancels the context, which ends the hub. A read
// pump whose socket closes after the hub ended has nobody to unregister with,
// and waiting for one held every stop of the node to ShutdownTimeout.
func TestAReadPumpEndsOnceTheHubHasStopped(t *testing.T) {
	srv := nodeUnderTest(t)
	held := socketAs(t, srv, func(r *http.Request) *http.Request { return r })
	if len(held) != 1 {
		t.Fatalf("%d clients registered, want 1", len(held))
	}

	srv.cancel()
	time.Sleep(50 * time.Millisecond)
	_ = held[0].conn.Close()

	done := make(chan struct{})
	go func() {
		srv.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("the node's goroutines did not end after the hub stopped: %v", srv.wg.Running())
	}
}
