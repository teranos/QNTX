package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/teranos/QNTX/internal/slug"
	errors "github.com/teranos/sacred-error"
)

// "do you think k could keep a trusted cached copy ?"
// The primary is the only writer of a line. A peer asks it for the ones about
// the namespaces it serves, keeps the answer, and answers from it.

// linesPath is where a node asks the primary for its lines. A peer still asking
// at /auth/lines, which answered one namespace, is answered nothing there and
// keeps the lines it last heard.
const linesPath = "/auth/lines/served"

// LinesEvery is how often a peer asks again, and so the longest a revoke on the
// primary still acts here.
const LinesEvery = 30 * time.Second

// heldLines is what the primary said: the grants in each namespace a peer
// serves, and what the roles those grants name may say.
type heldLines struct {
	Namespaces []namespaceLines `json:"namespaces"`
	Words      []WordLine       `json:"words"`
}

// namespaceLines is the grants in one namespace.
type namespaceLines struct {
	Namespace string     `json:"namespace"`
	Roles     []RoleLine `json:"roles"`
}

// handleLines answers a peer, signing as its did:key, with the lines about the
// namespaces it was declared serving and nothing else.
func (h *Handler) handleLines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.writeError(w, http.StatusBadRequest, "the form did not read: "+err.Error())
		return
	}
	if r.PostForm.Get("client_assertion_type") != clientAssertionType {
		h.writeError(w, http.StatusUnauthorized, "a node asks by signing as its did:key, and this carried no client assertion")
		return
	}
	asked, err := h.assertedClient(r.PostForm, time.Now())
	if err != nil {
		h.writeError(w, http.StatusUnauthorized, "the asking node did not prove itself")
		h.logger.Infow("Lines refused", "client", r.PostFormValue("client_id"), "error", err)
		return
	}
	asker, ok := h.clientByDID(asked.GetID())
	if !ok || !asker.Node {
		h.writeError(w, http.StatusUnauthorized, "only a node asks for lines")
		return
	}
	h.writeJSON(w, http.StatusOK, h.linesAbout(asker.Namespaces))
}

// linesAbout is every grant in each namespace served, and every word line
// naming a role one of them names. A word line about roles nobody there holds
// is not the peer's business.
func (h *Handler) linesAbout(served []string) heldLines {
	answer := heldLines{Namespaces: make([]namespaceLines, 0, len(served))}
	var named []string
	for _, namespace := range served {
		roles := h.roleLines(namespace)
		answer.Namespaces = append(answer.Namespaces, namespaceLines{Namespace: namespace, Roles: roles})
		for _, line := range roles {
			named = append(named, line.Roles...)
		}
	}
	for _, line := range h.wordLines() {
		if slices.ContainsFunc(line.Roles, func(role string) bool { return slices.Contains(named, role) }) {
			answer.Words = append(answer.Words, line)
		}
	}
	return answer
}

// PrimaryLines is a peer's copy of what the primary said, read as its lines.
// Until the primary has answered once there is no copy, and that is said.
type PrimaryLines struct {
	h     *Handler
	mu    sync.Mutex
	heard bool
	held  heldLines
}

// LinesFromThePrimary is the reader a peer reads its lines through, or false
// on a node with no primary, whose own store holds its lines.
func (h *Handler) LinesFromThePrimary() (*PrimaryLines, bool) {
	if !h.logsInElsewhere {
		return nil, false
	}
	return &PrimaryLines{h: h}, true
}

// RoleLines is the primary's grants, in a namespace it said they are about.
// Any other namespace holds none here.
func (p *PrimaryLines) RoleLines(namespace string) ([]RoleLine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.heard {
		return nil, errors.Newf("%s has not said what the lines are yet", p.h.loginAt)
	}
	at := slices.IndexFunc(p.held.Namespaces, func(held namespaceLines) bool {
		return slug.Of(held.Namespace) == slug.Of(namespace)
	})
	if at < 0 {
		return []RoleLine{}, nil
	}
	return p.held.Namespaces[at].Roles, nil
}

// WordLines is what the roles the primary granted here may say.
func (p *PrimaryLines) WordLines() ([]WordLine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.heard {
		return nil, errors.Newf("%s has not said what the lines are yet", p.h.loginAt)
	}
	return p.held.Words, nil
}

// Refresh asks the primary again. A primary that does not answer leaves the
// copy as it was: this node keeps going on what it last heard.
func (p *PrimaryLines) Refresh(now time.Time) error {
	answer, err := p.h.askLines(now)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.held, p.heard = answer, true
	p.mu.Unlock()
	p.h.ForgetRoles()
	return nil
}

// askLines asks the primary for this node's lines, signing as this node.
func (h *Handler) askLines(now time.Time) (heldLines, error) {
	did, signed, err := h.asserted(h.loginAt, now)
	if err != nil {
		return heldLines{}, err
	}
	form := url.Values{}
	form.Set("client_id", did)
	form.Set("client_assertion_type", clientAssertionType)
	form.Set("client_assertion", signed)
	client := http.Client{Timeout: issuerTimeout}
	resp, err := client.PostForm(h.loginAt+linesPath, form)
	if err != nil {
		return heldLines{}, errors.Wrapf(err, "%s did not answer", h.loginAt+linesPath)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, issuerAnswerBytes))
	if closeErr := resp.Body.Close(); closeErr != nil {
		return heldLines{}, errors.Wrap(closeErr, "the lines did not close")
	}
	if readErr != nil {
		return heldLines{}, errors.Wrap(readErr, "the lines did not read")
	}
	if resp.StatusCode != http.StatusOK {
		return heldLines{}, errors.Newf("%s answered %d: %s", h.loginAt+linesPath, resp.StatusCode, body)
	}
	var answer heldLines
	if err := json.Unmarshal(body, &answer); err != nil {
		return heldLines{}, errors.Wrapf(err, "the answer is not lines: %s", body)
	}
	return answer, nil
}
