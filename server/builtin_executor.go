package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/errors"
)

// builtinExecutor is what a builtin_execute watcher reaches: the daemon's own
// registry, by name. A handler here is compiled into this node, so there is
// nothing to wait for — it is held or the name is wrong.
type builtinExecutor struct {
	registry    *async.HandlerRegistry
	noteFailure func(HandlerFailure)
}

// ExecuteBuiltin runs the named handler with the attestation as the job's
// payload, in the caller's goroutine. The watcher engine already runs each
// fire on its own, so a handler that waits on a CI run waits there.
func (b *builtinExecutor) ExecuteBuiltin(ctx context.Context, handlerName string, as *types.As) error {
	if b == nil || b.registry == nil {
		return errors.New("no handler registry to reach a built-in through")
	}
	handler := b.registry.Get(handlerName)
	if handler == nil {
		return errors.Wrapf(async.ErrHandlerNotRegistered, "no built-in named %s", handlerName)
	}

	payload, err := json.Marshal(as)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal attestation %s for built-in %s", as.ID, handlerName)
	}

	now := time.Now()
	job := &async.Job{
		ID:          "builtin:" + handlerName + ":" + as.ID,
		HandlerName: handlerName,
		Payload:     payload,
		Source:      "watcher",
		Status:      async.JobStatusRunning,
		CreatedAt:   now,
		StartedAt:   &now,
		UpdatedAt:   now,
	}

	start := time.Now()
	if err := handler.Execute(ctx, job); err != nil {
		// The row reads this the way it reads a scheduled handler's failure:
		// same log, same slot.
		if b.noteFailure != nil {
			b.noteFailure(HandlerFailure{
				Handler:     handlerName,
				ExecutionID: job.ID,
				Error:       err.Error(),
				Details:     errors.GetAllDetails(err),
				DurationMs:  int(time.Since(start).Milliseconds()),
			})
		}
		return err
	}
	return nil
}
