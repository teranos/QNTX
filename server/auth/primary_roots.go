package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	errors "github.com/teranos/sacred-error"
)

// "the root identities administer the laye or mycelia network, however we want to call it"
// "and QNTX and a QNTX Node Namespaces, likewise delegate this out to laye"

// rootsPath is where laye says who administers the network.
const rootsPath = "/laye/roots"

// RootsEvery is how often a node whose primary is laye asks it for the roots.
const RootsEvery = 30 * time.Second

// RefreshPrimaryRoots asks the primary who the roots are, signing as this
// node, and goes by them alongside any am.toml still names. A primary that is
// not laye has no such answer, and that is no failure. A primary that does
// not answer is said, and the roots last heard stand.
func (h *Handler) RefreshPrimaryRoots(now time.Time) error {
	if !h.logsInElsewhere {
		return nil
	}
	did, signed, err := h.asserted(h.loginAt, now)
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("client_id", did)
	form.Set("client_assertion_type", clientAssertionType)
	form.Set("client_assertion", signed)
	client := http.Client{Timeout: issuerTimeout}
	resp, err := client.PostForm(h.loginAt+rootsPath, form)
	if err != nil {
		return errors.Wrapf(err, "%s did not answer", h.loginAt+rootsPath)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, issuerAnswerBytes))
	if closeErr := resp.Body.Close(); closeErr != nil {
		return errors.Wrap(closeErr, "the roots answer did not close")
	}
	if readErr != nil {
		return errors.Wrap(readErr, "the roots answer did not read")
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return errors.Newf("%s answered %d: %s", h.loginAt+rootsPath, resp.StatusCode, body)
	}
	var said struct {
		RootIdentities []string `json:"root_identities"`
	}
	if err := json.Unmarshal(body, &said); err != nil {
		return errors.Wrapf(err, "the roots answer is not a list of roots: %s", body)
	}
	h.identities.setFromPrimary(said.RootIdentities)
	return nil
}
