package grpc

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"go.uber.org/zap"
)

// "they dont come back? oh, that sounddddds like an issue"
//
// A plugin whose old process outlived the 5s stop was never launched again.
// These hold that it is launched once that process is gone.

func newRelaunchManager(t *testing.T) *PluginManager {
	t.Helper()
	log := zap.NewNop().Sugar()
	m := NewPluginManager(log, log, "")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m.shutdownCtx = ctx
	return m
}

func TestOnceGone_LaunchesOnlyAfterTheOldProcessIsGone(t *testing.T) {
	m := newRelaunchManager(t)
	gone := make(chan struct{})
	ran := make(chan struct{})
	m.onceGone("cyrnel", gone, func(context.Context) { close(ran) })

	m.mu.RLock()
	_, waiting := m.retryCancels["cyrnel"]
	m.mu.RUnlock()
	if !waiting {
		t.Fatal("while it waits, the plugin is not held where a retry loop is")
	}

	select {
	case <-ran:
		t.Fatal("launched again while its old process is still there")
	case <-time.After(100 * time.Millisecond):
	}

	close(gone)
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("not launched again after its old process was gone")
	}
}

func TestOnceGone_TakenOverFirstIsNotLaunched(t *testing.T) {
	m := newRelaunchManager(t)
	gone := make(chan struct{})
	ran := make(chan struct{})
	m.onceGone("qntx-qdrant", gone, func(context.Context) { close(ran) })

	// An enable, restart or disable cancels what waits, as takeLoaded does.
	m.mu.Lock()
	m.retryCancels["qntx-qdrant"]()
	delete(m.retryCancels, "qntx-qdrant")
	m.mu.Unlock()
	close(gone)

	select {
	case <-ran:
		t.Fatal("launched again after the plugin was taken over")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestOnceGone_NodeShutdownIsNotLaunched(t *testing.T) {
	log := zap.NewNop().Sugar()
	m := NewPluginManager(log, log, "")
	ctx, cancel := context.WithCancel(context.Background())
	m.shutdownCtx = ctx
	gone := make(chan struct{})
	ran := make(chan struct{})
	m.onceGone("gaze", gone, func(context.Context) { close(ran) })
	cancel()
	close(gone)

	select {
	case <-ran:
		t.Fatal("launched again while the node shuts down")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestLaunchedPlugin_GoneAfterStopAndAfterItsOwnExit(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &launchedPlugin{cmd: cmd}
	select {
	case <-p.gone():
		t.Fatal("gone while the process runs")
	case <-time.After(100 * time.Millisecond):
	}
	if err := p.stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case <-p.gone():
	case <-time.After(time.Second):
		t.Fatal("not gone after stop")
	}

	quick := exec.Command("true")
	if err := quick.Start(); err != nil {
		t.Fatal(err)
	}
	q := &launchedPlugin{cmd: quick}
	select {
	case <-q.gone():
	case <-time.After(2 * time.Second):
		t.Fatal("a process that exited by itself is not gone")
	}
}
