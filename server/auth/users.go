package auth

import (
	"time"

	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/errors"
)

// What minted a did:key (ADR-030). laye mints one per browser, an authenticator
// derives one per device, and a User holds several of each.
const (
	OriginBrowser = "BROWSER"
	OriginDevice  = "DEVICE"
)

// UserKey is one did:key a User holds. None of them is the User.
type UserKey struct {
	DID    string `json:"did"`
	Origin string `json:"origin"`
}

// UserAccount is one provider account a User holds. CanonicalID is what the
// provider calls it, and the string auth.root_identities is matched against.
type UserAccount struct {
	Provider    string `json:"provider"`
	CanonicalID string `json:"canonical_id"`
	Handle      string `json:"handle"`
}

// User is a human being (ADR-031). This is the minimal pass — who they are,
// what they may do, and the routes that reach them.
type User struct {
	ID string `json:"id"`
	// DisplayName is what this person calls themselves, and the one handle a User
	// has that no provider issued. Empty is a person who has not said.
	DisplayName string `json:"display_name"`
	// EmailAddresses is any number of them, because neither one tells one User
	// from another. A new User supplies the first.
	EmailAddresses []string `json:"email_addresses"`
	Level          Level    `json:"level"`
	// CreatedBy is the User that made this one. Empty belongs to ROOT alone,
	// created by proving a listed route before there is a User to name.
	CreatedBy string        `json:"created_by"`
	Keys      []UserKey     `json:"keys"`
	Accounts  []UserAccount `json:"accounts"`
	CreatedAt int64         `json:"created_at"`
}

// Reaches reports whether an auth.root_identities entry reaches this User. A
// route is a did:key or an account's canonical_id, so both are asked.
func (u User) Reaches(route string) bool {
	for _, k := range u.Keys {
		if k.DID == route {
			return true
		}
	}
	for _, a := range u.Accounts {
		if a.CanonicalID == route {
			return true
		}
	}
	return false
}

// RootName is what the ROOT User is called before they say otherwise, and a
// name no other User may take.
const RootName = "root"

// Name is what to call this person. The ROOT User is root until they set
// something, which is why they never have to set one.
func (u User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.Level == LevelRoot {
		return RootName
	}
	return ""
}

// HoldsKey reports whether this User already logged in from this browser.
func (u User) HoldsKey(did string) bool {
	for _, k := range u.Keys {
		if k.DID == did {
			return true
		}
	}
	return false
}

// UserStore is where Users are kept: the system namespace, above the
// namespaces a User lives in (ADR-031). Nil when the backend has none.
type UserStore interface {
	// ByRoute resolves an auth.root_identities entry to the User it reaches.
	// False means nothing has been minted for it.
	ByRoute(route string) (User, bool, error)
	// List returns every User. How many there are is what decides ROOT.
	List() ([]User, error)
	// Put writes a User whole, replacing what was there.
	Put(u User) error
}

// joinUser records who an admission reached, minting the User the first time a
// route proves itself.

// Creating is not recording. ADR-030 says recording never fails the thing it
// records, and that holds for the key this browser adds — one more place to
// reach someone who already exists.

// It cannot hold for the User itself. Proving a listed route is what creates
// the ROOT User (ADR-031), so a claim that could not write one has claimed
// nothing, and admitting the browser anyway leaves it signed in as nobody.
func (h *Handler) joinUser(route string, matched *SignedBinding, layeDID string) (User, error) {
	if h.users == nil {
		return User{}, nil
	}

	u, found, err := h.users.ByRoute(route)
	if err != nil {
		return User{}, errors.Wrapf(err, "failed to read the User %q reaches", route)
	}

	if !found {
		u, err = h.reachRoot(route, matched)
		if err != nil {
			return User{}, errors.Wrapf(err, "failed to create the User %q reaches", route)
		}
	}

	// The browser this login came from is one more place the User is reachable.
	if u.HoldsKey(layeDID) {
		return u, nil
	}

	u.Keys = append(u.Keys, UserKey{DID: layeDID, Origin: OriginBrowser})
	if err := h.users.Put(u); err != nil {
		// Losing a way to reach a User is not losing the User, so this login
		// stands and the next one writes the key again.
		h.logger.Errorw("could not record the key a User logged in with",
			"user", u.ID, "did", layeDID, "error", err)
	}
	return u, nil
}

// userFor resolves the User a route reaches, for carrying into a session. A
// deployment with no store, or a route nothing holds, gives the zero User —
// this is a lookup, and failing it is not a reason to refuse a login.
func (h *Handler) userFor(route string) User {
	if h.users == nil {
		return User{}
	}

	u, found, err := h.users.ByRoute(route)
	if err != nil {
		h.logger.Errorw("could not read the User a session belongs to", "route", route, "error", err)
		return User{}
	}
	if !found {
		h.logger.Warnw("a session was issued for a route no User holds", "route", route)
		return User{}
	}
	return u
}

// joinDeviceKey records the key an authenticator derived for this device.

// Enrolment is the moment a biometric and an account can be tied together
// (ADR-030), so it is where the device key lands. The same finger derives the
// same key, which is why enrolling twice is not two keys.
func (h *Handler) joinDeviceKey(route, ownerDID string) {
	if h.users == nil || ownerDID == "" {
		return
	}

	u, found, err := h.users.ByRoute(route)
	if err != nil {
		h.logger.Errorw("could not read the User enrolling a device",
			"route", route, "did", ownerDID, "error", err)
		return
	}
	if !found {
		h.logger.Warnw("a device was enrolled for a route no User holds",
			"route", route, "did", ownerDID)
		return
	}
	if u.HoldsKey(ownerDID) {
		return
	}

	u.Keys = append(u.Keys, UserKey{DID: ownerDID, Origin: OriginDevice})
	if err := h.users.Put(u); err != nil {
		h.logger.Errorw("could not record the device key a User enrolled",
			"user", u.ID, "did", ownerDID, "error", err)
	}
}

// reachRoot records a proven route against the User it belongs to.

// Every route reaching here is listed in auth.root_identities, and listing
// several is how one User is reached from more than one place (ADR-030). A
// Mastodon account and an atproto DID are two routes to one person.
func (h *Handler) reachRoot(route string, matched *SignedBinding) (User, error) {
	// Read then write, with a network round trip in between. Two routes proving
	// themselves at once would each find no ROOT and each mint one, and a node
	// with two ROOT Users has no owner.
	h.minting.Lock()
	defer h.minting.Unlock()

	existing, err := h.users.List()
	if err != nil {
		return User{}, errors.Wrapf(err, "failed to read the Users before recording %q", route)
	}

	for _, held := range existing {
		if held.Level != LevelRoot {
			continue
		}
		u := withRoute(held, route, matched)
		if err := h.users.Put(u); err != nil {
			return User{}, errors.Wrapf(err, "failed to add %q to ROOT User %s", route, u.ID)
		}
		h.logger.Infow("route joined to the ROOT User", "user", u.ID, "route", route)
		return u, nil
	}

	// Nobody yet. Proving a listed route is what creates the first User, and
	// the first User is always the ROOT User (ADR-031).
	u := User{Level: LevelRoot, CreatedAt: time.Now().UTC().UnixMilli()}
	u.ID, err = identity.GenerateUserID("user")
	if err != nil {
		return User{}, errors.Wrapf(err, "failed to mint a User id for %q", route)
	}

	u = withRoute(u, route, matched)
	if err := h.users.Put(u); err != nil {
		return User{}, errors.Wrapf(err, "failed to write the User minted for %q", route)
	}
	h.logger.Infow("User minted", "user", u.ID, "level", u.Level, "route", route)
	return u, nil
}

// withRoute writes a route onto a User as the key or the account it is.
func withRoute(u User, route string, matched *SignedBinding) User {
	if matched == nil {
		u.Keys = append(u.Keys, UserKey{DID: route, Origin: OriginBrowser})
		return u
	}

	handle := ""
	if matched.Claim.Handle != nil {
		handle = *matched.Claim.Handle
	}
	u.Accounts = append(u.Accounts, UserAccount{
		Provider:    matched.Claim.Provider,
		CanonicalID: route,
		Handle:      handle,
	})
	return u
}
