package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/errors"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// countingTokens is a record that says how often it was read and what was
// written to it, so a test can hold it to never being either.
type countingTokens struct {
	held    []TokenRecord
	records int
	puts    []string
}

func (r *countingTokens) Records() ([]TokenRecord, error) {
	r.records++
	return append([]TokenRecord(nil), r.held...), nil
}

func (r *countingTokens) PutRecord(t TokenRecord) error {
	r.puts = append(r.puts, t.ID)
	for at, held := range r.held {
		if held.Hash == t.Hash {
			r.held[at] = t
			return nil
		}
	}
	r.held = append(r.held, t)
	return nil
}

func aToken(hash, label string) IssuedToken {
	return IssuedToken{
		Hash:       hash,
		DID:        "did:key:z" + hash,
		Label:      label,
		MintedBy:   "https://mastodon.example/@tim",
		Level:      LevelAttestor,
		Namespaces: []string{"default"},
	}
}

// The whole reason a token moves onto the node: its record was rewritten on
// every use, which is one S3 PUT per authenticated request.
func TestRecordingAUseNeverReachesTheRecord(t *testing.T) {
	record := &countingTokens{}
	table, _, err := OpenTokenTable(qntxtest.CreateTestDB(t), record)
	require.NoError(t, err)

	_, err = table.Issue(aToken("abc", "one"))
	require.NoError(t, err)
	require.Len(t, record.puts, 1, "minting has to write the record")

	for range 20 {
		require.NoError(t, table.Touch("abc"))
	}
	assert.Len(t, record.puts, 1, "recording a use wrote the record")
}

// The trap. A token that has been used differs from its object by last_used_at
// alone, and a take-in that compares every field writes every used token back
// at every open — spending on S3 exactly what moving the table stopped
// spending, with every other test still passing.
func TestOpeningDoesNotWriteBackATokenThatWasOnlyUsed(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	record := &countingTokens{}

	first, _, err := OpenTokenTable(db, record)
	require.NoError(t, err)
	_, err = first.Issue(aToken("abc", "one"))
	require.NoError(t, err)
	require.NoError(t, first.Touch("abc"))
	require.NoError(t, first.Flush())
	record.puts = nil

	_, done, err := OpenTokenTable(db, record)
	require.NoError(t, err)
	assert.Empty(t, record.puts, "a token whose only difference is its last use was written back")
	assert.Equal(t, 0, done.WrittenBack)
}

// A gated request resolves a hash. It must not be a read of the record.
func TestTheTokenRecordIsReadOnceOnOpenAndNeverOnARequest(t *testing.T) {
	record := &countingTokens{}
	table, _, err := OpenTokenTable(qntxtest.CreateTestDB(t), record)
	require.NoError(t, err)
	_, err = table.Issue(aToken("abc", "one"))
	require.NoError(t, err)
	assert.Equal(t, 1, record.records)

	for range 20 {
		_, live := table.Lookup("abc")
		assert.True(t, live)
	}
	assert.Equal(t, 1, record.records, "a request read the record")
}

// Moving a client is a change to what it is, so the record carries it too, and
// a table rebuilt from the record after host loss holds the client where it was
// moved to.
func TestAMoveReachesTheTableAndTheRecord(t *testing.T) {
	record := &countingTokens{}
	table, _, err := OpenTokenTable(qntxtest.CreateTestDB(t), record)
	require.NoError(t, err)
	client := aToken("abc", "grok")
	client.Level = LevelOAuth
	id, err := table.Issue(client)
	require.NoError(t, err)

	require.NoError(t, table.SetNamespaces(id, []string{"vakconnectie"}))

	held, live := table.Lookup("abc")
	require.True(t, live)
	assert.Equal(t, []string{"vakconnectie"}, held.Namespaces)
	require.Len(t, record.held, 1)
	assert.Equal(t, []string{"vakconnectie"}, record.held[0].Namespaces, "the record kept the namespace the client was moved away from")
}

// A token the record holds and the table lacks is what rebuilds the table
// after host loss.
func TestOpeningTakesInTheTokensTheRecordHoldsAndTheTableLacks(t *testing.T) {
	record := &countingTokens{held: []TokenRecord{{
		ID: "T-1", Hash: "abc", Label: "one", DID: "did:key:zabc",
		Level: string(LevelAttestor), Namespaces: []string{"default"}, CreatedAt: 1,
	}}}
	table, done, err := OpenTokenTable(qntxtest.CreateTestDB(t), record)
	require.NoError(t, err)
	assert.Equal(t, 1, done.TakenIn)

	_, live := table.Lookup("abc")
	assert.True(t, live, "a token taken in from the record does not authorize")
}

// strictTokens is a record that takes a list only as a list, as the one behind
// the table on parquet does: null where a sequence belongs is refused there.
type strictTokens struct {
	countingTokens
	refuse bool
}

func (r *strictTokens) PutRecord(t TokenRecord) error {
	if r.refuse {
		return errors.New("the record is not answering")
	}
	if t.Namespaces == nil || t.ScopeRead == nil || t.ScopeWrite == nil {
		return errors.Newf("invalid type: null, expected a sequence (access token %s)", t.Label)
	}
	return r.countingTokens.PutRecord(t)
}

// A token issued with no namespaces named acts in every one the node serves,
// and that is an empty list in the record, never a missing one. The ROOT
// agent's token is issued this way (ADR-048).
func TestATokenIssuedWithNoNamespacesReachesTheRecordWhole(t *testing.T) {
	record := &strictTokens{}
	table, _, err := OpenTokenTable(qntxtest.CreateTestDB(t), record)
	require.NoError(t, err)

	everywhere := aToken("abc", "root-agent")
	everywhere.Namespaces = nil
	_, err = table.Issue(everywhere)
	require.NoError(t, err)

	require.Len(t, record.held, 1)
	assert.Equal(t, []string{}, record.held[0].Namespaces)
}

// A token whose record write failed is in the table alone, and the next open
// writes it to the record: the table is the truth.
func TestOpeningWritesBackATokenTheRecordLacks(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	record := &strictTokens{refuse: true}
	first, _, err := OpenTokenTable(db, record)
	require.NoError(t, err)
	_, err = first.Issue(aToken("abc", "one"))
	require.Error(t, err)
	require.Empty(t, record.held)

	record.refuse = false
	_, done, err := OpenTokenTable(db, record)
	require.NoError(t, err)
	assert.Equal(t, 1, done.WrittenBack)
	require.Len(t, record.held, 1)
	assert.Equal(t, "abc", record.held[0].Hash)
}

// "we need to keep users in mem"
//
// The gate reads a token and records its use on every request. Neither may
// wait on the operational db: that is where one namespace's flood of requests
// queued every other namespace's behind it.
func TestAGatedRequestNeverAsksTheOperationalDb(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	table, _, err := OpenTokenTable(db, nil)
	require.NoError(t, err)
	_, err = table.Issue(aToken("abc", "one"))
	require.NoError(t, err)

	require.NoError(t, db.Close())

	_, live := table.Lookup("abc")
	assert.True(t, live, "a token was not found once the operational db stopped answering")
	assert.NoError(t, table.Touch("abc"))
}

// A use is kept in memory and written on Flush, and until then the token says
// it was used all the same.
func TestLastUsedReachesTheTableOnFlush(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	table, _, err := OpenTokenTable(db, nil)
	require.NoError(t, err)
	_, err = table.Issue(aToken("abc", "one"))
	require.NoError(t, err)

	require.NoError(t, table.Touch("abc"))
	listed, err := table.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.NotNil(t, listed[0].LastUsedAt, "a use not yet written is not shown")

	reopened, _, err := OpenTokenTable(db, nil)
	require.NoError(t, err)
	written, err := reopened.List()
	require.NoError(t, err)
	assert.Nil(t, written[0].LastUsedAt, "a use reached the table before Flush")

	require.NoError(t, table.Flush())
	reopened, _, err = OpenTokenTable(db, nil)
	require.NoError(t, err)
	written, err = reopened.List()
	require.NoError(t, err)
	assert.Equal(t, listed[0].LastUsedAt, written[0].LastUsedAt)
}

// A flush the table refuses loses nothing: the use stays for the next.
func TestALastUsedTheTableRefusedIsKept(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	table, _, err := OpenTokenTable(db, nil)
	require.NoError(t, err)
	_, err = table.Issue(aToken("abc", "one"))
	require.NoError(t, err)
	require.NoError(t, table.Touch("abc"))

	require.NoError(t, db.Close())
	assert.Error(t, table.Flush())

	listed, err := table.List()
	require.NoError(t, err)
	assert.NotNil(t, listed[0].LastUsedAt, "a use the table refused was dropped")
}
