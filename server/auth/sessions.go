package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/teranos/QNTX/errors"
)

type session struct {
	token     string
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

func (s *sessionStore) create() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", errors.Wrap(err, "failed to generate session token")
	}
	token := hex.EncodeToString(bytes)
	s.sessions.Store(token, &session{
		token:     token,
		expiresAt: time.Now().Add(s.expiry),
	})
	return token, nil
}

func (s *sessionStore) validate(token string) bool {
	val, ok := s.sessions.Load(token)
	if !ok {
		return false
	}
	sess := val.(*session)
	if time.Now().After(sess.expiresAt) {
		s.sessions.Delete(token)
		return false
	}
	return true
}

func (s *sessionStore) invalidate(token string) {
	s.sessions.Delete(token)
}

func (s *sessionStore) sweep() {
	now := time.Now()
	s.sessions.Range(func(key, value interface{}) bool {
		sess := value.(*session)
		if now.After(sess.expiresAt) {
			s.sessions.Delete(key)
		}
		return true
	})
}
