package so

import "context"

// Job is the part of an executor's job that an action handler sees. ATS owns
// this contract; whatever runs the action (Pulse, in QNTX) implements it.
type Job interface {
	// ID identifies the job in errors and logs.
	ID() string
	// Payload is the encoded Payload the action produced.
	Payload() []byte
	// SetTotal declares how many units of work the job has.
	SetTotal(total int)
	// SetProgress records how many units are done.
	SetProgress(done int)
	// SetResult records the job's result as JSON.
	SetResult(resultJSON string)
}

// Handler executes one kind of action's jobs.
type Handler interface {
	// Name is the name an executor routes jobs to this handler by.
	Name() string
	// Execute runs the job. It must stop when ctx is cancelled.
	Execute(ctx context.Context, job Job) error
}
