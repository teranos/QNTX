package auth

import (
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/teranos/errors"
)

// A door is a domain people arrive at, and the namespace they arrive in
//. A passkey belongs to the domain it was made at, so a door is a
// relying party of its own and the node holds one per door.
//
// Which door a request reached is read off the request's own origin. Where a
// request acts is a property of the caller and never of the request (ADR-026);
// a door names a namespace, so picking one by anything a caller could type
// would be picking a namespace by asking.

// OperatorClient is an OAuth client the operator registered, with the secret
// already resolved. Empty ID means the client was not configured.
type OperatorClient struct {
	ID     string
	Secret string
}

func (c OperatorClient) whole() bool { return c.ID != "" && c.Secret != "" }

// Door is one door as am.toml gives it. Namespace is the key under
// [auth.door.*], and the rest is what a browser is told.
type Door struct {
	Namespace string
	RPID      string
	Origins   []string
	// Clients is this door's own OAuth clients, by provider id. A door that
	// names none falls back to the node's, which is what makes a consent screen
	// say the node's name rather than the door's.
	Clients map[string]OperatorClient
}

// door is a Door with its relying party built.
type door struct {
	namespace string
	rp        *webauthn.WebAuthn
	clients   map[string]OperatorClient
}

// doors is every door this node answers, by origin. Replaced whole rather than
// edited, so a request reads one consistent set even while am.toml changes.
type doors struct {
	byOrigin atomic.Pointer[map[string]*door]
}

func (d *doors) set(built map[string]*door) { d.byOrigin.Store(&built) }

// clientsAt is the OAuth clients the door onto this namespace names. Nil for a
// namespace no door opens onto, which is every namespace with no [auth.door]
// entry — and the node's own clients answer for those.
func (d *doors) clientsAt(namespace string) map[string]OperatorClient {
	byOrigin := d.byOrigin.Load()
	if byOrigin == nil {
		return nil
	}
	for _, opened := range *byOrigin {
		if opened.namespace == namespace {
			return opened.clients
		}
	}
	return nil
}

func (d *doors) at(origin string) (*door, bool) {
	byOrigin := d.byOrigin.Load()
	if byOrigin == nil {
		return nil, false
	}
	found, ok := (*byOrigin)[origin]
	return found, ok
}

// SetDoors replaces the doors this node answers, keeping the node's own
// relying party as the door onto default. The config watcher calls this, so
// adding a door to am.toml opens it without waiting for a restart.
//
// A door that cannot work is refused here rather than at the moment somebody
// arrives at it, and refusing one leaves the doors already open untouched.
func (h *Handler) SetDoors(configured []Door) error {
	built := map[string]*door{}

	// The relying party this node was created with is the door onto default.
	// Nothing about it moves; it is now understood as a door.
	own := &door{namespace: NamespaceDefault, rp: h.webauthn}
	for _, origin := range h.ownOrigins {
		built[origin] = own
	}

	// Which namespaces already have a door. A namespace's door is what names
	// its OAuth clients, so two doors onto one namespace would make "which
	// client does this namespace use" a question with two answers.
	hasDoor := map[string]bool{NamespaceDefault: true}

	for _, configuredDoor := range configured {
		if configuredDoor.Namespace == "" {
			return errors.New("a door with no namespace opens onto nothing")
		}
		if len(configuredDoor.Origins) == 0 {
			return errors.Newf("the door onto %q names no origin, so nothing reaches it", configuredDoor.Namespace)
		}
		if hasDoor[configuredDoor.Namespace] {
			if configuredDoor.Namespace == NamespaceDefault {
				return errors.New("auth.rp_id is already the door onto \"default\" — a second one has nothing left to open")
			}
			return errors.Newf("%q already has a door — one namespace is reached through one door", configuredDoor.Namespace)
		}
		hasDoor[configuredDoor.Namespace] = true

		for _, origin := range configuredDoor.Origins {
			if isWebOrigin(origin) {
				if err := covers(configuredDoor.RPID, origin); err != nil {
					return errors.Wrapf(err, "the door onto %q cannot be reached", configuredDoor.Namespace)
				}
			}
			if taken, ok := built[origin]; ok {
				return errors.Newf(
					"%s is the door onto %q and cannot also be the door onto %q — one origin reaches one namespace",
					origin, taken.namespace, configuredDoor.Namespace)
			}
		}

		// An app's scheme is a door too, and a return address only: no browser
		// presents a passkey for a scheme, so the relying party is told the
		// web origins alone.
		rp, err := webauthn.New(&webauthn.Config{
			RPDisplayName: "QNTX",
			RPID:          configuredDoor.RPID,
			RPOrigins:     webOrigins(configuredDoor.Origins),
		})
		if err != nil {
			return errors.Wrapf(err, "the door onto %q has no relying party (rp_id=%q)", configuredDoor.Namespace, configuredDoor.RPID)
		}

		opened := &door{
			namespace: configuredDoor.Namespace,
			rp:        rp,
			clients:   configuredDoor.Clients,
		}
		for _, origin := range configuredDoor.Origins {
			built[origin] = opened
		}
	}

	h.doors.set(built)
	return nil
}

// isWebOrigin is whether a browser could stand at this origin. Anything else
// is an app's own scheme: a door, a return address, never a passkey origin.
func isWebOrigin(origin string) bool {
	return strings.HasPrefix(origin, "https://") || strings.HasPrefix(origin, "http://")
}

// webOrigins is the origins a relying party may be told.
func webOrigins(origins []string) []string {
	web := make([]string, 0, len(origins))
	for _, origin := range origins {
		if isWebOrigin(origin) {
			web = append(web, origin)
		}
	}
	return web
}

// doorFor answers which door a request reached, or false when no door claims
// where it came from. A ceremony without a door would run against a relying
// party the browser never agreed to.
func (h *Handler) doorFor(r *http.Request) (*door, bool) {
	came, said := arrivedAt(r)
	if !said {
		return nil, false
	}
	return h.doors.at(came)
}

// doorNamespace is where a request arrived, for the paths that need the
// namespace and not the relying party.
//
// An origin no door claims answers default, which is the node's own door and
// the node's own OAuth clients. A ceremony that needs a relying party asks
// atDoor instead and is refused there — this is the softer question.
func (h *Handler) doorNamespace(r *http.Request) string {
	if arrived, ok := h.doorFor(r); ok {
		return arrived.namespace
	}
	return NamespaceDefault
}

// atDoor answers which door a ceremony reached, or refuses it.
//
// A caller from an origin no door claims is told nothing more than that. A
// refusal naming the doors that do exist would be a directory of them, and a
// door that has to be found is not one anybody was meant to reach.
func (h *Handler) atDoor(w http.ResponseWriter, r *http.Request) (*door, bool) {
	arrived, ok := h.doorFor(r)
	if !ok {
		came, _ := arrivedAt(r)
		h.logger.Infow("Ceremony refused", "origin", came, "reason", "no door answers there")
		h.writeError(w, http.StatusUnauthorized, "refused")
		return nil, false
	}
	return arrived, true
}

// arrivedAt is the origin a request came from.
//
// A fetch carries Origin, and that is the page's origin — the browser's own
// assertion, and the thing a ceremony is validated against. It is not this
// node's origin: a page at q.sbvh.nl calling api.q.sbvh.nl sends the first.
// Doors are named after where the page is, so Origin is the one that answers.
//
// A request carrying none falls back to the host it asked for, which finds a
// door only on a node that serves the page itself — the dev arrangement, where
// the two are one host. Where they are not, the host is the API's and no door
// claims it, so the fallback reaches nothing rather than reaching the wrong
// thing. Nothing here has to know which arrangement it is in.
// The second return is whether the request said where it came from at all. A
// request that named nowhere and an origin no door claims are different facts,
// and one empty string cannot be both.
func arrivedAt(r *http.Request) (string, bool) {
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin, true
	}
	// A top-level navigation carries no Origin, and starting a ceremony is one.
	// Referer is the browser's word for where the page was, so it is read the
	// same way Origin is — never from anything the page itself chose to send.
	if referred := originOf(r.Header.Get("Referer")); referred != "" {
		return referred, true
	}
	// A page at an app's scheme sends neither, so the navigation names its
	// door. This is the page's word, and it decides nothing on its own: every
	// reader of arrivedAt asks doors.at, which knows only what am.toml named.
	if named := originOf(r.URL.Query().Get("door")); named != "" {
		return named, true
	}
	if r.Host == "" {
		return "", false
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host, true
}

// returnableTo is where a ceremony may send somebody when it is over.
//
// Only an origin a door was named after. Anyone can navigate to this node from
// a page of their own, so the header saying where they came from is a place
// this node will send them only if am.toml already said so.
func (h *Handler) returnableTo(r *http.Request) string {
	came, said := arrivedAt(r)
	if !said {
		return ""
	}
	if _, known := h.doors.at(came); !known {
		return ""
	}
	return came
}

// originOf is the scheme and host of a URL, which is what an origin is. The
// default referrer policy already sends only that much across sites; a browser
// configured to send the whole URL has the rest cut off here.
func originOf(url string) string {
	cut := strings.Index(url, "://")
	if cut == -1 {
		return ""
	}
	rest := url[cut+len("://"):]
	if end := strings.Index(rest, "/"); end != -1 {
		rest = rest[:end]
	}
	if rest == "" {
		return ""
	}
	return url[:cut] + "://" + rest
}

// covers reports whether a browser would accept this rp id for this origin.
// The rule is the browser's: an rp id must be a registrable domain suffix of
// the origin's host, so a ceremony that breaks it is refused by every client
// and is worth catching where am.toml is read.
func covers(rpID, origin string) error {
	host := origin
	if cut := strings.Index(host, "://"); cut != -1 {
		host = host[cut+3:]
	}
	if cut := strings.Index(host, "/"); cut != -1 {
		host = host[:cut]
	}
	if cut := strings.LastIndex(host, ":"); cut != -1 && !strings.Contains(host[cut:], "]") {
		host = host[:cut]
	}

	host = strings.ToLower(strings.TrimSuffix(host, "."))
	id := strings.ToLower(strings.TrimSuffix(rpID, "."))

	if id == "" {
		return errors.Newf("%s names no rp id", origin)
	}
	if host == id {
		return nil
	}
	if strings.HasSuffix(host, "."+id) {
		return nil
	}
	return errors.Newf("rp_id %q is not a registrable domain suffix of %s, and every browser refuses that", rpID, origin)
}
