package async

import (
	"context"

	"github.com/teranos/QNTX/ats/so"
)

// ForSo is the JobHandler that runs an ATS action's handler. ATS defines
// so.Handler; Pulse runs it by giving it a so.Job over the Pulse job.
func ForSo(h so.Handler) JobHandler {
	return soHandler{h}
}

type soHandler struct{ so.Handler }

func (s soHandler) Execute(ctx context.Context, job *Job) error {
	return s.Handler.Execute(ctx, soJob{job})
}

// soJob is a Pulse job as an ATS action handler sees it.
type soJob struct{ job *Job }

func (j soJob) ID() string        { return j.job.ID }
func (j soJob) Payload() []byte   { return j.job.Payload }
func (j soJob) SetTotal(n int)    { j.job.Progress.Total = n }
func (j soJob) SetProgress(n int) { j.job.UpdateProgress(n) }
func (j soJob) SetResult(s string) {
	j.job.Source = s
}
