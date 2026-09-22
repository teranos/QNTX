package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/errors"
)

type recordingHandler struct {
	name   string
	got    *types.As
	refuse error
}

func (h *recordingHandler) Name() string { return h.name }
func (h *recordingHandler) Execute(_ context.Context, job *async.Job) error {
	var as types.As
	if err := json.Unmarshal(job.Payload, &as); err != nil {
		return err
	}
	h.got = &as
	return h.refuse
}

// The watcher names a handler; the registry holds it; the attestation is the
// job's payload, whole.
func TestBuiltinExecutorReachesTheRegisteredHandler(t *testing.T) {
	reg := async.NewHandlerRegistry()
	h := &recordingHandler{name: "test.builtin"}
	reg.Register(h)

	var failed []HandlerFailure
	ex := &builtinExecutor{registry: reg, noteFailure: func(f HandlerFailure) { failed = append(failed, f) }}

	as := &types.As{ID: "as-1", Predicates: []string{"p"}, Attributes: map[string]interface{}{"k": "v"}}
	if err := ex.ExecuteBuiltin(context.Background(), "test.builtin", as); err != nil {
		t.Fatalf("ExecuteBuiltin: %v", err)
	}
	if h.got == nil || h.got.ID != "as-1" || h.got.Attributes["k"] != "v" {
		t.Fatalf("handler got %+v", h.got)
	}
	if len(failed) != 0 {
		t.Fatalf("a success was noted as a failure: %+v", failed)
	}
}

// A name the registry does not hold is an error the watcher records. Nothing
// is queued for later: a built-in is compiled in or it is not.
func TestBuiltinExecutorRefusesAnUnknownName(t *testing.T) {
	ex := &builtinExecutor{registry: async.NewHandlerRegistry(), noteFailure: func(HandlerFailure) {}}
	if err := ex.ExecuteBuiltin(context.Background(), "nobody.home", &types.As{ID: "as-2"}); err == nil {
		t.Fatal("an unknown built-in was accepted")
	}
}

// A handler that fails is on the row the way a scheduled one is: the failure
// log is the same log.
func TestBuiltinExecutorNotesAFailureForTheRow(t *testing.T) {
	reg := async.NewHandlerRegistry()
	reg.Register(&recordingHandler{name: "test.fails", refuse: errors.New("gh: not found")})

	var failed []HandlerFailure
	ex := &builtinExecutor{registry: reg, noteFailure: func(f HandlerFailure) { failed = append(failed, f) }}

	err := ex.ExecuteBuiltin(context.Background(), "test.fails", &types.As{ID: "as-3"})
	if err == nil {
		t.Fatal("the handler's error was swallowed")
	}
	if len(failed) != 1 || failed[0].Handler != "test.fails" || failed[0].Error == "" {
		t.Fatalf("failure not noted for the row: %+v", failed)
	}
}
