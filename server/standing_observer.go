package server

import (
	"context"
	"encoding/json"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/server/namespaces"
	"go.uber.org/zap"
)

// The standing table, run for whichever namespace a row lands in.
//
// A stored watcher is one namespace's: "a watcher in namespace A does not fire
// on an attestation in namespace B" (ADR-026), and the engine that runs them
// observes the namespace it serves. A standing row is "what every namespace
// watches before anybody makes a watcher" (ats/watcher/standing.go). Those are
// two different things, and they run on two different paths: the engine runs
// stored watchers, this runs standing built-ins, and each namespace registers
// this as one of the steps it runs when it starts.
//
// Why it exists: the ground token is an ATTESTOR in the Ground namespace, so
// every push it streamed landed there, the engine watching default never saw
// one, and the row said nothing for a day.
type standingObserver struct {
	ctx context.Context
	// Looked up at fire time: a namespace starts before the daemon is wired,
	// and the executor it will need does not exist yet when it registers.
	builtins func() watcher.BuiltinExecutor
	// Where a row that matched and could not be run is put: the status row,
	// not only the log, which is ROOT's to read.
	noteFailure func(HandlerFailure)
	logger      *zap.SugaredLogger
}

func (o *standingObserver) failed(handler, why string, as *types.As) {
	if o.noteFailure == nil {
		return
	}
	o.noteFailure(HandlerFailure{Handler: handler, ExecutionID: "standing:" + as.ID, Error: why})
}

// OnAttestationCreated runs every standing built-in whose filter names the
// row. A standing tell is the engine's to broadcast and is not run here.
func (o *standingObserver) OnAttestationCreated(as *types.As) {
	if o == nil || as == nil {
		return
	}
	for _, w := range watcher.Standing() {
		if w.ActionType != storage.ActionTypeBuiltinExecute {
			continue
		}
		if !watcher.StandingMatches(as, w) {
			continue
		}
		var action watcher.BuiltinExecuteAction
		if err := json.Unmarshal([]byte(w.ActionData), &action); err != nil || action.HandlerName == "" {
			o.logger.Errorw("A standing row names no built-in to run",
				"watcher_id", w.ID, "action_data", w.ActionData, "error", err)
			continue
		}
		exec := o.builtins()
		if exec == nil {
			o.logger.Errorw("A standing row matched and nothing is wired to run its built-in",
				"watcher_id", w.ID, "attestation_id", as.ID, "handler", action.HandlerName)
			o.failed(action.HandlerName, "a push matched the standing row and no executor was wired to run it", as)
			continue
		}
		if err := exec.ExecuteBuiltin(o.ctx, action.HandlerName, as); err != nil {
			// The executor has noted it for the row already; this is the log's copy.
			o.logger.Warnw("A standing built-in failed",
				"watcher_id", w.ID, "attestation_id", as.ID, "handler", action.HandlerName, "error", err)
		}
	}
}

// standingSubsystem is the step a namespace runs when it starts: its
// attestations reach the standing table from then on.
type standingSubsystem struct{}

func (standingSubsystem) Name() string { return "standing-watchers" }

func (standingSubsystem) Start(u *namespaces.Universe) error {
	s := GetDefaultServer()
	if s == nil || u == nil {
		return nil
	}
	if s.standing == nil {
		s.standing = &standingObserver{
			ctx: s.ctx,
			builtins: func() watcher.BuiltinExecutor {
				if s.builtin == nil {
					return nil
				}
				return s.builtin
			},
			noteFailure: s.noteHandlerFailure,
			logger:      s.logger.Named("standing"),
		}
	}
	storage.RegisterObserver(u.Name(), s.standing)
	return nil
}
