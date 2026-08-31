package auth

import (
	"strings"

	"github.com/teranos/errors"
)

// Drafting a door.
//
// A door comes from am.toml and nothing else, and nothing in this node writes
// that file. That is not a reason a person has to work out the block by hand:
// the node knows what goes in it, so it says the block and the person pastes
// it.
//
// It is the same arrangement as the OAuth client one level down. Google has no
// call that creates one — the client and its consent screen are made in the
// console by a person — so creating a door ends with the console link and the
// two strings that console asks for, both of which the node already knows.

// DoorDraft is what the door onto a namespace would be, said out loud.
//
// Nothing here is applied. It is a block to paste and the strings a provider's
// console will ask for, and it exists so that neither has to be worked out.
type DoorDraft struct {
	// Namespace is the door this would open onto.
	Namespace string `json:"namespace"`
	// RPID is the relying party the passkeys made at this door belong to.
	RPID string `json:"rp_id"`
	// Origins is where a browser reaches this door — where the page is, never
	// where the API answers.
	Origins []string `json:"origins"`
	// RedirectURI is where a provider sends somebody back, which is this node
	// and not the door. It is what the provider's console asks to be told, and
	// registering a different one is what makes a ceremony fail at the end.
	RedirectURI string `json:"redirect_uri"`
	// TOML is the block, ready to paste under [auth] in am.toml.
	TOML string `json:"toml"`
	// ClientTOML is the block that gives this door its own OAuth client, which
	// is what makes the consent screen say the door's name instead of the
	// node's. Separate because it is worth nothing until a console has issued
	// a client, and the door works without it.
	ClientTOML string `json:"client_toml"`
}

// maxDraftOrigins bounds what one door is drafted with. A door is a handful of
// hostnames; a caller sending more is not describing a door.
const maxDraftOrigins = 16

// DraftDoor composes the door onto a namespace.
//
// rpID empty takes the host of the first origin, which always covers that
// origin and is the answer whenever a door is one hostname. A door spanning
// several needs the domain they share, and only the person who owns them can
// say which that is — so it is asked for rather than guessed at.
//
// Every rule a door is held to at SetDoors is applied here, so a block this
// hands over is a block that opens rather than one refused on paste.
func (h *Handler) DraftDoor(namespace string, origins []string, rpID string) (DoorDraft, error) {
	if err := plainEnoughForTOML("namespace", namespace); err != nil {
		return DoorDraft{}, err
	}
	if namespace == NamespaceSystem {
		return DoorDraft{}, errors.New("the system namespace is not anyone's, and nobody arrives at it")
	}
	if namespace == NamespaceDefault {
		return DoorDraft{}, errors.New("auth.rp_id is already the door onto \"default\" — a second one has nothing left to open")
	}
	if len(origins) == 0 {
		return DoorDraft{}, errors.New("a door names where a browser reaches it, and this one names nowhere")
	}
	if len(origins) > maxDraftOrigins {
		return DoorDraft{}, errors.Newf("%d origins is more than the %d one door is drafted with", len(origins), maxDraftOrigins)
	}
	for _, origin := range origins {
		if err := plainEnoughForTOML("origin", origin); err != nil {
			return DoorDraft{}, err
		}
		if !strings.HasPrefix(origin, "https://") && !strings.HasPrefix(origin, "http://") {
			return DoorDraft{}, errors.Newf("%q is not an origin — a browser sends a scheme and a host, and that is what a door is named after", origin)
		}
	}

	if rpID == "" {
		rpID = hostOf(origins[0])
	}
	if err := plainEnoughForTOML("rp_id", rpID); err != nil {
		return DoorDraft{}, err
	}
	// The browser's rule, asked here rather than when somebody arrives. A block
	// that would be refused on paste is not a block worth handing over.
	for _, origin := range origins {
		if err := covers(rpID, origin); err != nil {
			return DoorDraft{}, errors.Wrapf(err, "the door onto %q cannot be reached", namespace)
		}
	}

	draft := DoorDraft{
		Namespace:   namespace,
		RPID:        rpID,
		Origins:     origins,
		RedirectURI: h.publicOrigin() + callbackPath,
	}
	draft.TOML = doorTOML(draft)
	draft.ClientTOML = doorClientTOML(namespace)
	return draft, nil
}

// doorTOML is the block itself. Written out rather than marshalled: the shape
// is fixed and small, and what a person pastes should be what this file says
// it is.
func doorTOML(d DoorDraft) string {
	var b strings.Builder
	b.WriteString("[auth.door." + d.Namespace + "]\n")
	b.WriteString("rp_id   = \"" + d.RPID + "\"\n")
	b.WriteString("origins = [")
	for i, origin := range d.Origins {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("\"" + origin + "\"")
	}
	b.WriteString("]\n")
	return b.String()
}

// doorClientTOML is the block that gives a door its own OAuth client. The
// secret is a reference and never a literal: am.toml ships as a world-readable
// SSM String parameter, so a literal there is already disclosed.
func doorClientTOML(namespace string) string {
	return "[auth.door." + namespace + ".provider.google]\n" +
		"client_id     = \"<what the console issues>\"\n" +
		"client_secret = \"ssm:///q/" + namespace + "/google/client-secret\"\n"
}

// hostOf is the host part of an origin, which is the rp id a one-hostname door
// takes. A host always covers itself, so this default never produces a block
// the browser would refuse.
func hostOf(origin string) string {
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
	return host
}

// plainEnoughForTOML refuses anything that would not survive being written into
// the block as itself.
//
// Refused rather than escaped. A door is a handful of hostnames and a namespace
// name; something in one of them that needs escaping is a mistake worth seeing,
// and escaping would hand back a block that reads differently than it parses.
func plainEnoughForTOML(what, value string) error {
	if value == "" {
		return errors.Newf("the %s is empty", what)
	}
	if strings.ContainsAny(value, "\"'\\\n\r\t[]{}#=") {
		return errors.Newf("the %s %q holds a character that cannot be written into am.toml as itself", what, value)
	}
	if strings.TrimSpace(value) != value {
		return errors.Newf("the %s %q begins or ends in whitespace", what, value)
	}
	return nil
}
