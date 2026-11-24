package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/teranos/QNTX/internal/admission"
)

// A request refused for reach tells the node the route and who was refused,
// so the reach it lacked waits on the person; the refusal is still a refusal.
func TestARefusalForReachTellsTheNodeWhatWasLacked(t *testing.T) {
	h := &Handler{logger: zap.NewNop().Sugar()}
	var told []string
	h.SetOutOfReach(func(admitted Admission, route string) {
		told = append(told, route, admitted.LevelName(), admitted.Identity)
	})
	refused := admission.Granted(Admission{Identity: "apple:person"}, LevelToken, nil, false, admission.Words{})
	w := httptest.NewRecorder()
	h.rejectOutOfReach(w, httptest.NewRequest(http.MethodGet, "/api/lines/scene", nil), refused, "lines:scene", Also(LevelSuper))

	assert.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, []string{"lines:scene", string(LevelToken), "apple:person"}, told)
}
