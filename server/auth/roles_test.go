package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The names the issue uses: garden and orchard are namespaces, WORKER and
// COORDINATOR are roles. A role name ships in no binary — these are strings a
// deployment made up, and that is the point.
const (
	gardenNamespace  = "garden"
	orchardNamespace = "orchard"
	roleWorker       = "WORKER"
	roleCoordinator  = "COORDINATOR"
	googleAccount    = "google:110169484474386276334"
	workerTokenDID   = "did:key:zWorkerToken"
)

// A reader with lines and nothing behind them. What is tested here is how two
// claims about one role settle, which is policy rather than storage.
type memRoles struct {
	lines map[string][]RoleLine
	words []WordLine
	reads int
}

func (m *memRoles) RoleLines(namespace string) ([]RoleLine, error) {
	m.reads++
	return m.lines[namespace], nil
}

func (m *memRoles) WordLines() ([]WordLine, error) {
	return m.words, nil
}

// A handler that holds the given lines and knows the mastodon account as ROOT,
// so "by ROOT" on a line has something to match.
func handlerHolding(t *testing.T, lines map[string][]RoleLine) (*Handler, *memRoles) {
	t.Helper()
	h := &Handler{logger: zap.NewNop().Sugar()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	kept := &memRoles{lines: lines}
	h.SetRoleReader(kept)
	return h, kept
}

// The User a grant is about: one person, two accounts and a browser key.
func gardener() User {
	return User{
		ID:    "US-gardener",
		Level: LevelPublicRegistration,
		Accounts: []UserAccount{
			{Provider: "mastodon", CanonicalID: "https://mastodon.example/@gardener"},
			{Provider: "google", CanonicalID: googleAccount},
		},
		Keys: []UserKey{{DID: "did:key:zBrowser", Origin: OriginBrowser}},
	}
}

func at(minute int) time.Time {
	return time.Date(2026, 9, 4, 12, minute, 0, 0, time.UTC)
}

// Revoking is writing the second predicate, and what the two lines resolve to
// is nothing. The grant is not deleted — it is outranked by what came after.
func TestAGrantThenARevokeHoldsNothing(t *testing.T) {
	granted := RoleLine{Routes: []string{googleAccount}, Roles: []string{roleWorker},
		Granted: true, Actor: mastodonAccount, At: at(0)}
	revoked := RoleLine{Routes: []string{googleAccount}, Roles: []string{roleWorker},
		Granted: false, Actor: mastodonAccount, At: at(1)}

	h, _ := handlerHolding(t, map[string][]RoleLine{gardenNamespace: {granted}})
	require.Equal(t, []string{roleWorker}, h.RolesOf(gardener(), gardenNamespace),
		"the grant on its own does not hold, so the revoke proves nothing")

	h, _ = handlerHolding(t, map[string][]RoleLine{gardenNamespace: {granted, revoked}})
	assert.Empty(t, h.RolesOf(gardener(), gardenNamespace))
}

// A ROOT claim outranks every other actor, whatever the clock says. The
// dangerous reading is last-write-wins, which hands the role back to anybody
// who can write a line after ROOT took it away.
func TestALaterCoordinatorGrantDoesNotOutrankAnEarlierRootRevoke(t *testing.T) {
	revokedByRoot := RoleLine{Routes: []string{googleAccount}, Roles: []string{roleWorker},
		Granted: false, Actor: mastodonAccount, At: at(0)}
	grantedLater := RoleLine{Routes: []string{googleAccount}, Roles: []string{roleWorker},
		Granted: true, Actor: "did:key:zCoordinatorToken", At: at(5)}

	h, _ := handlerHolding(t, map[string][]RoleLine{gardenNamespace: {grantedLater}})
	require.Equal(t, []string{roleWorker}, h.RolesOf(gardener(), gardenNamespace),
		"a grant by an actor who is not ROOT is read like any other line")

	h, _ = handlerHolding(t, map[string][]RoleLine{gardenNamespace: {revokedByRoot, grantedLater}})
	assert.Empty(t, h.RolesOf(gardener(), gardenNamespace))
}

// A person is reached by any number of routes and a grant names one of them,
// so the User is what a line is matched against — not the route they logged in
// by, which would make a role depend on which door was used today.
func TestAGrantOnOneAccountHoldsForTheWholeUser(t *testing.T) {
	h, _ := handlerHolding(t, map[string][]RoleLine{gardenNamespace: {
		{Routes: []string{googleAccount}, Roles: []string{roleWorker},
			Granted: true, Actor: mastodonAccount, At: at(0)},
	}})

	assert.Equal(t, []string{roleWorker}, h.RolesOf(gardener(), gardenNamespace))
	assert.Empty(t, h.RolesOf(User{ID: "US-stranger"}, gardenNamespace),
		"a User the line names no route of holds it too")
}

// A role holds in exactly one namespace. A coordinator of two holds two lines,
// and one line is never both.
func TestAGrantInOneNamespaceSaysNothingInAnother(t *testing.T) {
	h, _ := handlerHolding(t, map[string][]RoleLine{gardenNamespace: {
		{Routes: []string{googleAccount}, Roles: []string{roleCoordinator},
			Granted: true, Actor: mastodonAccount, At: at(0)},
	}})

	assert.Equal(t, []string{roleCoordinator}, h.RolesOf(gardener(), gardenNamespace))
	assert.Empty(t, h.RolesOf(gardener(), orchardNamespace))
}

// A token may hold a role, so that the day dispatching is a program it is one
// grant line and the same lines — not a human version and a machine version.
func TestATokensDidHoldsARole(t *testing.T) {
	h, _ := handlerHolding(t, map[string][]RoleLine{gardenNamespace: {
		{Routes: []string{workerTokenDID}, Roles: []string{roleWorker},
			Granted: true, Actor: mastodonAccount, At: at(0)},
	}})

	assert.Equal(t, []string{roleWorker}, h.RolesOfDID(workerTokenDID, gardenNamespace))
	assert.Empty(t, h.RolesOfDID("did:key:zSomeOtherToken", gardenNamespace))
}

// Loaded per namespace on first read and dropped whole on a write, because the
// node is the only writer. No ticker: nothing expires that nothing changed.
func TestRolesAreReadOnceUntilAWriteDropsThem(t *testing.T) {
	h, kept := handlerHolding(t, map[string][]RoleLine{gardenNamespace: {
		{Routes: []string{googleAccount}, Roles: []string{roleWorker},
			Granted: true, Actor: mastodonAccount, At: at(0)},
	}})

	h.RolesOf(gardener(), gardenNamespace)
	h.RolesOf(gardener(), gardenNamespace)
	require.Equal(t, 1, kept.reads, "the namespace was read again for an answer nothing changed")

	h.RolesOf(gardener(), orchardNamespace)
	require.Equal(t, 2, kept.reads, "a second namespace is a second read")

	h.ForgetRoles()
	h.RolesOf(gardener(), gardenNamespace)
	assert.Equal(t, 3, kept.reads, "a write did not drop what was held")
}
