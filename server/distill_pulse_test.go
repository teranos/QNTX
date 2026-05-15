package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

type mockDistiller struct {
	distilled     int
	sigmasCreated int
	skipped       int
	err           error
	called        bool
	cutoff        string
	batchSize     int
}

func (m *mockDistiller) AgeDistill(cutoffRFC3339 string, batchSize int) (int, int, int, error) {
	m.called = true
	m.cutoff = cutoffRFC3339
	m.batchSize = batchSize
	return m.distilled, m.sigmasCreated, m.skipped, m.err
}

func TestDistillHandler_CallsDistiller(t *testing.T) {
	mock := &mockDistiller{
		distilled:     10,
		sigmasCreated: 1,
		skipped:       2,
	}

	srv := &QNTXServer{ageDistiller: mock}
	h := &distillHandler{
		server:    srv,
		maxAge:    1 * time.Hour,
		batchSize: 500,
		logger:    zap.NewNop().Sugar(),
	}

	err := h.Execute(context.Background(), &async.Job{})
	require.NoError(t, err)
	assert.True(t, mock.called)
	assert.Equal(t, 500, mock.batchSize)

	// Cutoff should be approximately 1 hour ago in RFC3339
	cutoff, parseErr := time.Parse(time.RFC3339, mock.cutoff)
	require.NoError(t, parseErr)
	assert.WithinDuration(t, time.Now().UTC().Add(-1*time.Hour), cutoff, 5*time.Second)
}

func TestDistillHandler_NilDistiller(t *testing.T) {
	srv := &QNTXServer{} // ageDistiller is nil
	h := &distillHandler{
		server:    srv,
		maxAge:    1 * time.Hour,
		batchSize: 500,
		logger:    zap.NewNop().Sugar(),
	}

	err := h.Execute(context.Background(), &async.Job{})
	require.NoError(t, err)
}

func TestDistillHandler_PropagatesError(t *testing.T) {
	mock := &mockDistiller{
		err: assert.AnError,
	}

	srv := &QNTXServer{ageDistiller: mock}
	h := &distillHandler{
		server:    srv,
		maxAge:    2 * time.Hour,
		batchSize: 100,
		logger:    zap.NewNop().Sugar(),
	}

	err := h.Execute(context.Background(), &async.Job{})
	assert.ErrorIs(t, err, assert.AnError)
}

func TestDistillHandler_Name(t *testing.T) {
	h := &distillHandler{}
	assert.Equal(t, "distill", h.Name())
}
