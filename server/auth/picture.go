package auth

import (
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/teranos/errors"
)

// "the user has a small picture via its auth, did you know that?"

// The picture is a URL at the provider. The browser never fetches it there:
// the page's CSP lets images come from the node alone, and a fetch from the
// browser would hand the provider the viewer's address at every page load.
// The node fetches it once, through the same guarded client the ceremony
// used, and answers it as its own.

// pictureTTL is how long a fetched picture is answered from memory before the
// provider is asked again.
const pictureTTL = 24 * time.Hour

// maxPictureBytes bounds what a provider may hand back as a picture.
const maxPictureBytes = 2 << 20

type heldPictureBytes struct {
	body        []byte
	contentType string
	fetchedAt   time.Time
}

// pictureBytes is what the node holds of each picture URL it has fetched.
var pictureBytes sync.Map

// pictureClient is the guarded provider client everywhere except the test
// binary, which points it at httptest's own.
var pictureClient = providerClient

// HandlePicture answers the picture of the User this request's admission
// resolved to, as the node's own image.
// GET /i/picture
func (h *Handler) HandlePicture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	admitted, gated := AdmissionFrom(r.Context())
	if !gated {
		h.writeError(w, http.StatusInternalServerError, "this route was served without a gate")
		return
	}
	u, status, err := h.theUser(admitted)
	if err != nil {
		h.writeError(w, status, err.Error())
		return
	}
	src := u.Picture()
	if src == "" {
		h.writeError(w, http.StatusNotFound, "no provider has shown this person")
		return
	}

	held, err := fetchPicture(src)
	if err != nil {
		h.logger.Warnw("the picture a provider showed was not fetched", "user", u.ID, "error", err)
		h.writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", held.contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(held.body); err != nil {
		h.logger.Warnw("the picture was not delivered", "user", u.ID, "error", err)
	}
}

// fetchPicture is the bytes at a picture URL, from memory when they were
// fetched within pictureTTL, and from the provider otherwise.
func fetchPicture(src string) (heldPictureBytes, error) {
	if val, ok := pictureBytes.Load(src); ok {
		held := val.(heldPictureBytes)
		if time.Since(held.fetchedAt) < pictureTTL {
			return held, nil
		}
	}

	parsed, err := url.Parse(src)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return heldPictureBytes{}, errors.Newf("the picture %q is not an https URL", src)
	}
	req, err := http.NewRequest(http.MethodGet, src, nil)
	if err != nil {
		return heldPictureBytes{}, errors.Wrapf(err, "the picture %q could not be asked for", src)
	}
	resp, err := pictureClient().Do(req)
	if err != nil {
		return heldPictureBytes{}, errors.Wrapf(err, "the picture at %s was not answered", parsed.Host)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return heldPictureBytes{}, errors.Newf("the picture at %s answered %d", parsed.Host, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPictureBytes+1))
	if err != nil {
		return heldPictureBytes{}, errors.Wrapf(err, "the picture at %s was cut off", parsed.Host)
	}
	if len(body) > maxPictureBytes {
		return heldPictureBytes{}, errors.Newf("the picture at %s is larger than %d bytes", parsed.Host, maxPictureBytes)
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}
	held := heldPictureBytes{body: body, contentType: contentType, fetchedAt: time.Now()}
	pictureBytes.Store(src, held)
	return held, nil
}
