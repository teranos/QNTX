package server

import (
	"net/http"
	"testing"
)

// CloudFront rewrites only these, per its own documentation: a distribution may
// name a custom response for 400, 403, 404, 405, 414, 416 and the 5xx range.
// A status outside the set reaches the viewer whatever an edge is configured to
// do, now or later.
var edgeMayRewrite = map[int]bool{
	400: true, 403: true, 404: true, 405: true, 414: true, 416: true,
	500: true, 501: true, 502: true, 503: true, 504: true,
}

// A refusal from /g/ is imported by a browser, and an edge that rewrites it
// hands the page HTML with a 200 — which fails as a parse error naming nothing.
// The statuses this handler refuses with are therefore chosen to survive.
func TestGlyphRefusalsSurviveTheEdge(t *testing.T) {
	refusals := map[string]int{
		"nothing published": StatusNoModulePublished,
		"malformed ask":     http.StatusUnprocessableEntity,
	}

	for what, status := range refusals {
		if edgeMayRewrite[status] {
			t.Errorf("%s answers %d, which an edge may rewrite into a page", what, status)
		}
	}
}

// The status has to mean something to a reader, not only survive. Gone says the
// node looked; it is not a 4xx chosen at random for its number.
func TestNoModulePublishedIsGone(t *testing.T) {
	if StatusNoModulePublished != http.StatusGone {
		t.Errorf("StatusNoModulePublished = %d, want %d (Gone)",
			StatusNoModulePublished, http.StatusGone)
	}
}
