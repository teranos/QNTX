package access

import (
	"fmt"
	"slices"
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
	// What the provider last showed this person as. Unsigned, like the handle,
	// and replaced by whatever the provider gives at the next login.
	Picture string `json:"picture,omitempty"`
	// The signed binding that reached this account (ADR-031). Kept as it was
	// presented, so auth.binding_signers can be asked about its signer again
	// each time the account admits someone. Nil on a record written before
	// bindings were kept.
	Binding *SignedBinding `json:"binding,omitempty"`
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
	// PhoneNumbers is any number of them, for the same reason. Kept as digits
	// with the leading + a person typed, so one number is one string.
	PhoneNumbers []string `json:"phone_numbers"`
	Level        Level    `json:"level"`
	// Namespace is the door this User arrived at, and is set for a public
	// registration alone. The same provider account at two doors is two
	// registrations, and this is what tells them apart.
	Namespace string `json:"namespace,omitempty"`
	// Standing is the namespace this User is in, which the rectangle in the
	// namespaces bar draws. Where they came in is Namespace above and never
	// changes; this moves every time they step somewhere else.
	//
	// It is the person's and not the session's, so it is the same on every
	// device and survives logging out. Empty is a User who has not stepped
	// anywhere, which is default.
	Standing string `json:"standing,omitempty"`
	// CreatedBy is the User that made this one. Empty belongs to ROOT alone,
	// created by proving a listed route before there is a User to name.
	CreatedBy string `json:"created_by"`
	// DisabledBy is the User that switched this one off, and empty is a User
	// that is on. A person switches themselves off and on again; what ROOT
	// switched off is not theirs to switch on (ADR-031).
	DisabledBy string        `json:"disabled_by"`
	Keys       []UserKey     `json:"keys"`
	Accounts   []UserAccount `json:"accounts"`
	CreatedAt  int64         `json:"created_at"`
}

// SwitchedOff reports whether this User is off. A switched-off User still
// exists, still logs in, and is admitted to nothing but the switch.
func (u User) SwitchedOff() bool {
	return u.DisabledBy != ""
}

// PrimaryEmail is the address mail to this User goes to (ADR-041): the first
// one they supplied. Empty is a User who gave none.
func (u User) PrimaryEmail() string {
	if len(u.EmailAddresses) == 0 {
		return ""
	}
	return u.EmailAddresses[0]
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

// Picture is what this person looks like: the first picture any of their
// accounts carries. Empty is a person no provider has shown.
func (u User) Picture() string {
	for _, a := range u.Accounts {
		if a.Picture != "" {
			return a.Picture
		}
	}
	return ""
}

// WithPicture writes what a provider just showed onto the account it showed it
// for. Reports whether anything changed, so an unchanged User is not rewritten.
func (u User) WithPicture(canonicalID, picture string) (User, bool) {
	if picture == "" {
		return u, false
	}
	changed := false
	accounts := slices.Clone(u.Accounts)
	for i, a := range accounts {
		if a.CanonicalID == canonicalID && a.Picture != picture {
			accounts[i].Picture = picture
			changed = true
		}
	}
	u.Accounts = accounts
	return u, changed
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

// SignedBinding is laye's wire shape (crates/me). A binding says "this peer
// key belongs to this account", and it is worth exactly as much as the key
// that signed it.
type SignedBinding struct {
	Claim struct {
		PeerPubkeyHex string  `json:"peer_pubkey_hex"`
		Provider      string  `json:"provider"`
		CanonicalID   string  `json:"canonical_id"`
		Handle        *string `json:"handle"`
		IssuedAt      uint64  `json:"issued_at"`
	} `json:"claim"`
	SignatureHex    string `json:"signature_hex"`
	SignerPubkeyHex string `json:"signer_pubkey_hex"`
}

// CanonicalBytes reproduces laye-binding/v1 from crates/me/src/lib.rs. Both
// sides must render it identically or every signature fails.
func (b SignedBinding) CanonicalBytes() []byte {
	handle := ""
	if b.Claim.Handle != nil {
		handle = *b.Claim.Handle
	}
	return []byte(fmt.Sprintf("laye-binding/v1|%s|%s|%s|%s|%d",
		b.Claim.PeerPubkeyHex, b.Claim.Provider, b.Claim.CanonicalID, handle, b.Claim.IssuedAt))
}
