package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	errors "github.com/teranos/sacred-error"
)

// "to me it feels like k should own my Media Assets"
// "all other nodes are peers of the primary"
// "just for clarification, the bytes dont go through Q to K but L to K right?"
//
// 「[Fable 5.2] A peer holds a file another peer is to hold. Only the primary knows the peers, and」
// 「[Fable 5.2] a peer admits only what its primary signed. So the peer asks the primary to vouch」
// 「[Fable 5.2] for it, signing as its did:key, and is handed the primary's signature that it is a」
// 「[Fable 5.2] PEER, made out to the other peer, for one request. The bytes go from the one peer」
// 「[Fable 5.2] to the other and never to the primary, as a peer fetches an archive from where the」
// 「[Fable 5.2] primary said GitHub keeps it (github_for_peers.go). No node speaks as a person.」

// filesForPeersPath is where a peer asks its primary to vouch for it to
// another peer it brings a file to.
const filesForPeersPath = "/auth/files"

// filesForPeersWithin bounds bringing one file to a peer.
const filesForPeersWithin = 2 * time.Minute

// peerAnswerMost is the most of an answer about a file that is read.
const peerAnswerMost = 1 << 20

// vouchedToPeer is what the primary answers a peer it vouches for.
type vouchedToPeer struct {
	// Authorization is what the one request to the peer goes with.
	Authorization string `json:"authorization"`
	// Origin is where the primary reaches that peer. The asking peer may know
	// a way of its own (nodes.direct).
	Origin string `json:"origin"`
}

// handleFilesForPeers answers a peer, signing as its did:key, with this
// node's signature that it is a peer, for the one request it brings a file to
// the peer it names with. No file comes here.
func (h *Handler) handleFilesForPeers(w http.ResponseWriter, r *http.Request) {
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
		h.logger.Infow("Vouching for a peer was refused", "client", r.PostFormValue("client_id"), "error", err)
		return
	}
	asker, ok := h.clientByDID(asked.GetID())
	if !ok || !asker.Node {
		h.writeError(w, http.StatusUnauthorized, "only a node is vouched for to a peer")
		return
	}
	to, known := h.peerNamed(r.PostForm.Get("to"))
	if !known {
		h.writeError(w, http.StatusNotFound, "no peer is called "+r.PostForm.Get("to"))
		return
	}
	if to.DID == asker.DID {
		h.writeError(w, http.StatusBadRequest, to.Label+" brings a file to itself")
		return
	}
	// The peer is told which node brings the file, at the one level a node holds.
	carriedAs, err := h.carriedSignature(asker.DID, asker.Label, LevelPeer, to, to.Namespaces, time.Now())
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "this node did not sign for "+asker.Label+" to "+to.Label+": "+err.Error())
		return
	}
	h.logger.Infow("A peer is vouched for to a peer", "from", asker.Label, "to", to.Label)
	h.writeJSON(w, http.StatusOK, vouchedToPeer{Authorization: carriedAs, Origin: to.Origin.String()})
}

// SetDirect names where this node reaches a peer itself (nodes.direct), by
// the name the primary calls it. A peer with no way named here is reached
// where the primary reaches it.
func (h *Handler) SetDirect(direct map[string]string) error {
	h.direct = map[string]string{}
	for name, origin := range direct {
		trimmed, err := anOrigin(origin)
		if err != nil {
			return errors.Wrapf(err, "nodes.direct names %s", name)
		}
		h.direct[name] = trimmed
	}
	return nil
}

// anOrigin is origin as scheme, host and port alone, or why it is not one. A
// path, query, fragment or user in it would move where /api/files lands.
func anOrigin(origin string) (string, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(origin), "/")
	parsed, err := url.Parse(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return "", errors.Newf("%q is not an origin such as http://node.example.com:8770", origin)
	}
	return trimmed, nil
}

// toPeers is how a node speaks to its primary and its peers about a file: it
// follows no redirect, so the assertion and the file go only where they were sent.
var toPeers = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// BringFileToPeer brings a file this node holds to the peer the primary calls
// to. The primary vouches that this node is a peer; the bytes go from here to
// that peer. It answers what the peer said of the file, and its status. A node
// with no primary knows no peer to bring it to.
func (h *Handler) BringFileToPeer(ctx context.Context, to, filename string, content []byte) (int, []byte, error) {
	if !h.logsInElsewhere {
		return 0, nil, errors.Newf("this node has no primary, so it knows no peer called %s to bring %s to", to, filename)
	}
	ctx, cancel := context.WithTimeout(ctx, filesForPeersWithin)
	defer cancel()

	vouched, err := h.vouchedTo(ctx, to)
	if err != nil {
		return 0, nil, err
	}
	origin, err := anOrigin(vouched.Origin)
	if err != nil {
		return 0, nil, errors.Wrapf(err, "the primary named no origin for %s", to)
	}
	if direct, named := h.direct[to]; named {
		origin = direct
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		return 0, nil, errors.Wrapf(err, "the form did not take %s", filename)
	}
	if _, err := part.Write(content); err != nil {
		return 0, nil, errors.Wrapf(err, "the form did not take the bytes of %s", filename)
	}
	if err := form.Close(); err != nil {
		return 0, nil, errors.Wrapf(err, "the form carrying %s did not close", filename)
	}
	at := origin + "/api/files"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, at, &body)
	if err != nil {
		return 0, nil, errors.Wrapf(err, "%s is not a request %s answers", at, to)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", vouched.Authorization)
	resp, err := toPeers.Do(req)
	if err != nil {
		return 0, nil, errors.Wrapf(err, "%s did not answer at %s", to, at)
	}
	said, readErr := io.ReadAll(io.LimitReader(resp.Body, peerAnswerMost))
	if closeErr := resp.Body.Close(); closeErr != nil {
		return 0, nil, errors.Wrapf(closeErr, "the answer of %s at %s did not close", to, at)
	}
	if readErr != nil {
		return 0, nil, errors.Wrapf(readErr, "the answer of %s at %s did not read", to, at)
	}
	h.logger.Infow("A file was brought to a peer", "to", to, "at", at, "filename", filename, "bytes", len(content), "answered", resp.StatusCode)
	return resp.StatusCode, said, nil
}

// vouchedTo asks the primary to vouch for this node to the peer it calls to,
// signing as this node.
func (h *Handler) vouchedTo(ctx context.Context, to string) (vouchedToPeer, error) {
	did, signed, err := h.asserted(h.loginAt, time.Now())
	if err != nil {
		return vouchedToPeer{}, err
	}
	form := url.Values{}
	form.Set("client_id", did)
	form.Set("client_assertion_type", clientAssertionType)
	form.Set("client_assertion", signed)
	form.Set("to", to)
	at := h.loginAt + filesForPeersPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, at, strings.NewReader(form.Encode()))
	if err != nil {
		return vouchedToPeer{}, errors.Wrapf(err, "%s is not a request the primary answers", at)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := toPeers.Do(req)
	if err != nil {
		return vouchedToPeer{}, errors.Wrapf(err, "%s did not answer", at)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, peerAnswerMost))
	if closeErr := resp.Body.Close(); closeErr != nil {
		return vouchedToPeer{}, errors.Wrapf(closeErr, "the primary's answer at %s did not close", at)
	}
	if readErr != nil {
		return vouchedToPeer{}, errors.Wrapf(readErr, "the primary's answer at %s did not read", at)
	}
	if resp.StatusCode != http.StatusOK {
		return vouchedToPeer{}, errors.Newf("%s answered %d for %s: %s", at, resp.StatusCode, to, body)
	}
	var vouched vouchedToPeer
	if err := json.Unmarshal(body, &vouched); err != nil {
		return vouchedToPeer{}, errors.Wrapf(err, "the primary's answer for %s did not read: %s", to, body)
	}
	if vouched.Authorization == "" || vouched.Origin == "" {
		return vouchedToPeer{}, errors.Newf("the primary vouched for nothing to %s: %s", to, body)
	}
	return vouched, nil
}
