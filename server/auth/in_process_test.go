package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// "or should it be in the log?"

func passed(h *Handler, ctx context.Context, reach Reach) (int, bool) {
	ran := false
	gated := h.Middleware("agent:say", reach, func(w http.ResponseWriter, r *http.Request) {
		_, ran = AdmissionFrom(r.Context())
		w.WriteHeader(http.StatusTeapot)
	})
	w := httptest.NewRecorder()
	gated(w, httptest.NewRequest(http.MethodPost, "/api/agent/say", nil).WithContext(ctx))
	return w.Code, ran
}

// What the node released for its agent is said to it in process, as ROOT,
// and the turn's ask passes the gate as that.
func TestTheNodeAskingItselfIsAdmittedAsItAsked(t *testing.T) {
	h := handlerWithDoors(t)
	code, ran := passed(h, InProcess(context.Background(), Admitted(LevelRoot)), Also(LevelRoot))
	assert.Equal(t, http.StatusTeapot, code)
	assert.True(t, ran, "the handler ran with no admission")
}

// It is held to the reach table as anyone is.
func TestTheNodeAskingItselfIsHeldToTheReachTable(t *testing.T) {
	h := handlerWithDoors(t)
	code, ran := passed(h, InProcess(context.Background(), Admitted(LevelToken)), Also(LevelRoot))
	assert.Equal(t, http.StatusForbidden, code)
	assert.False(t, ran)
}

// An admission put on a context, not as the node asking itself, presents
// nothing at the gate.
func TestAnAdmissionAloneIsNotTheNodeAskingItself(t *testing.T) {
	h := handlerWithDoors(t)
	code, ran := passed(h, WithAdmission(context.Background(), Admitted(LevelRoot)), Also(LevelRoot))
	assert.NotEqual(t, http.StatusTeapot, code)
	assert.False(t, ran)
}
