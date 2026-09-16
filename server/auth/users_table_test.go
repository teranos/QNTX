package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/errors"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// countingRecord is a record that counts how often it is read, so a test can
// say the table never reads it on a request.
type countingRecord struct {
	memUsers
	lists int
	puts  []string
}

func (r *countingRecord) List() ([]User, error) {
	r.lists++
	return r.memUsers.List()
}

func (r *countingRecord) Put(u User) error {
	r.puts = append(r.puts, u.ID)
	return r.memUsers.Put(u)
}

func aUser(id string, keys ...string) User {
	u := User{ID: id, Level: LevelAttestor, CreatedAt: 1}
	for _, k := range keys {
		u.Keys = append(u.Keys, UserKey{DID: k, Origin: OriginBrowser})
	}
	return u
}

func TestTheRecordIsReadOnceOnOpenAndNeverOnARequest(t *testing.T) {
	record := &countingRecord{memUsers: memUsers{held: []User{aUser("US-1", "did:key:one")}}}
	table, done, err := OpenUserTable(qntxtest.CreateTestDB(t), record)
	require.NoError(t, err)
	assert.Equal(t, 1, record.lists)
	assert.Equal(t, Reconciled{Held: 0, TakenIn: 1}, done)

	for range 20 {
		_, found, err := table.ByRoute("did:key:one")
		require.NoError(t, err)
		assert.True(t, found)
		_, err = table.List()
		require.NoError(t, err)
	}
	assert.Equal(t, 1, record.lists, "a request read the record")
}

func TestOpeningTakesInWhatTheRecordHoldsAndTheTableLacks(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	first, _, err := OpenUserTable(db, nil)
	require.NoError(t, err)
	require.NoError(t, first.Put(aUser("US-1", "did:key:one")))

	record := &countingRecord{memUsers: memUsers{held: []User{aUser("US-1", "did:key:one"), aUser("US-2", "did:key:two")}}}
	table, done, err := OpenUserTable(db, record)
	require.NoError(t, err)
	assert.Equal(t, Reconciled{Held: 1, TakenIn: 1, WrittenBack: 0}, done)

	held, err := table.List()
	require.NoError(t, err)
	assert.Len(t, held, 2)
	assert.Empty(t, record.puts, "a User that matched was written back")
}

func TestOpeningWritesBackWhatTheTableHoldsWhenTheRecordDiffers(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	first, _, err := OpenUserTable(db, nil)
	require.NoError(t, err)
	stepped := aUser("US-1", "did:key:one")
	stepped.Standing = "garden"
	require.NoError(t, first.Put(stepped))

	record := &countingRecord{memUsers: memUsers{held: []User{aUser("US-1", "did:key:one")}}}
	_, done, err := OpenUserTable(db, record)
	require.NoError(t, err)
	assert.Equal(t, Reconciled{Held: 1, TakenIn: 0, WrittenBack: 1}, done)
	assert.Equal(t, []string{"US-1"}, record.puts)
	assert.Equal(t, "garden", record.held[0].Standing, "the table is the truth")
}

func TestPutWritesTheTableThenTheRecord(t *testing.T) {
	record := &countingRecord{}
	table, _, err := OpenUserTable(qntxtest.CreateTestDB(t), record)
	require.NoError(t, err)

	require.NoError(t, table.Put(aUser("US-1", "did:key:one")))
	assert.Equal(t, []string{"US-1"}, record.puts)

	u, found, err := table.ByRoute("did:key:one")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "US-1", u.ID)

	u.DisabledBy = "US-1"
	require.NoError(t, table.Put(u))
	held, err := table.List()
	require.NoError(t, err)
	require.Len(t, held, 1)
	assert.Equal(t, "US-1", held[0].DisabledBy)
}

type recordThatWillNotTake struct{ memUsers }

func (*recordThatWillNotTake) Put(User) error { return errors.New("S3 said no") }

func TestARecordThatWillNotTakeTheWriteIsSaidAndTheTableKeepsIt(t *testing.T) {
	table, _, err := OpenUserTable(qntxtest.CreateTestDB(t), &recordThatWillNotTake{})
	require.NoError(t, err)

	err = table.Put(aUser("US-1", "did:key:one"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "US-1 is in the operational db but its record was not written")
	assert.Contains(t, err.Error(), "S3 said no")

	_, found, err := table.ByRoute("did:key:one")
	require.NoError(t, err)
	assert.True(t, found)
}

func TestARecordThatDoesNotAnswerStopsTheOpen(t *testing.T) {
	_, _, err := OpenUserTable(qntxtest.CreateTestDB(t), brokenUsers{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the User record did not answer")
}

func TestAUserWithNoListsIsTheSameUserAsOneWithEmptyOnes(t *testing.T) {
	assert.True(t, sameUser(User{ID: "US-1"}, User{ID: "US-1", Keys: []UserKey{}, EmailAddresses: []string{}}))
	assert.False(t, sameUser(User{ID: "US-1"}, User{ID: "US-1", Standing: "garden"}))
}
