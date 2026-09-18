package sigil

import (
	"bytes"
	"context"
	"net/http"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
)

// A Gate is what stands between whoever is asking and every sigil: the auth
// middleware, in the shape reach.Gate hands it around. It reads the credential
// off a request and lets the next handler run for an admission the row admits.
type Gate func(route string, reaching auth.Reach, next http.HandlerFunc) http.HandlerFunc

// An Asking is one sigil about to be asked over one surface: what answers it,
// who is asking, and who the lines say reaches it. Package server fills it in;
// Ask is the only way from here to an answer.
type Asking struct {
	// Surface is the way in, "http" or "mcp": what the lines name it by, and
	// what the gate counts it under.
	Surface string
	Signum  string
	Sigil   *protocol.Sigil
	Answer  Answer
	Gate    Gate
	// Reaching is who the lines say reaches this sigil over this surface, and
	// Anyone says the lines serve it without asking who is calling.
	Reaching auth.Reach
	Anyone   bool
	// Caller is the request that carried the asking, which is what the gate
	// reads the credential off: the HTTP request itself, or the MCP request.
	Caller *http.Request
}

// Asked is how an asking ended: with the answer, with the sigil's refusal, or
// turned away at the gate. Exactly one is set.
type Asked struct {
	Answer  any
	Refusal *protocol.Refusal
	// Rejected is the gate's own answer, whole, for the surface to hand on in
	// its form: over HTTP as it is, over MCP as a tool error. It is not the
	// sigil's refusal, because the sigil was never reached.
	Rejected *Rejected
}

// Rejected is what the gate wrote when it turned somebody away.
type Rejected struct {
	Status int
	Header http.Header
	Body   string
}

// Route is what the gate counts this asking under and what a reach line names
// it by: the surface, the signum, the sigil.
func (a Asking) Route() string {
	return a.Surface + ":" + a.Signum + ":" + a.Sigil.GetName()
}

// Ask is one sigil asked. The gate first, with the row the lines give the
// sigil over this surface and the credential the caller carried; then what
// arrived is read and refused; then the function answers, handed who the gate
// admitted. Every surface asks here, so no surface can answer around the gate.
func (a Asking) Ask(ctx context.Context, arrived map[string]any) Asked {
	var asked *Asked
	answers := func(_ http.ResponseWriter, r *http.Request) {
		sent, refusal := Read(a.Sigil, arrived)
		if refusal != nil {
			asked = &Asked{Refusal: refusal}
			return
		}
		if refusal := Refuses(a.Sigil, sent); refusal != nil {
			asked = &Asked{Refusal: refusal}
			return
		}
		answer, refusal := a.Answer(r.Context(), sent)
		asked = &Asked{Answer: answer, Refusal: refusal}
	}
	if !a.Anyone {
		answers = a.Gate(a.Route(), a.Reaching, answers)
	}

	wrote := &rejected{header: http.Header{}}
	answers(wrote, a.Caller.WithContext(ctx))
	if asked == nil {
		// The gate answered and the sigil never did.
		return Asked{Rejected: &Rejected{Status: wrote.status, Header: wrote.header, Body: wrote.body.String()}}
	}
	return *asked
}

// rejected holds what the gate wrote, so it can be handed on rather than
// written to whatever connection carried the asking.
type rejected struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *rejected) Header() http.Header { return w.header }

func (w *rejected) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *rejected) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(b)
}
