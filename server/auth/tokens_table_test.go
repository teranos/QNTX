package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
