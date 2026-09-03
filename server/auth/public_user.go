package auth

import (
	"net/http"
	"slices"
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/errors"
)

// joinPublic is the User somebody makes by walking up to a door.
//
// Every other User the node holds was put there by somebody. This one arrived,
// proved an account at a provider, and that is the whole of what it took.
func (h *Handler) joinPublic(acct account, door string) (User, error) {
	if h.users == nil {
		return User{}, errors.New("this node keeps no Users, so a registration has nowhere to go")
	}
	if acct.CanonicalID == "" {
		return User{}, errors.New("the provider named no account, so there is nobody to register")
	}
	if door == "" {
		return User{}, errors.New("a registration belongs to the door it arrived at, and this one names none")
	}

	// Read then write, the way reachRoot does. Two ceremonies finishing at once
	// would each find nothing and each mint, and one person would be two.
	h.minting.Lock()
	defer h.minting.Unlock()

	held, err := h.users.List()
	if err != nil {
		return User{}, errors.Wrapf(err, "failed to read the Users before registering %q at %q", acct.CanonicalID, door)
	}

	// The same account at two doors is two registrations, so the door is half
	// of what identifies one.
	for _, u := range held {
		if u.Level == LevelPublicRegistration && u.Namespace == door && u.Reaches(acct.CanonicalID) {
			return h.withAddress(u, acct.Handle)
		}
	}

	u := User{
		Level:     LevelPublicRegistration,
		Namespace: door,
		CreatedAt: time.Now().UTC().UnixMilli(),
	}
	u.ID, err = identity.GenerateUserID("user")
	if err != nil {
		return User{}, errors.Wrapf(err, "failed to mint a User id for %q at %q", acct.CanonicalID, door)
	}
	u.Accounts = []UserAccount{{CanonicalID: acct.CanonicalID, Handle: acct.Handle}}

	written, err := h.withAddress(u, acct.Handle)
	if err != nil {
		return User{}, err
	}
	// "when a user registers, we attest it, and in it, an email address may be."
	//
	// Written here rather than where the ceremony ends, because this is where
	// the registration is: the ceremony's own record is somebody arriving and
	// proving an account, which happens whether or not a User comes of it. This
	// fires once, on the mint, so it is one record per registration and not one
	// per login.
	attrs := map[string]any{"door": door, "user": written.ID}
	// The provider decides what it hands over. An empty handle written down
	// would say it named nobody, which is not the same as not being asked.
	if acct.Handle != "" {
		attrs["handle"] = acct.Handle
	}
	h.attest(PredicateRegistered, acct.CanonicalID, attrs)

	h.logger.Infow("public registration", "user", written.ID, "door", door, "level", written.Level)
	return written, nil
}

// publicLevelOf is the rung of somebody the deployment never listed.
//
// A public registration is admitted by the User record existing, and the User
// record exists because a provider vouched at a door once. Nothing is asserted
// here: no User, no rung, and the caller refuses on the empty answer.
func (h *Handler) publicLevelOf(route string) Level {
	if h.users == nil || route == "" {
		return ""
	}
	u, found, err := h.users.ByRoute(route)
	if err != nil {
		// Not a refusal that says why. The store failing is the node's problem
		// and the log is where it belongs; the caller gets a shut door.
		h.logger.Errorw("could not read the User a route reaches, so its rung is unknown",
			"route", route, "error", err)
		return ""
	}
	if !found || u.Level != LevelPublicRegistration {
		return ""
	}
	return LevelPublicRegistration
}

// admitPublic logs somebody in who is on no list at all.
//
// A signed binding is a provider saying this key holds that account, and that
// is the entire gate: no passkey, no am.toml entry, nobody vouching. The
// passkey is what SUPER is made to require, and this is not that path.
//
// What it buys is a session at PUBLIC_REGISTRATION, which reaches no store.
// Reports whether the request was answered.
func (h *Handler) admitPublic(w http.ResponseWriter, r *http.Request, vouched []SignedBinding) bool {
	if len(vouched) == 0 {
		return false
	}
	// A registration belongs to the door it arrived at. No door, no namespace
	// for it to belong to, and nothing to write.
	arrived, ok := h.doorFor(r)
	if !ok {
		return false
	}

	// Whichever provider vouched first. They are the same kind of proof, and
	// registering is registering.
	binding := vouched[0]
	acct := account{CanonicalID: binding.Claim.CanonicalID}
	if binding.Claim.Handle != nil {
		acct.Handle = *binding.Claim.Handle
	}

	u, err := h.joinPublic(acct, arrived.namespace)
	if err != nil {
		h.attest(PredicateUnanswered, acct.CanonicalID, map[string]any{
			"asked": "User store", "doing": "register", "error": err.Error(),
		})
		// The attestation goes to a store that has just failed, so the cause is
		// logged as well or it is lost with it.
		h.logger.Errorw("a public registration was not written",
			"door", arrived.namespace, "error", err)
		h.writeError(w, http.StatusServiceUnavailable, "the User store did not answer")
		return true
	}

	// joinPublic attested the registration on the mint. What is left to record
	// here is the admission, which happens every time rather than once.
	token, err := h.sessions.create(acct.CanonicalID, u)
	if err != nil {
		h.logger.Errorw("a public registration was written but no session could be made for it",
			"user", u.ID, "door", arrived.namespace, "error", err)
		h.writeError(w, http.StatusInternalServerError, "the session was not created")
		return true
	}
	h.setSessionCookie(w, token)

	h.attest(PredicateLoggedIn, acct.CanonicalID, map[string]any{
		"provider": binding.Claim.Provider,
		"door":     arrived.namespace,
		"level":    string(LevelPublicRegistration),
	})
	h.logger.Infow("public registration admitted",
		"user", u.ID, "door", arrived.namespace, "provider", binding.Claim.Provider)

	// No device to go and get: this rung is done being admitted. "next" is
	// what the ROOT path uses to send a browser at an authenticator, and
	// saying "nothing" is saying the login finished here.
	h.writeJSON(w, http.StatusOK, map[string]any{
		"admitted_as": acct.CanonicalID,
		"next":        "nothing",
		"level":       string(LevelPublicRegistration),
		"name":        u.Name(),
		"user":        u.ID,
		// The cookie above reaches a page on this node's own site. A door on
		// another domain gets none, so it is handed the session to present.
		"session": token,
	})
	return true
}

// withAddress writes the User with the address the provider gave, when it gave
// one and the User does not already hold it.
func (h *Handler) withAddress(u User, address string) (User, error) {
	if address != "" && !slices.Contains(u.EmailAddresses, address) {
		u.EmailAddresses = append(u.EmailAddresses, address)
	}
	if err := h.users.Put(u); err != nil {
		return User{}, errors.Wrapf(err, "failed to write the public registration %s at %q", u.ID, u.Namespace)
	}
	return u, nil
}
