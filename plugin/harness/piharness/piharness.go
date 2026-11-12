// 「[Fable 5.2, Fable 5.1] Package piharness is Pi (earendil-works/pi) as a harness plugin runs it」
// 「[Fable 5.2, Fable 5.1] (ADR-059): one A2A turn is one run of Pi, its records streamed as it prints」
// 「[Fable 5.2, Fable 5.1] them, and its answer the Task completed. The loop is Pi's (ADR-048).」
package piharness

import (
	"context"
	"iter"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/teranos/QNTX/internal/pi"
	"github.com/teranos/QNTX/plugin/harness"
	errors "github.com/teranos/sacred-error"
)

// 「[Fable 5.2, Fable 5.1] Run is how one turn is run in Pi, as core sends it: Binary is the plugin's」
// 「[Fable 5.2, Fable 5.1] own to name, and Says the message's text.」
type Run = pi.Said

// 「[Fable 5.2, Fable 5.1] Record is one record Pi prints while a turn runs.」
type Record = pi.Event

// 「[Fable 5.2, Fable 5.1] Answer is what Pi says of a run that ended.」
type Answer = pi.Answer

// 「[Fable 5.2, Fable 5.1] Executor runs each turn in the Pi that Binary names.」
type Executor struct {
	// 「[Fable 5.2, Fable 5.1] Binary is where the plugin's Pi is, or why it has none.」
	Binary func(ctx context.Context) (string, error)

	running harness.Running
}

var _ a2asrv.AgentExecutor = (*Executor)(nil)

// 「[Fable 5.2, Fable 5.1] Handler is what a Pi harness plugin hands back as its Harness.」
func Handler(binary func(ctx context.Context) (string, error)) a2asrv.RequestHandler {
	return a2asrv.NewHandler(&Executor{Binary: binary}, a2asrv.WithTaskStore(harness.Turns(harness.TurnsKept)))
}

func (e *Executor) Execute(ctx context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
	return func(yield func(a2asdk.Event, error) bool) {
		var said Run
		says, err := harness.SaidIn(ec.Message, &said)
		if err != nil {
			yield(nil, errors.WithSecondaryError(a2asdk.ErrInvalidParams, err))
			return
		}
		binary, err := e.Binary(ctx)
		if err != nil {
			yield(nil, errors.Wrap(err, "this plugin has no Pi to run"))
			return
		}
		said.Binary, said.Says = binary, says

		// 「[Opus 5.5] How the turn is run carries what is handed to the process and」
		// 「[Opus 5.5] offered to no model (ADR-051): the Task keeps none of it.」
		// 「[Fable 5.2, Fable 5.1] a2a-go runs a turn apart from the call that asked for it, so a」
		// 「[Fable 5.2, Fable 5.1] caller that left does not end ctx: the run is stopped by Cancel,」
		// 「[Fable 5.2, Fable 5.1] which core asks for when it gives a turn up, and by the plugin's」
		// 「[Fable 5.2, Fable 5.1] server when the caller of the stream leaves. Both are in place」
		// 「[Fable 5.2, Fable 5.1] before the caller is told which Task this is. Pi is stopped the way」
		// 「[Fable 5.2, Fable 5.1] a person stops it, and its session is left as written.」
		running, stop := context.WithCancel(ctx)
		defer stop()
		defer e.running.Began(ec.TaskID, stop)()
		harness.StoppedWhenTheCallerLeaves(ctx, stop)

		submitted := a2asdk.NewSubmittedTask(ec, a2asdk.NewMessage(a2asdk.MessageRoleUser, a2asdk.NewTextPart(says)))
		if ec.StoredTask == nil && !yield(submitted, nil) {
			return
		}
		if running.Err() != nil {
			return
		}
		listening := true
		answer, err := said.Run(running, func(record Record) {
			if !listening {
				return
			}
			printed, err := harness.Printed(ec, record)
			if err == nil && !yield(printed, nil) {
				listening = false
				stop()
			}
		})
		if !listening {
			return
		}
		if err != nil {
			yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateFailed,
				a2asdk.NewMessage(a2asdk.MessageRoleAgent, a2asdk.NewTextPart(err.Error()))), nil)
			return
		}
		completed, err := harness.Answered(submitted, answer.Text, answer)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(completed, nil)
	}
}

// 「[Fable 5.2, Fable 5.1] Cancel stops the Task's run and ends the Task canceled.」
func (e *Executor) Cancel(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
	return func(yield func(a2asdk.Event, error) bool) {
		e.running.Stop(ec.TaskID)
		yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateCanceled, nil), nil)
	}
}
