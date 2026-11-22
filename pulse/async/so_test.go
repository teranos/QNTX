package async

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/so"
)

type soCounter struct{}

func (soCounter) Name() string { return "so.count" }

func (soCounter) Execute(_ context.Context, job so.Job) error {
	job.SetTotal(2)
	job.SetProgress(2)
	job.SetResult(job.ID() + ":" + string(job.Payload()))
	return nil
}

func TestAnSoHandlerRunsOnAPulseJob(t *testing.T) {
	h := ForSo(soCounter{})
	assert.Equal(t, "so.count", h.Name())

	job := &Job{ID: "j1", Payload: []byte(`{"a":1}`)}
	require.NoError(t, h.Execute(context.Background(), job))

	assert.Equal(t, Progress{Current: 2, Total: 2}, job.Progress)
	assert.Equal(t, `j1:{"a":1}`, job.Source)
}
