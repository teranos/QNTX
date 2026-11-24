package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// "yeah, because right now, i dont even see the outgoing towards node agent in ag right? yeah thats fucked up as well"
//
// What is said to this node's own agent is collected by laye, where the person
// sees it, beside everything else waiting on them. Released, or due, laye
// hands it back here, and the node runs it as the agent's turn.

// layeDeliveredPath is where laye hands what was released for this node's agent.
const layeDeliveredPath = "/auth/laye/delivered"

// LayeDelivered is one delivery laye hands: whose, by whom, and what is said,
// as the one thing laye made of what was collected.
type LayeDelivered struct {
	DID   string   `json:"did"`
	To    string   `json:"to"`
	From  string   `json:"from"`
	Says  string   `json:"says"`
	Tasks []string `json:"tasks"`
}

// SetLayeDelivered has the node run what laye hands for its agent, answering
// the Task its turn is.
func (h *Handler) SetLayeDelivered(run func(ctx context.Context, delivered LayeDelivered) (string, error)) {
	h.layeDelivered = run
}

func (h *Handler) handleLayeDelivered(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, layeDeliveredPath+" answers POST")
		return
	}
	if err := h.layeAsking(r, time.Now()); err != nil {
		h.writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if h.layeDelivered == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node runs no agent laye delivers to")
		return
	}
	var delivered LayeDelivered
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&delivered); err != nil {
		h.writeError(w, http.StatusBadRequest, "laye hands {did, to, from, says, tasks}: "+err.Error())
		return
	}
	if delivered.DID == "" || strings.TrimSpace(delivered.Says) == "" {
		h.writeError(w, http.StatusBadRequest, "did and says are each named")
		return
	}
	task, err := h.layeDelivered(r.Context(), delivered)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.logger.Infow("What laye released for this node's agent is said to it, as a Task", "to", delivered.To, "by", delivered.From, "task", task)
	h.writeJSON(w, http.StatusOK, map[string]string{"task": task})
}
