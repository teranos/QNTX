package plugin

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBase(name string) *Base {
	b := NewBase(Metadata{
		Name:        name,
		Version:     "1.0.0",
		QNTXVersion: ">= 0.1.0",
		Description: "test plugin",
		Author:      "test",
		License:     "MIT",
	})
	b.Init(newMockServiceRegistry())
	return &b
}

func TestBase_Metadata(t *testing.T) {
	b := newTestBase("test")
	meta := b.Metadata()

	assert.Equal(t, "test", meta.Name)
	assert.Equal(t, "1.0.0", meta.Version)
	assert.Equal(t, "MIT", meta.License)
}

func TestBase_Services(t *testing.T) {
	services := newMockServiceRegistry()
	b := NewBase(Metadata{Name: "test"})
	b.Init(services)

	assert.Equal(t, services, b.Services())
}

func TestBase_PauseResume(t *testing.T) {
	b := newTestBase("test")
	ctx := context.Background()

	// Starts unpaused
	assert.False(t, b.IsPaused())

	// Pause succeeds
	require.NoError(t, b.Pause(ctx))
	assert.True(t, b.IsPaused())

	// Resume succeeds
	require.NoError(t, b.Resume(ctx))
	assert.False(t, b.IsPaused())
}

func TestBase_DoublePauseErrors(t *testing.T) {
	b := newTestBase("myplugin")
	ctx := context.Background()

	require.NoError(t, b.Pause(ctx))

	err := b.Pause(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "myplugin")
	assert.Contains(t, err.Error(), "already paused")
}

func TestBase_DoubleResumeErrors(t *testing.T) {
	b := newTestBase("myplugin")
	ctx := context.Background()

	err := b.Resume(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "myplugin")
	assert.Contains(t, err.Error(), "not paused")
}

func TestBase_HealthReflectsPauseState(t *testing.T) {
	b := newTestBase("test")
	ctx := context.Background()

	// Healthy when running
	status := b.Health(ctx)
	assert.True(t, status.Healthy)
	assert.False(t, status.Paused)
	assert.Contains(t, status.Message, "operational")

	// Healthy but paused after Pause
	require.NoError(t, b.Pause(ctx))
	status = b.Health(ctx)
	assert.True(t, status.Healthy)
	assert.True(t, status.Paused)
	assert.Contains(t, status.Message, "paused")

	// Back to operational after Resume
	require.NoError(t, b.Resume(ctx))
	status = b.Health(ctx)
	assert.False(t, status.Paused)
	assert.Contains(t, status.Message, "operational")
}

func TestBase_HealthMessageIncludesPluginName(t *testing.T) {
	b := newTestBase("github")
	ctx := context.Background()

	status := b.Health(ctx)
	assert.Contains(t, status.Message, "github")
}

func TestBase_Shutdown(t *testing.T) {
	b := newTestBase("test")
	err := b.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestBase_RegisterWebSocket(t *testing.T) {
	b := newTestBase("test")
	handlers, err := b.RegisterWebSocket()
	assert.NoError(t, err)
	assert.Nil(t, handlers)
}

func TestBase_ConcurrentPauseResume(t *testing.T) {
	b := newTestBase("test")
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.Pause(ctx)
			_ = b.Resume(ctx)
			_ = b.IsPaused()
			_ = b.Health(ctx)
		}()
	}
	wg.Wait()

	// Should be in a valid state — not panicked, not deadlocked
	_ = b.IsPaused()
}
