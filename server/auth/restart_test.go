package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// "I'm pretty sure i logged in 5 times today with no additional security benifit to speak of"

// A session outlives the process that made it: a restart is a new store on
// the same db, and the token it handed out still names the same person.
func TestASessionSurvivesARestart(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	before := newSessionStore(24).kept(db, zap.NewNop().Sugar())
	token, err := before.create("https://mastodon.example/@tim", User{ID: "US-TIM-1", DisplayName: "Tim de Facile", Namespace: "Clean"})
	require.NoError(t, err)

	after := newSessionStore(24).kept(db, zap.NewNop().Sugar())
	identity, live := after.identityOf(token)
	require.True(t, live)
	assert.Equal(t, "https://mastodon.example/@tim", identity)
	user, name, namespace := after.userOf(token)
	assert.Equal(t, []string{"US-TIM-1", "Tim de Facile", "Clean"}, []string{user, name, namespace})

	// The db names the session by its hash; the token is not in it.
	var held int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM auth_sessions WHERE hash = ?`, token).Scan(&held))
	assert.Zero(t, held)

	// Signing out after the restart ends it for the next restart too.
	after.invalidate(token)
	_, live = newSessionStore(24).kept(db, zap.NewNop().Sugar()).identityOf(token)
	assert.False(t, live)
}

// An expired session does not come back from the db, and the sweep removes it.
func TestAnExpiredSessionDoesNotSurviveARestart(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	before := newSessionStore(24).kept(db, zap.NewNop().Sugar())
	token, err := before.create("https://mastodon.example/@tim", User{ID: "US-TIM-1"})
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE auth_sessions SET expires_at = ?`, time.Now().Add(-time.Minute).Unix())
	require.NoError(t, err)

	after := newSessionStore(24).kept(db, zap.NewNop().Sugar())
	_, live := after.identityOf(token)
	assert.False(t, live)

	_, err = before.create("https://mastodon.example/@tim", User{ID: "US-TIM-1"})
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE auth_sessions SET expires_at = ?`, time.Now().Add(-time.Minute).Unix())
	require.NoError(t, err)
	after.sweep()
	var left int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM auth_sessions`).Scan(&left))
	assert.Zero(t, left)
}
