package auth

import (
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/teranos/errors"
)

// A door is a domain people arrive at, and the namespace they arrive in
// (ADR-034). A passkey belongs to the domain it was made at, so a door is a
// relying party of its own and the node holds one per door.
//
// Which door a request reached is read off the request's own origin. Where a
// request acts is a property of the caller and never of the request (ADR-026);
// a door names a namespace, so picking one by anything a caller could type
// would be picking a namespace by asking.

// Door is one door as am.toml gives it. Namespace is the key under
// [auth.door.*], and the rest is what a browser is told.
type Door struct {
	Namespace string
	RPID      string
	Origins   []string
}

// door is a Door with its relying party built.
type door struct {
	namespace string
	rp        *webauthn.WebAuthn
}

// doors is every door this node answers, by origin. Replaced whole rather than
// edited, so a request reads one consistent set even while am.toml changes.
type doors struct {
	byOrigin atomic.Pointer[map[string]*door]
}

func (d *doors) set(built map[string]*door) { d.byOrigin.Store(&built) }

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
	for _, origin := range h.webauthn.Config.RPOrigins {
		built[origin] = own
	}

	for _, configuredDoor := range configured {
		if configuredDoor.Namespace == "" {
			return errors.New("a door with no namespace opens onto nothing")
		}
		if len(configuredDoor.Origins) == 0 {
			return errors.Newf("the door onto %q names no origin, so nothing reaches it", configuredDoor.Namespace)
		}

		for _, origin := range configuredDoor.Origins {
			if err := covers(configuredDoor.RPID, origin); err != nil {
				return errors.Wrapf(err, "the door onto %q cannot be reached", configuredDoor.Namespace)
			}
			if taken, ok := built[origin]; ok {
				return errors.Newf(
					"%s is the door onto %q and cannot also be the door onto %q — one origin reaches one namespace",
					origin, taken.namespace, configuredDoor.Namespace)
			}
		}

		rp, err := webauthn.New(&webauthn.Config{
			RPDisplayName: "QNTX",
			RPID:          configuredDoor.RPID,
			RPOrigins:     configuredDoor.Origins,
		})
		if err != nil {
			return errors.Wrapf(err, "the door onto %q has no relying party (rp_id=%q)", configuredDoor.Namespace, configuredDoor.RPID)
		}

		opened := &door{namespace: configuredDoor.Namespace, rp: rp}
		for _, origin := range configuredDoor.Origins {
			built[origin] = opened
		}
	}

	h.doors.set(built)
	return nil
}

// doorFor answers which door a request reached, or false when no door claims
// where it came from. A ceremony without a door would run against a relying
// party the browser never agreed to.
func (h *Handler) doorFor(r *http.Request) (*door, bool) {
	return h.doors.at(arrivedAt(r))
}

// atDoor answers which door a ceremony reached, or refuses it.
//
// A caller from an origin no door claims is told nothing more than that. A
// refusal naming the doors that do exist would be a directory of them, and a
// door that has to be found is not one anybody was meant to reach.
func (h *Handler) atDoor(w http.ResponseWriter, r *http.Request) (*door, bool) {
	arrived, ok := h.doorFor(r)
	if !ok {
		h.logger.Infow("Ceremony refused", "origin", arrivedAt(r), "reason", "no door answers there")
		h.writeError(w, http.StatusUnauthorized, "refused")
		return nil, false
	}
	return arrived, true
}

// arrivedAt is the origin a request came from. A fetch carries Origin, which is
// the browser's own assertion and the thing a ceremony is validated against. A
// page request carries none, and the host it asked for is the same fact by
// another name.
func arrivedAt(r *http.Request) string {
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin
	}
	if r.Host == "" {
		return ""
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
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
