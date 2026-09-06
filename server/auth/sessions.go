package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/teranos/errors"
)

type session struct {
	token string
	// What admitted this session: an entry from auth.root_identities. Empty
	// when nothing in this deployment names an identity, which is the state a
	// passkey-only install stays in.
	identity string
	// Who that identity reaches (ADR-031), resolved once here rather than on
	// every request. Requests outnumber logins, and the User store is a scan.
	userID      string
	displayName string
	// The door that User registered at, which is the namespace they act in
	// (ADR-032). Empty for a User that walked up to no door — ROOT, and
	// everyone somebody else put here.
	namespace string

	expiresAt time.Time
}

type sessionStore struct {
	sessions sync.Map
	expiry   time.Duration
}

func newSessionStore(expiryHours int) *sessionStore {
	return &sessionStore{
		expiry: time.Duration(expiryHours) * time.Hour,
	}
}

func (s *sessionStore) create(identity string, user User) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", errors.Wrap(err, "failed to generate session token")
	}
	token := hex.EncodeToString(bytes)
	s.sessions.Store(token, &session{
		token:    token,
		identity: identity,
		userID:   user.ID,
		// Name, not the raw field: the ROOT User is root until they say
		// otherwise, and every surface should get the same answer.
		displayName: user.Name(),
		namespace:   user.Namespace,
		expiresAt:   time.Now().Add(s.expiry),
	})
	return token, nil
}

// userOf returns who this session is and where they came in, which is not the
// same question as what admitted it. Empty when the deployment keeps no Users.
func (s *sessionStore) userOf(token string) (userID, displayName, namespace string) {
	val, ok := s.sessions.Load(token)
	if !ok {
		return "", "", ""
	}

	// Anything else in the map is a wiring mistake, and naming nobody is a
	// better answer to it than panicking inside a request.
	sess, ok := val.(*session)
	if !ok {
		return "", "", ""
	}
	return sess.userID, sess.displayName, sess.namespace
}

func (s *sessionStore) validate(token string) bool {
	_, ok := s.identityOf(token)
	return ok
}

// identityOf returns what admitted this session. The bool is validity, so an
// unnamed identity and an expired session are different answers.
func (s *sessionStore) identityOf(token string) (string, bool) {
	val, ok := s.sessions.Load(token)
	if !ok {
		return "", false
	}
	// The map holds only what admit stores; an entry that is not a session
	// is not one to honour.
	sess, isSession := val.(*session)
	if !isSession {
		s.sessions.Delete(token)
		return "", false
	}
	if time.Now().After(sess.expiresAt) {
		s.sessions.Delete(token)
		return "", false
	}
	return sess.identity, true
}

func (s *sessionStore) invalidate(token string) {
	s.sessions.Delete(token)
}

// endEvery ends every session this User holds, and says how many there were.
//
// A person is not a browser. Erasing them has to reach the phone and the laptop
// and the tab they forgot about, not only the one that asked — a session left
// standing is a way back into a record that no longer exists.
//
// Nobody is not everybody: a deployment that keeps no Users issues sessions
// naming no User, and an empty id must not end all of them.
func (s *sessionStore) endEvery(userID string) int {
	if userID == "" {
		return 0
	}
	ended := 0
	s.sessions.Range(func(key, value any) bool {
		sess, isSession := value.(*session)
		if !isSession || sess.userID != userID {
			return true
		}
		s.sessions.Delete(key)
		ended++
		return true
	})
	return ended
}

func (s *sessionStore) sweep() {
	now := time.Now()
	s.sessions.Range(func(key, value interface{}) bool {
		sess, isSession := value.(*session)
		if !isSession || now.After(sess.expiresAt) {
			s.sessions.Delete(key)
		}
		return true
	})
}
