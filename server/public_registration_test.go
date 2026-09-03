package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

func admittedAs(level auth.Level, namespaces ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	return r.WithContext(auth.WithAdmission(r.Context(), auth.Admission{
		Level:      level,
		Namespaces: namespaces,
	}))
}

// Somebody who walked up to a door reaches no store. Logging in is the whole
// of what the rung buys.
func TestAPublicRegistrationReachesNoStore(t *testing.T) {
	s := &QNTXServer{}

	_, err := s.storeFor(admittedAs(auth.LevelPublicRegistration, "garden"))

	require.Error(t, err, "a public registration was handed a store")
	assert.NotContains(t, err.Error(), "garden",
		"the refusal named the namespace it was refused from")
}

// The rung sits under all of them, so it is not one of the levels that reach
// the system namespace.
func TestAPublicRegistrationIsNotAboveAnything(t *testing.T) {
	s := &QNTXServer{}

	_, err := s.storeFor(admittedAs(auth.LevelPublicRegistration, auth.NamespaceSystem))

	require.Error(t, err)
}
