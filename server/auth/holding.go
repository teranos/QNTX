package auth

import (
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/errors"
)

// Holding is what this handler still has that names a namespace: the Users
// registered at it, the live sessions acting in it, and the tokens that have
// not been revoked.
//
// A namespace is deleted only when nothing names it, and this is three of the
// things that can. Each is listed by what identifies it rather than counted
// alone, because the caller empties them one at a time by the verbs that exist
// — forget the credential, log the session out, revoke the token — and cannot
// do that from a number.
type Holding struct {
	// Users registered at this namespace, by User id. A public registration
	// carries the door it arrived at, and that door is a namespace.
	Users []string
	// Sessions is how many live sessions name it. A session has no id anybody
	// outside it can act on, so this is a count and says so.
	Sessions int
	// Tokens that name it and have not been revoked, by token id.
	Tokens []string
}

// Empty reports whether nothing here names the namespace.
func (h Holding) Empty() bool {
	return len(h.Users) == 0 && h.Sessions == 0 && len(h.Tokens) == 0
}

// NamesNamespace is everything this handler holds that names one namespace.
//
// Reached by slug: a door is keyed by slug and a namespace keeps the name it
// was created with, so "Clean" and "clean" are one namespace here too. A User
// carries the door it registered at, which is the key rather than the name.
//
// A store that will not answer is an error and not an empty answer: deleting a
// namespace on the strength of a lookup that failed would delete it while
// somebody was still registered at it.
func (h *Handler) NamesNamespace(namespace string) (Holding, error) {
	reachedBy := slug.Of(namespace)
	held := Holding{}

	if h.users != nil {
		everyone, err := h.users.List()
		if err != nil {
			return Holding{}, errors.Wrapf(err,
				"cannot tell which Users are registered at %s", namespace)
		}
		for _, u := range everyone {
			if u.Namespace != "" && slug.Of(u.Namespace) == reachedBy {
				held.Users = append(held.Users, u.ID)
			}
		}
	}

	h.sessions.sessions.Range(func(_, value any) bool {
		sess, isSession := value.(*session)
		if isSession && sess.namespace != "" && slug.Of(sess.namespace) == reachedBy {
			held.Sessions++
		}
		return true
	})

	if h.tokens != nil {
		minted, err := h.tokens.List()
		if err != nil {
			return Holding{}, errors.Wrapf(err,
				"cannot tell which tokens name %s", namespace)
		}
		for _, t := range minted {
			// A revoked token authenticates nothing, so it does not hold a
			// namespace open. Revoking is how a caller empties this list.
			if t.RevokedAt != nil {
				continue
			}
			for _, named := range t.Namespaces {
				if slug.Of(named) == reachedBy {
					held.Tokens = append(held.Tokens, t.ID)
					break
				}
			}
		}
	}

	return held, nil
}
