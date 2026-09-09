package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
)

const (
	rootAccount   = "https://mastodon.example/@tim"
	gardenerRoute = "google:110169484474386276334"
)

// A node that knows one ROOT account, so a grant has somebody who may write it.
func rootKnowingServer(t *testing.T) *QNTXServer {
	t.Helper()
	store, db := createTestStore(t)
	system, _ := createTestStore(t)

	h, err := auth.New(nil, "localhost", nil, 8770, 8820, 24, zap.NewNop().Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		nil, nil, false, []string{rootAccount}, nil)
	require.NoError(t, err)

	s := &QNTXServer{db: db, authHandler: h, logger: zap.NewNop().Sugar()}
	s.held = servingOne(db, store)
	s.held.SetSystem(oneNamespace("system", system))
	return s
}

// systemHolds is what the node wrote about itself, read back through the door
// that only reads — the same one every lookup in the node uses.
func systemHolds(t *testing.T, s *QNTXServer) []*types.As {
	t.Helper()
	sys, err := s.held.Read(auth.NamespaceSystem)
	require.NoError(t, err)
	held, err := sys.GetAttestations(ats.AttestationFilter{Limit: 10})
	require.NoError(t, err)
	return held
}

func grants(t *testing.T, s *QNTXServer, caller auth.Admission, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/attestations", jsonBody(body))
	req = req.WithContext(auth.WithAdmission(req.Context(), caller))
	rec := httptest.NewRecorder()
	s.handleCreateAttestation(rec, req)
	return rec
}

const workerGrant = `{"subjects":["google:110169484474386276334"],` +
	`"predicates":["role:granted","WORKER"],"contexts":["garden"]}`

// Somebody who walked up to a door and made themselves cannot hand themselves
// a role. The refusal names the predicate, because being turned away from the
// store for reaching none is a different answer than this one.
func TestAPublicRegistrationCannotWriteAGrant(t *testing.T) {
	s := rootKnowingServer(t)

	rec := grants(t, s, auth.Admitted(auth.LevelPublicRegistration, "garden"), workerGrant)

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), auth.PredicateRoleGranted)
}

// A revoke is refused the same way. Being able to write only the second
// predicate would be a way to take a role off somebody else.
func TestAPublicRegistrationCannotWriteARevoke(t *testing.T) {
	s := rootKnowingServer(t)

	rec := grants(t, s, auth.Admitted(auth.LevelPublicRegistration, "garden"),
		`{"subjects":["google:110169484474386276334"],`+
			`"predicates":["role:revoked","WORKER"],"contexts":["garden"]}`)

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), auth.PredicateRoleRevoked)
}

// A grant is written where the node keeps what it knows about itself, whatever
// namespace the writer is in. The session here acts in garden and the line
// lands in system, and its actor is the ROOT identity that said it.
func TestAGrantIsWrittenWhereTheNodeKeepsItsOwn(t *testing.T) {
	s := rootKnowingServer(t)
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount

	rec := grants(t, s, root, workerGrant)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	inGarden, err := s.held.Served().GetAttestations(ats.AttestationFilter{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, inGarden, "a grant landed in the namespace its writer was in")

	held := systemHolds(t, s)
	require.Len(t, held, 1)
	assert.Equal(t, []string{gardenerRoute}, held[0].Subjects)
	assert.Equal(t, []string{auth.PredicateRoleGranted, "WORKER"}, held[0].Predicates)
	assert.Equal(t, []string{"garden"}, held[0].Contexts)
	assert.Equal(t, []string{rootAccount}, held[0].Actors,
		"a grant whose actor is nobody cannot be outranked by one that is ROOT's")
}

// And the node can read back what it just wrote: the line is a record, and the
// record is the only place the answer comes from.
func TestTheNodeReadsBackTheGrantItWrote(t *testing.T) {
	s := rootKnowingServer(t)
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount
	s.authHandler.SetRoleReader(roleLines{s: s})

	gardener := auth.User{ID: "US-gardener",
		Accounts: []auth.UserAccount{{Provider: "google", CanonicalID: gardenerRoute}}}
	require.Empty(t, s.authHandler.RolesOf(gardener, "garden"))

	require.Equal(t, http.StatusCreated, grants(t, s, root, workerGrant).Code)

	assert.Equal(t, []string{"WORKER"}, s.authHandler.RolesOf(gardener, "garden"))
	assert.Empty(t, s.authHandler.RolesOf(gardener, "orchard"))
}
